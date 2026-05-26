package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const epsilon = 1e-8

type Vec3 struct {
	X float64
	Y float64
	Z float64
}

type Triangle struct {
	A Vec3
	B Vec3
	C Vec3
}

type Point2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Segment2D struct {
	A Point2D
	B Point2D
}

type LayerResult struct {
	Z        float64         `json:"z"`
	Points   []Point2D       `json:"points"`
	Contours []ContourResult `json:"contours,omitempty"`
}

type ContourResult struct {
	Closed   bool            `json:"closed"`
	Role     string          `json:"role,omitempty"`
	Points   []Point2D       `json:"points"`
	Children []ContourResult `json:"children,omitempty"`
}

type SliceOutput struct {
	Input       string        `json:"input"`
	LayerHeight float64       `json:"layer_height_mm"`
	Layers      []LayerResult `json:"layers"`
}

func main() {
	input := flag.String("in", "", "Path to input STL file")
	layerHeight := flag.Float64("layer", 0.2, "Layer height in mm")
	output := flag.String("out", "", "Optional path to JSON output file (defaults to stdout)")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		exitWithError(errors.New("missing required -in argument"))
	}
	if *layerHeight <= 0 {
		exitWithError(errors.New("-layer must be > 0"))
	}

	triangles, err := readSTL(*input)
	if err != nil {
		exitWithError(err)
	}
	if len(triangles) == 0 {
		exitWithError(errors.New("no triangles parsed from STL"))
	}

	layers := sliceTriangles(triangles, *layerHeight)
	result := SliceOutput{
		Input:       filepath.Base(*input),
		LayerHeight: *layerHeight,
		Layers:      layers,
	}

	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		exitWithError(fmt.Errorf("failed to encode JSON: %w", err))
	}

	if strings.TrimSpace(*output) == "" {
		fmt.Println(string(encoded))
		return
	}

	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		exitWithError(fmt.Errorf("failed to write output file: %w", err))
	}
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func readSTL(path string) ([]Triangle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed reading STL: %w", err)
	}

	if tris, ok, err := parseBinarySTL(data); err != nil {
		return nil, err
	} else if ok {
		return tris, nil
	}

	return parseASCIISTL(string(data))
}

func parseBinarySTL(data []byte) ([]Triangle, bool, error) {
	if len(data) < 84 {
		return nil, false, nil
	}

	count := binary.LittleEndian.Uint32(data[80:84])
	expectedSize := 84 + int(count)*50
	if expectedSize != len(data) {
		return nil, false, nil
	}

	triangles := make([]Triangle, 0, count)
	offset := 84
	for i := uint32(0); i < count; i++ {
		if offset+50 > len(data) {
			return nil, false, errors.New("binary STL ended unexpectedly")
		}

		// Skip normal vector (12 bytes).
		offset += 12
		v1 := readBinaryVec3(data[offset : offset+12])
		offset += 12
		v2 := readBinaryVec3(data[offset : offset+12])
		offset += 12
		v3 := readBinaryVec3(data[offset : offset+12])
		offset += 12

		// Skip attribute byte count (2 bytes).
		offset += 2
		triangles = append(triangles, Triangle{A: v1, B: v2, C: v3})
	}

	return triangles, true, nil
}

func readBinaryVec3(b []byte) Vec3 {
	if len(b) < 12 {
		return Vec3{}
	}
	return Vec3{
		X: float64(math.Float32frombits(binary.LittleEndian.Uint32(b[0:4]))),
		Y: float64(math.Float32frombits(binary.LittleEndian.Uint32(b[4:8]))),
		Z: float64(math.Float32frombits(binary.LittleEndian.Uint32(b[8:12]))),
	}
}

