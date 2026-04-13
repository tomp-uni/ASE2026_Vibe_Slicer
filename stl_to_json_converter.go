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
	Z      float64   `json:"z"`
	Points []Point2D `json:"points"`
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
	minZ, maxZ := meshBoundsZ(triangles)
	layers := make([]LayerResult, 0)

	for z := minZ; z <= maxZ+epsilon; z += layerHeight {
		segments := make([]Segment2D, 0)
		for _, tri := range triangles {
			p1, p2, ok := intersectTriangleAtZ(tri, z)
			if !ok {
				continue
			}

			a := Point2D{X: p1.X, Y: p1.Y}
			b := Point2D{X: p2.X, Y: p2.Y}
			if pointsEqual(a, b) {
				continue
			}

			segments = appendUniqueSegment(segments, Segment2D{A: a, B: b})
		}

		loops := stitchSegmentsToLoops(segments)
		points := flattenLoops(loops)
		if len(points) == 0 {
			points = pointsFromSegments(segments)
		}

		layers = append(layers, LayerResult{Z: roundTo(z, 6), Points: points})
	}

	return layers
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

func flattenLoops(loops [][]Point2D) []Point2D {
	points := make([]Point2D, 0)
	for _, loop := range loops {
		for _, p := range loop {
			points = appendUniquePoint(points, p)
		}
	}
	return points
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