func parseASCIISTL(content string) ([]Triangle, error) {
	reader := strings.NewReader(content)
	scanner := bufio.NewScanner(reader)

	vertices := make([]Vec3, 0, 3)
	triangles := make([]Triangle, 0)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(strings.ToLower(line), "vertex") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid vertex line: %q", line)
		}

		x, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid X in vertex %q: %w", line, err)
		}
		y, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Y in vertex %q: %w", line, err)
		}
		z, err := strconv.ParseFloat(parts[3], 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Z in vertex %q: %w", line, err)
		}

		vertices = append(vertices, Vec3{X: x, Y: y, Z: z})
		if len(vertices) == 3 {
			triangles = append(triangles, Triangle{A: vertices[0], B: vertices[1], C: vertices[2]})
			vertices = vertices[:0]
		}
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed reading ASCII STL: %w", err)
	}
	if len(triangles) == 0 {
		return nil, errors.New("could not parse ASCII STL triangles")
	}

	return triangles, nil
}

func sliceTriangles(triangles []Triangle, layerHeight float64) []LayerResult {
	tol := geometryTolerance(triangles, layerHeight)
	minZ, maxZ := meshBoundsZ(triangles)
	layers := make([]LayerResult, 0)

	for z := minZ; z <= maxZ+tol; z += layerHeight {
		segments := make([]Segment2D, 0)
		for _, tri := range triangles {
			p1, p2, ok := intersectTriangleAtZWithTolerance(tri, z, tol)
			if !ok {
				continue
			}

			a := Point2D{X: p1.X, Y: p1.Y}
			b := Point2D{X: p2.X, Y: p2.Y}
			if pointsEqualWithin(a, b, tol) {
				continue
			}

			segments = appendUniqueSegmentWithTolerance(segments, Segment2D{A: a, B: b}, tol)
		}

		paths := extractContourPathsWithTolerance(segments, tol)
		contours := buildContourHierarchy(paths, tol)
		points := flattenContourHierarchy(contours)
		layers = append(layers, LayerResult{Z: roundTo(z, 6), Points: points, Contours: contours})
	}

	return layers
}

func geometryTolerance(triangles []Triangle, layerHeight float64) float64 {
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	minZ, maxZ := math.Inf(1), math.Inf(-1)

	for _, t := range triangles {
		for _, v := range []Vec3{t.A, t.B, t.C} {
			if v.X < minX {
				minX = v.X
			}
			if v.X > maxX {
				maxX = v.X
			}
			if v.Y < minY {
				minY = v.Y
			}
			if v.Y > maxY {
				maxY = v.Y
			}
			if v.Z < minZ {
				minZ = v.Z
			}
			if v.Z > maxZ {
				maxZ = v.Z
			}
		}
	}

	if math.IsInf(minX, 1) || math.IsInf(minY, 1) || math.IsInf(minZ, 1) {
		return 1e-6
	}

	dx := maxX - minX
	dy := maxY - minY
	dz := maxZ - minZ
	diagonal := math.Sqrt(dx*dx + dy*dy + dz*dz)
	tol := math.Max(1e-6, diagonal*1e-8)
	if layerHeight > 0 {
		tol = math.Max(tol, layerHeight*1e-6)
	}
	return tol
}

func meshBoundsZ(triangles []Triangle) (float64, float64) {
	minZ := math.Inf(1)
	maxZ := math.Inf(-1)

	for _, t := range triangles {
		for _, v := range []Vec3{t.A, t.B, t.C} {
			if v.Z < minZ {
				minZ = v.Z
			}
			if v.Z > maxZ {
				maxZ = v.Z
			}
		}
	}

	return minZ, maxZ
}

func intersectTriangleAtZ(tri Triangle, z float64) (Vec3, Vec3, bool) {
	edges := [][2]Vec3{{tri.A, tri.B}, {tri.B, tri.C}, {tri.C, tri.A}}
	intersections := make([]Vec3, 0, 2)

	for _, edge := range edges {
		a := edge[0]
		b := edge[1]
		da := a.Z - z
		db := b.Z - z

		if nearlyZero(da) && nearlyZero(db) {
			continue
		}

		if nearlyZero(da) {
			intersections = appendUniqueVec3(intersections, Vec3{X: a.X, Y: a.Y, Z: z})
			continue
		}
		if nearlyZero(db) {
			intersections = appendUniqueVec3(intersections, Vec3{X: b.X, Y: b.Y, Z: z})
			continue
		}

		if (da < 0 && db > 0) || (da > 0 && db < 0) {
			t := da / (da - db)
			p := Vec3{
				X: a.X + t*(b.X-a.X),
				Y: a.Y + t*(b.Y-a.Y),
				Z: z,
			}
			intersections = appendUniqueVec3(intersections, p)
		}
	}

	if len(intersections) != 2 {
		return Vec3{}, Vec3{}, false
	}

	return intersections[0], intersections[1], true
}

func appendUniqueVec3(points []Vec3, p Vec3) []Vec3 {
	for _, existing := range points {
		if nearlyEqual(existing.X, p.X) && nearlyEqual(existing.Y, p.Y) && nearlyEqual(existing.Z, p.Z) {
			return points
		}
	}
	return append(points, p)
}

func appendUniquePoint(points []Point2D, p Point2D) []Point2D {
	for _, existing := range points {
		if pointsEqual(existing, p) {
			return points
		}
	}
	return append(points, p)
}

func appendUniqueSegment(segments []Segment2D, s Segment2D) []Segment2D {
	for _, existing := range segments {
		if (pointsEqual(existing.A, s.A) && pointsEqual(existing.B, s.B)) ||
			(pointsEqual(existing.A, s.B) && pointsEqual(existing.B, s.A)) {
			return segments
		}
	}

	return append(segments, s)
}

func stitchSegmentsToLoops(segments []Segment2D) [][]Point2D {
	if len(segments) == 0 {
		return nil
	}

	adjacency := make(map[string][]int)
	segmentKeys := make([][2]string, len(segments))
	pointByKey := make(map[string]Point2D)

	for i, s := range segments {
		ak := pointKey(s.A)
		bk := pointKey(s.B)
		segmentKeys[i] = [2]string{ak, bk}
		adjacency[ak] = append(adjacency[ak], i)
		adjacency[bk] = append(adjacency[bk], i)
		if _, ok := pointByKey[ak]; !ok {
			pointByKey[ak] = Point2D{X: roundTo(s.A.X, 6), Y: roundTo(s.A.Y, 6)}
		}
		if _, ok := pointByKey[bk]; !ok {
			pointByKey[bk] = Point2D{X: roundTo(s.B.X, 6), Y: roundTo(s.B.Y, 6)}
		}
	}

	used := make([]bool, len(segments))
	loops := make([][]Point2D, 0)

	for i := 0; i < len(segments); i++ {
		if used[i] {
			continue
		}

		used[i] = true
		start := segmentKeys[i][0]
		current := segmentKeys[i][1]
		prev := start
		loop := []Point2D{pointByKey[start], pointByKey[current]}

		for {
			if current == start {
				break
			}

			nextSegment := -1
			nextPoint := ""
			for _, candidate := range adjacency[current] {
				if used[candidate] {
					continue
				}

				a := segmentKeys[candidate][0]
				b := segmentKeys[candidate][1]
				other := a
				if a == current {
					other = b
				}

				if other == prev {
					continue
				}

				nextSegment = candidate
				nextPoint = other
				break
			}

			if nextSegment == -1 {
				for _, candidate := range adjacency[current] {
					if used[candidate] {
						continue
					}

					a := segmentKeys[candidate][0]
					b := segmentKeys[candidate][1]
					nextSegment = candidate
					if a == current {
						nextPoint = b
					} else {
						nextPoint = a
					}
					break
				}
			}

			if nextSegment == -1 {
				break
			}

			used[nextSegment] = true
			prev = current
			current = nextPoint
			loop = append(loop, pointByKey[current])
		}

		if len(loop) >= 3 && current == start {
			loop = loop[:len(loop)-1]
			loop = simplifyCollinearLoop(loop)
			if len(loop) >= 3 {
				loops = append(loops, loop)
			}
		}
	}

	return loops
}

func orderedToolpathPoints(loops [][]Point2D) []Point2D {
	if len(loops) == 0 {
		return nil
	}

	normalized := make([][]Point2D, 0, len(loops))
	for _, loop := range loops {
		normalized = append(normalized, normalizeLoop(loop))
	}

	// Keep multi-loop output deterministic by ordering loops by start corner.
	sort.Slice(normalized, func(i, j int) bool {
		a := normalized[i][0]
		b := normalized[j][0]
		if !nearlyEqual(a.X, b.X) {
			return a.X < b.X
		}
		if !nearlyEqual(a.Y, b.Y) {
			return a.Y < b.Y
		}
		return len(normalized[i]) < len(normalized[j])
	})

	points := make([]Point2D, 0)
	for _, loop := range normalized {
		points = append(points, loop...)
	}

	return points
}

func normalizeLoop(loop []Point2D) []Point2D {
	if len(loop) == 0 {
		return nil
	}

	normalized := append([]Point2D(nil), loop...)
	if signedArea(normalized) > 0 {
		reverseLoop(normalized)
	}

	start := 0
	for i := 1; i < len(normalized); i++ {
		if normalized[i].X < normalized[start].X-epsilon ||
			(nearlyEqual(normalized[i].X, normalized[start].X) && normalized[i].Y < normalized[start].Y-epsilon) {
			start = i
		}
	}

	rotated := make([]Point2D, 0, len(normalized))
	rotated = append(rotated, normalized[start:]...)
	rotated = append(rotated, normalized[:start]...)
	return rotated
}

func reverseLoop(points []Point2D) {
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
}

func signedArea(points []Point2D) float64 {
	if len(points) < 3 {
		return 0
	}

	area := 0.0
	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		area += points[i].X*points[j].Y - points[j].X*points[i].Y
	}

	return area / 2
}

func pointsFromSegments(segments []Segment2D) []Point2D {
	points := make([]Point2D, 0)
	for _, s := range segments {
		points = appendUniquePoint(points, s.A)
		points = appendUniquePoint(points, s.B)
	}
	return points
}

func simplifyCollinearLoop(points []Point2D) []Point2D {
	if len(points) < 3 {
		return points
	}

	simplified := points
	for {
		if len(simplified) < 3 {
			return simplified
		}

		changed := false
		n := len(simplified)
		next := make([]Point2D, 0, n)

		for i := 0; i < n; i++ {
			prev := simplified[(i-1+n)%n]
			curr := simplified[i]
			nxt := simplified[(i+1)%n]

			if isCollinearAndBetween(prev, curr, nxt) {
				changed = true
				continue
			}

			next = append(next, curr)
		}

		simplified = next
		if !changed {
			return simplified
		}
	}
}

func isCollinearAndBetween(a, b, c Point2D) bool {
	abx := b.X - a.X
	aby := b.Y - a.Y
	bcx := c.X - b.X
	bcy := c.Y - b.Y
	cross := abx*bcy - aby*bcx
	if math.Abs(cross) > 1e-6 {
		return false
	}

	minX := math.Min(a.X, c.X) - epsilon
	maxX := math.Max(a.X, c.X) + epsilon
	minY := math.Min(a.Y, c.Y) - epsilon
	maxY := math.Max(a.Y, c.Y) + epsilon

	return b.X >= minX && b.X <= maxX && b.Y >= minY && b.Y <= maxY
}

func pointKey(p Point2D) string {
	return fmt.Sprintf("%.6f,%.6f", roundTo(p.X, 6), roundTo(p.Y, 6))
}

func pointsEqual(a, b Point2D) bool {
	return nearlyEqual(a.X, b.X) && nearlyEqual(a.Y, b.Y)
}

func nearlyZero(v float64) bool {
	return math.Abs(v) < epsilon
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func roundTo(v float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(v*scale) / scale
}

type contourPath struct {
	Points []Point2D
	Closed bool
}

type contourNode struct {
	Contour  ContourResult
	Area     float64
	Centroid Point2D
	Children []*contourNode
	Parent   *contourNode
}

func intersectTriangleAtZWithTolerance(tri Triangle, z, tol float64) (Vec3, Vec3, bool) {
	edges := [][2]Vec3{{tri.A, tri.B}, {tri.B, tri.C}, {tri.C, tri.A}}
	intersections := make([]Vec3, 0, 2)

	for _, edge := range edges {
		a := edge[0]
		b := edge[1]
		da := a.Z - z
		db := b.Z - z

		if math.Abs(da) <= tol && math.Abs(db) <= tol {
			continue
		}

		if math.Abs(da) <= tol {
			intersections = appendUniqueVec3WithTolerance(intersections, Vec3{X: a.X, Y: a.Y, Z: z}, tol)
			continue
		}
		if math.Abs(db) <= tol {
			intersections = appendUniqueVec3WithTolerance(intersections, Vec3{X: b.X, Y: b.Y, Z: z}, tol)
			continue
		}

		if (da < 0 && db > 0) || (da > 0 && db < 0) {
			t := da / (da - db)
			p := Vec3{
				X: a.X + t*(b.X-a.X),
				Y: a.Y + t*(b.Y-a.Y),
				Z: z,
			}
			intersections = appendUniqueVec3WithTolerance(intersections, p, tol)
		}
	}

	if len(intersections) != 2 {
		return Vec3{}, Vec3{}, false
	}

	return intersections[0], intersections[1], true
}

func appendUniqueVec3WithTolerance(points []Vec3, p Vec3, tol float64) []Vec3 {
	for _, existing := range points {
		if pointsEqualWithin(Point2D{X: existing.X, Y: existing.Y}, Point2D{X: p.X, Y: p.Y}, tol) && math.Abs(existing.Z-p.Z) <= tol {
			return points
		}
	}
	return append(points, p)
}

func appendUniqueSegmentWithTolerance(segments []Segment2D, s Segment2D, tol float64) []Segment2D {
	for _, existing := range segments {
		if (pointsEqualWithin(existing.A, s.A, tol) && pointsEqualWithin(existing.B, s.B, tol)) ||
			(pointsEqualWithin(existing.A, s.B, tol) && pointsEqualWithin(existing.B, s.A, tol)) {
			return segments
		}
	}

	return append(segments, s)
}

func pointsEqualWithin(a, b Point2D, tol float64) bool {
	return math.Abs(a.X-b.X) <= tol && math.Abs(a.Y-b.Y) <= tol
}

func pointKeyWithTolerance(p Point2D, tol float64) string {
	if tol <= 0 {
		tol = 1e-6
	}
	qx := int64(math.Round(p.X / tol))
	qy := int64(math.Round(p.Y / tol))
	return fmt.Sprintf("%d,%d", qx, qy)
}

func snapPointWithTolerance(p Point2D, tol float64) Point2D {
	if tol <= 0 {
		tol = 1e-6
	}
	return Point2D{X: math.Round(p.X/tol) * tol, Y: math.Round(p.Y/tol) * tol}
}

func extractContourPathsWithTolerance(segments []Segment2D, tol float64) []contourPath {
	if len(segments) == 0 {
		return nil
	}

	adjacency := make(map[string][]int)
	segmentKeys := make([][2]string, len(segments))
	pointByKey := make(map[string]Point2D)
	degree := make(map[string]int)

	for i, s := range segments {
		ak := pointKeyWithTolerance(s.A, tol)
		bk := pointKeyWithTolerance(s.B, tol)
		segmentKeys[i] = [2]string{ak, bk}
		adjacency[ak] = append(adjacency[ak], i)
		adjacency[bk] = append(adjacency[bk], i)
		degree[ak]++
		degree[bk]++
		if _, ok := pointByKey[ak]; !ok {
			pointByKey[ak] = snapPointWithTolerance(s.A, tol)
		}
		if _, ok := pointByKey[bk]; !ok {
			pointByKey[bk] = snapPointWithTolerance(s.B, tol)
		}
	}

	for key := range adjacency {
		sort.Ints(adjacency[key])
	}

	used := make([]bool, len(segments))
	paths := make([]contourPath, 0)

	for {
		startSeg := chooseNextContourStart(used, segmentKeys, degree)
		if startSeg == -1 {
			break
		}

		path := walkContourPath(startSeg, used, adjacency, segmentKeys, pointByKey, degree)
		if len(path.Points) == 0 {
			continue
		}

		if path.Closed {
			path.Points = simplifyClosedContour(path.Points, tol)
			if len(path.Points) < 3 {
				continue
			}
		} else {
			path.Points = simplifyOpenContour(path.Points, tol)
			if len(path.Points) < 2 {
				continue
			}
		}

		paths = append(paths, path)
	}

	return paths
}

func chooseNextContourStart(used []bool, segmentKeys [][2]string, degree map[string]int) int {
	best := -1
	for i := range segmentKeys {
		if used[i] {
			continue
		}
		a := segmentKeys[i][0]
		b := segmentKeys[i][1]
		if degree[a] != 2 || degree[b] != 2 {
			if best == -1 || segmentKeyLess(segmentKeys[i], segmentKeys[best]) {
				best = i
			}
		}
	}
	if best != -1 {
		return best
	}

	for i := range segmentKeys {
		if used[i] {
			continue
		}
		if best == -1 || segmentKeyLess(segmentKeys[i], segmentKeys[best]) {
			best = i
		}
	}

	return best
}

func segmentKeyLess(a, b [2]string) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	if a[1] != b[1] {
		return a[1] < b[1]
	}
	return false
}

func walkContourPath(startSeg int, used []bool, adjacency map[string][]int, segmentKeys [][2]string, pointByKey map[string]Point2D, degree map[string]int) contourPath {
	startKey, nextKey := contourStartOrientation(segmentKeys[startSeg], degree)
	used[startSeg] = true

	points := []Point2D{pointByKey[startKey]}
	if startKey != nextKey {
		points = append(points, pointByKey[nextKey])
	}

	prevKey := startKey
	currentKey := nextKey
	closed := false

	for {
		nextSeg, nextPointKey, ok := chooseNextContourEdge(currentKey, prevKey, adjacency, segmentKeys, used)
		if !ok {
			break
		}

		used[nextSeg] = true
		prevKey = currentKey
		currentKey = nextPointKey
		if currentKey == startKey {
			closed = true
			break
		}
		points = append(points, pointByKey[currentKey])
	}

	return contourPath{Points: points, Closed: closed}
}

func contourStartOrientation(keys [2]string, degree map[string]int) (string, string) {
	a, b := keys[0], keys[1]
	da := degree[a]
	db := degree[b]

	switch {
	case da != 2 && db == 2:
		return a, b
	case db != 2 && da == 2:
		return b, a
	case da != 2 && db != 2:
		if da != db {
			if da < db {
				return a, b
			}
			return b, a
		}
		if a <= b {
			return a, b
		}
		return b, a
	default:
		if a <= b {
			return a, b
		}
		return b, a
	}
}

func chooseNextContourEdge(currentKey, prevKey string, adjacency map[string][]int, segmentKeys [][2]string, used []bool) (int, string, bool) {
	type candidate struct {
		index int
		key   string
	}

	candidates := make([]candidate, 0)
	for _, segIdx := range adjacency[currentKey] {
		if used[segIdx] {
			continue
		}
		a := segmentKeys[segIdx][0]
		b := segmentKeys[segIdx][1]
		other := ""
		switch {
		case a == currentKey:
			other = b
		case b == currentKey:
			other = a
		default:
			continue
		}
		if other == prevKey {
			continue
		}
		candidates = append(candidates, candidate{index: segIdx, key: other})
	}

	if len(candidates) == 0 {
		for _, segIdx := range adjacency[currentKey] {
			if used[segIdx] {
				continue
			}
			a := segmentKeys[segIdx][0]
			b := segmentKeys[segIdx][1]
			other := ""
			switch {
			case a == currentKey:
				other = b
			case b == currentKey:
				other = a
			default:
				continue
			}
			candidates = append(candidates, candidate{index: segIdx, key: other})
		}
	}

	if len(candidates) == 0 {
		return -1, "", false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].key != candidates[j].key {
			return candidates[i].key < candidates[j].key
		}
		return candidates[i].index < candidates[j].index
	})

	return candidates[0].index, candidates[0].key, true
}

func simplifyClosedContour(points []Point2D, tol float64) []Point2D {
	if len(points) < 3 {
		return points
	}

	cleaned := dedupeConsecutivePointsWithTolerance(points, tol)
	if len(cleaned) < 3 {
		return cleaned
	}

	if signedArea(cleaned) > 0 {
		reverseLoop(cleaned)
	}

	start := 0
	for i := 1; i < len(cleaned); i++ {
		if cleaned[i].X < cleaned[start].X-tol || (math.Abs(cleaned[i].X-cleaned[start].X) <= tol && cleaned[i].Y < cleaned[start].Y-tol) {
			start = i
		}
	}

	rotated := append([]Point2D(nil), cleaned[start:]...)
	rotated = append(rotated, cleaned[:start]...)
	return simplifyCollinearLoop(rotated)
}

func simplifyOpenContour(points []Point2D, tol float64) []Point2D {
	if len(points) < 2 {
		return points
	}

	cleaned := dedupeConsecutivePointsWithTolerance(points, tol)
	if len(cleaned) < 3 {
		return cleaned
	}

	result := make([]Point2D, 0, len(cleaned))
	result = append(result, cleaned[0])
	for i := 1; i < len(cleaned)-1; i++ {
		prev := result[len(result)-1]
		curr := cleaned[i]
		next := cleaned[i+1]
		if isCollinearAndBetween(prev, curr, next) {
			continue
		}
		result = append(result, curr)
	}
	result = append(result, cleaned[len(cleaned)-1])
	return dedupeConsecutivePointsWithTolerance(result, tol)
}

func dedupeConsecutivePointsWithTolerance(points []Point2D, tol float64) []Point2D {
	if len(points) == 0 {
		return nil
	}

	result := make([]Point2D, 0, len(points))
	result = append(result, points[0])
	for i := 1; i < len(points); i++ {
		if pointsEqualWithin(result[len(result)-1], points[i], tol) {
			continue
		}
		result = append(result, points[i])
	}

	if len(result) > 1 && pointsEqualWithin(result[0], result[len(result)-1], tol) {
		result = result[:len(result)-1]
	}

	return result
}

func buildContourHierarchy(paths []contourPath, tol float64) []ContourResult {
	if len(paths) == 0 {
		return nil
	}

	closedNodes := make([]*contourNode, 0, len(paths))
	openNodes := make([]*contourNode, 0)

	for _, path := range paths {
		if path.Closed {
			normalized := append([]Point2D(nil), path.Points...)
			normalized = dedupeConsecutivePointsWithTolerance(normalized, tol)
			if len(normalized) < 3 {
				continue
			}
			normalized = normalizeLoop(normalized)
			normalized = dedupeConsecutivePointsWithTolerance(normalized, tol)
			if len(normalized) < 3 {
				continue
			}
			closedNodes = append(closedNodes, &contourNode{
				Contour: ContourResult{
					Closed: true,
					Points: append([]Point2D(nil), normalized...),
				},
				Area:     math.Abs(signedArea(normalized)),
				Centroid: polygonCentroid(normalized),
			})
			continue
		}

		open := simplifyOpenContour(path.Points, tol)
		if len(open) < 2 {
			continue
		}
		openNodes = append(openNodes, &contourNode{
			Contour: ContourResult{
				Closed: false,
				Role:   "open",
				Points: append([]Point2D(nil), open...),
			},
			Centroid: polygonCentroid(open),
		})
	}

	for _, node := range closedNodes {
		var parent *contourNode
		parentArea := math.Inf(1)
		for _, other := range closedNodes {
			if other == node || other.Area <= node.Area+tol {
				continue
			}
			if !pointInPolygon(node.Centroid, other.Contour.Points, tol) {
				continue
			}
			if other.Area < parentArea {
				parent = other
				parentArea = other.Area
			}
		}
		node.Parent = parent
		if parent != nil {
			parent.Children = append(parent.Children, node)
		}
	}

	roots := make([]*contourNode, 0, len(closedNodes)+len(openNodes))
	for _, node := range closedNodes {
		if node.Parent == nil {
			roots = append(roots, node)
		}
	}
	roots = append(roots, openNodes...)
	sortContourNodes(roots)

	results := make([]ContourResult, 0, len(roots))
	for _, root := range roots {
		results = append(results, contourNodeToResult(root, 0))
	}

	return results
}

func sortContourNodes(nodes []*contourNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return contourNodeLess(nodes[i], nodes[j])
	})
	for _, node := range nodes {
		if len(node.Children) > 0 {
			sortContourNodes(node.Children)
		}
	}
}

func contourNodeLess(a, b *contourNode) bool {
	if len(a.Contour.Points) == 0 || len(b.Contour.Points) == 0 {
		return len(a.Contour.Points) < len(b.Contour.Points)
	}
	pa := a.Contour.Points[0]
	pb := b.Contour.Points[0]
	if !pointsEqualWithin(pa, pb, 1e-6) {
		if pa.X != pb.X {
			return pa.X < pb.X
		}
		return pa.Y < pb.Y
	}
	if a.Contour.Closed != b.Contour.Closed {
		return a.Contour.Closed && !b.Contour.Closed
	}
	if a.Area != b.Area {
		return a.Area > b.Area
	}
	return len(a.Contour.Points) < len(b.Contour.Points)
}

func contourNodeToResult(node *contourNode, depth int) ContourResult {
	result := ContourResult{
		Closed: node.Contour.Closed,
		Points: append([]Point2D(nil), node.Contour.Points...),
	}

	if node.Contour.Closed {
		if depth%2 == 0 {
			result.Role = "outer"
		} else {
			result.Role = "hole"
		}
	} else {
		result.Role = node.Contour.Role
	}

	if len(node.Children) > 0 {
		result.Children = make([]ContourResult, 0, len(node.Children))
		for _, child := range node.Children {
			result.Children = append(result.Children, contourNodeToResult(child, depth+1))
		}
	}

	return result
}

func flattenContourHierarchy(contours []ContourResult) []Point2D {
	points := make([]Point2D, 0)
	for _, contour := range contours {
		points = append(points, contour.Points...)
		if len(contour.Children) > 0 {
			points = append(points, flattenContourHierarchy(contour.Children)...)
		}
	}
	return points
}

func polygonCentroid(points []Point2D) Point2D {
	if len(points) == 0 {
		return Point2D{}
	}

	area := signedArea(points)
	if math.Abs(area) <= 1e-12 {
		return averagePoint(points)
	}

	cx := 0.0
	cy := 0.0
	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		factor := points[i].X*points[j].Y - points[j].X*points[i].Y
		cx += (points[i].X + points[j].X) * factor
		cy += (points[i].Y + points[j].Y) * factor
	}

	scale := 1.0 / (6.0 * area)
	return Point2D{X: cx * scale, Y: cy * scale}
}

func averagePoint(points []Point2D) Point2D {
	if len(points) == 0 {
		return Point2D{}
	}

	sx := 0.0
	sy := 0.0
	for _, p := range points {
		sx += p.X
		sy += p.Y
	}
	return Point2D{X: sx / float64(len(points)), Y: sy / float64(len(points))}
}

func pointInPolygon(point Point2D, polygon []Point2D, tol float64) bool {
	if len(polygon) < 3 {
		return false
	}

	inside := false
	for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
		a := polygon[j]
		b := polygon[i]
		if pointOnSegmentWithinTolerance(point, a, b, tol) {
			return true
		}

		intersects := ((a.Y > point.Y) != (b.Y > point.Y)) &&
			(point.X <= (b.X-a.X)*(point.Y-a.Y)/(b.Y-a.Y)+a.X+tol)
		if intersects {
			inside = !inside
		}
	}

	return inside
}

func pointOnSegmentWithinTolerance(p, a, b Point2D, tol float64) bool {
	minX := math.Min(a.X, b.X) - tol
	maxX := math.Max(a.X, b.X) + tol
	minY := math.Min(a.Y, b.Y) - tol
	maxY := math.Max(a.Y, b.Y) + tol
	if p.X < minX || p.X > maxX || p.Y < minY || p.Y > maxY {
		return false
	}

	cross := (b.X-a.X)*(p.Y-a.Y) - (b.Y-a.Y)*(p.X-a.X)
	return math.Abs(cross) <= math.Max(tol*10, 1e-9)
}
