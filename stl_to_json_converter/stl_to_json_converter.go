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

type sliceTolerances struct {
	intersection float64
	graph        float64
	validation   float64
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
	_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
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
	tols := geometryTolerances(triangles, layerHeight)
	minZ, maxZ := meshBoundsZ(triangles)
	layers := make([]LayerResult, 0)

	for layerIdx := 0; ; layerIdx++ {
		z := minZ + float64(layerIdx)*layerHeight
		if z > maxZ+tols.validation {
			break
		}

		segments := make([]Segment2D, 0)
		coplanarSegments := make([]Segment2D, 0)
		for _, tri := range triangles {
			triSegments, coplanar := trianglePlaneSegmentsAtZ(tri, z, tols.intersection)
			if coplanar {
				coplanarSegments = append(coplanarSegments, triSegments...)
				continue
			}
			for _, segment := range triSegments {
				if pointsEqualWithin(segment.A, segment.B, tols.graph) {
					continue
				}
				segments = appendUniqueSegmentWithTolerance(segments, segment, tols.graph)
			}
		}
		if len(segments) == 0 && len(coplanarSegments) > 0 {
			for _, segment := range coplanarSegments {
				if pointsEqualWithin(segment.A, segment.B, tols.graph) {
					continue
				}
				segments = appendUniqueSegmentWithTolerance(segments, segment, tols.graph)
			}
		}

		paths := extractContourPathsWithTolerance(segments, tols.graph)
		contours := buildContourHierarchy(paths, tols.validation)
		points := flattenContourHierarchy(contours)
		layers = append(layers, LayerResult{Z: roundTo(z, 6), Points: points, Contours: contours})
	}

	return layers
}

func geometryTolerances(triangles []Triangle, layerHeight float64) sliceTolerances {
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
		return sliceTolerances{intersection: 1e-6, graph: 2e-6, validation: 4e-6}
	}

	dx := maxX - minX
	dy := maxY - minY
	dz := maxZ - minZ
	diagonal := math.Sqrt(dx*dx + dy*dy + dz*dz)
	base := math.Max(1e-6, diagonal*1e-8)
	if layerHeight > 0 {
		base = math.Max(base, layerHeight*1e-6)
	}
	return sliceTolerances{
		intersection: base,
		graph:        math.Max(base*2, 2e-6),
		validation:   math.Max(base*4, 4e-6),
	}
}

func trianglePlaneSegmentsAtZ(tri Triangle, z, tol float64) ([]Segment2D, bool) {
	vertices := []Vec3{tri.A, tri.B, tri.C}
	offsets := []float64{tri.A.Z - z, tri.B.Z - z, tri.C.Z - z}
	onPlane := 0
	for _, d := range offsets {
		if math.Abs(d) <= tol {
			onPlane++
		}
	}

	if onPlane == 3 {
		return triangleBoundarySegments(tri), true
	}

	points := make([]Point2D, 0, 3)
	for i := 0; i < 3; i++ {
		a := vertices[i]
		b := vertices[(i+1)%3]
		da := a.Z - z
		db := b.Z - z

		if math.Abs(da) <= tol && math.Abs(db) <= tol {
			points = appendUniquePointWithinTolerance(points, Point2D{X: a.X, Y: a.Y}, tol)
			points = appendUniquePointWithinTolerance(points, Point2D{X: b.X, Y: b.Y}, tol)
			continue
		}
		if math.Abs(da) <= tol {
			points = appendUniquePointWithinTolerance(points, Point2D{X: a.X, Y: a.Y}, tol)
		}
		if math.Abs(db) <= tol {
			points = appendUniquePointWithinTolerance(points, Point2D{X: b.X, Y: b.Y}, tol)
		}
		if (da < -tol && db > tol) || (da > tol && db < -tol) {
			t := da / (da - db)
			p := Point2D{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
			points = appendUniquePointWithinTolerance(points, p, tol)
		}
	}

	if len(points) == 2 {
		return []Segment2D{{A: points[0], B: points[1]}}, false
	}

	return nil, false
}

func triangleBoundarySegments(tri Triangle) []Segment2D {
	edges := []struct {
		segment Segment2D
		length  float64
	}{
		{segment: Segment2D{A: Point2D{X: tri.A.X, Y: tri.A.Y}, B: Point2D{X: tri.B.X, Y: tri.B.Y}}, length: math.Hypot(tri.B.X-tri.A.X, tri.B.Y-tri.A.Y)},
		{segment: Segment2D{A: Point2D{X: tri.B.X, Y: tri.B.Y}, B: Point2D{X: tri.C.X, Y: tri.C.Y}}, length: math.Hypot(tri.C.X-tri.B.X, tri.C.Y-tri.B.Y)},
		{segment: Segment2D{A: Point2D{X: tri.C.X, Y: tri.C.Y}, B: Point2D{X: tri.A.X, Y: tri.A.Y}}, length: math.Hypot(tri.A.X-tri.C.X, tri.A.Y-tri.C.Y)},
	}
	sort.SliceStable(edges, func(i, j int) bool { return edges[i].length > edges[j].length })
	result := make([]Segment2D, 0, 2)
	for i := 1; i < len(edges); i++ {
		if edges[i].length <= epsilon {
			continue
		}
		result = append(result, edges[i].segment)
	}
	return result
}

func appendUniquePointWithinTolerance(points []Point2D, p Point2D, tol float64) []Point2D {
	for _, existing := range points {
		if pointsEqualWithin(existing, p, tol) {
			return points
		}
	}
	return append(points, p)
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

type contourGraphEdge struct {
	ID   int
	AKey string
	BKey string
	A    Point2D
	B    Point2D
}

type contourGraphIncident struct {
	EdgeID   int
	OtherKey string
	Angle    float64
}

type contourGraphNode struct {
	Key       string
	Point     Point2D
	Incidents []contourGraphIncident
}

type contourGraph struct {
	Nodes      map[string]*contourGraphNode
	Edges      []contourGraphEdge
	EdgeOrder  []int
	edgeLookup map[string]int
}

func buildContourGraph(segments []Segment2D, tol float64) contourGraph {
	if tol <= 0 {
		tol = 1e-6
	}

	graph := contourGraph{
		Nodes:      make(map[string]*contourGraphNode),
		Edges:      make([]contourGraphEdge, 0, len(segments)),
		EdgeOrder:  make([]int, 0, len(segments)),
		edgeLookup: make(map[string]int),
	}

	for _, segment := range segments {
		ak := pointKeyWithTolerance(segment.A, tol)
		bk := pointKeyWithTolerance(segment.B, tol)
		if ak == bk {
			continue
		}
		if contourEdgeKeyLess(bk, ak) {
			ak, bk = bk, ak
			segment.A, segment.B = segment.B, segment.A
		}
		key := contourEdgeKey(ak, bk)
		if _, ok := graph.edgeLookup[key]; ok {
			continue
		}

		id := len(graph.Edges)
		graph.edgeLookup[key] = id
		graph.Edges = append(graph.Edges, contourGraphEdge{ID: id, AKey: ak, BKey: bk, A: snapPointWithTolerance(segment.A, tol), B: snapPointWithTolerance(segment.B, tol)})
		graph.EdgeOrder = append(graph.EdgeOrder, id)
		graph.ensureNode(ak, graph.Edges[id].A)
		graph.ensureNode(bk, graph.Edges[id].B)
	}

	for _, edge := range graph.Edges {
		graph.Nodes[edge.AKey].Incidents = append(graph.Nodes[edge.AKey].Incidents, contourGraphIncident{EdgeID: edge.ID, OtherKey: edge.BKey})
		graph.Nodes[edge.BKey].Incidents = append(graph.Nodes[edge.BKey].Incidents, contourGraphIncident{EdgeID: edge.ID, OtherKey: edge.AKey})
	}

	for _, node := range graph.Nodes {
		for i := range node.Incidents {
			other := graph.Nodes[node.Incidents[i].OtherKey]
			node.Incidents[i].Angle = math.Atan2(other.Point.Y-node.Point.Y, other.Point.X-node.Point.X)
		}
		sort.SliceStable(node.Incidents, func(i, j int) bool {
			if !nearlyEqualAngle(node.Incidents[i].Angle, node.Incidents[j].Angle) {
				return node.Incidents[i].Angle < node.Incidents[j].Angle
			}
			if node.Incidents[i].OtherKey != node.Incidents[j].OtherKey {
				return node.Incidents[i].OtherKey < node.Incidents[j].OtherKey
			}
			return node.Incidents[i].EdgeID < node.Incidents[j].EdgeID
		})
	}

	sort.SliceStable(graph.EdgeOrder, func(i, j int) bool {
		a := graph.Edges[graph.EdgeOrder[i]]
		b := graph.Edges[graph.EdgeOrder[j]]
		if a.AKey != b.AKey {
			return a.AKey < b.AKey
		}
		if a.BKey != b.BKey {
			return a.BKey < b.BKey
		}
		return a.ID < b.ID
	})

	return graph
}

func (g *contourGraph) ensureNode(key string, point Point2D) {
	if _, ok := g.Nodes[key]; !ok {
		g.Nodes[key] = &contourGraphNode{Key: key, Point: point}
	}
}

func contourEdgeKey(aKey, bKey string) string {
	if contourEdgeKeyLess(bKey, aKey) {
		aKey, bKey = bKey, aKey
	}
	return aKey + "|" + bKey
}

func contourEdgeKeyLess(a, b string) bool {
	if a != b {
		return a < b
	}
	return false
}

func nearlyEqualAngle(a, b float64) bool {
	return math.Abs(a-b) <= 1e-12
}

func normalizePositiveAngle(angle float64) float64 {
	twoPi := 2 * math.Pi
	angle = math.Mod(angle, twoPi)
	if angle < 0 {
		angle += twoPi
	}
	return angle
}

func extractContourPathsWithTolerance(segments []Segment2D, tol float64) []contourPath {
	graph := buildContourGraph(segments, tol)
	paths := extractContourPathsFromGraph(graph, tol)
	if len(paths) > 0 {
		return paths
	}
	return extractContourPathsWithToleranceLegacy(segments, tol)
}

func extractContourPathsFromGraph(graph contourGraph, tol float64) []contourPath {
	if len(graph.Edges) == 0 {
		return nil
	}

	used := make([]bool, len(graph.Edges))
	paths := make([]contourPath, 0, len(graph.Edges))

	for _, nodeKey := range sortedGraphNodeKeys(graph) {
		if degreeOfNode(graph, nodeKey) != 1 {
			continue
		}
		node := graph.Nodes[nodeKey]
		if node == nil {
			continue
		}
		for _, incident := range node.Incidents {
			if used[incident.EdgeID] {
				continue
			}
			if path, edgeIDs, ok := walkGraphOpenPath(graph, incident.EdgeID, used, tol); ok {
				markContourEdgesUsed(used, edgeIDs)
				if len(path.Points) >= 2 {
					paths = append(paths, path)
				}
			}
		}
	}

	for _, edgeID := range graph.EdgeOrder {
		if used[edgeID] {
			continue
		}
		if path, edgeIDs, ok := walkGraphClosedPath(graph, edgeID, used, tol); ok {
			markContourEdgesUsed(used, edgeIDs)
			if len(path.Points) >= 3 {
				paths = append(paths, path)
				continue
			}
		}
		if path, edgeIDs, ok := walkGraphOpenPath(graph, edgeID, used, tol); ok {
			markContourEdgesUsed(used, edgeIDs)
			if len(path.Points) >= 2 {
				paths = append(paths, path)
			}
		}
	}

	for i := range paths {
		if paths[i].Closed {
			paths[i].Points = simplifyClosedContour(paths[i].Points, tol)
			if len(paths[i].Points) < 3 || !validateClosedContour(paths[i].Points, tol) {
				paths[i].Points = nil
			}
		} else {
			paths[i].Points = simplifyOpenContour(paths[i].Points, tol)
		}
	}

	filtered := make([]contourPath, 0, len(paths))
	for _, path := range paths {
		if path.Closed {
			if len(path.Points) >= 3 && validateClosedContour(path.Points, tol) {
				filtered = append(filtered, path)
			}
			continue
		}
		if len(path.Points) >= 2 {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func walkGraphOpenPath(graph contourGraph, startEdgeID int, used []bool, _ float64) (contourPath, []int, bool) {
	edge := graph.Edges[startEdgeID]
	startKey, nextKey := chooseGraphOpenOrientation(graph, edge, used)
	if startKey == "" || nextKey == "" {
		return contourPath{}, nil, false
	}

	points := []Point2D{graph.Nodes[startKey].Point, graph.Nodes[nextKey].Point}
	pathEdges := []int{startEdgeID}
	prevKey := startKey
	currentKey := nextKey
	maxSteps := len(graph.Edges) + 1

	for steps := 0; steps < maxSteps; steps++ {
		if degreeOfNode(graph, currentKey) != 2 {
			break
		}
		nextEdgeID, candidateKey, ok := chooseGraphNextEdge(graph, prevKey, currentKey, used, pathEdges, startKey)
		if !ok {
			break
		}
		pathEdges = append(pathEdges, nextEdgeID)
		points = append(points, graph.Nodes[candidateKey].Point)
		prevKey = currentKey
		currentKey = candidateKey
	}

	if len(points) >= 2 {
		return contourPath{Points: points, Closed: false}, pathEdges, true
	}
	return contourPath{}, nil, false
}

func walkGraphClosedPath(graph contourGraph, startEdgeID int, used []bool, tol float64) (contourPath, []int, bool) {
	edge := graph.Edges[startEdgeID]
	orientations := [][2]string{{edge.AKey, edge.BKey}, {edge.BKey, edge.AKey}}
	for _, orientation := range orientations {
		path, edgeIDs, ok := attemptGraphClosedPath(graph, startEdgeID, orientation[0], orientation[1], used)
		if ok && len(path.Points) >= 3 {
			path.Points = simplifyClosedContour(path.Points, tol)
			if len(path.Points) >= 3 && validateClosedContour(path.Points, tol) {
				return path, edgeIDs, true
			}
		}
	}
	return contourPath{}, nil, false
}

func attemptGraphClosedPath(graph contourGraph, startEdgeID int, startKey, nextKey string, used []bool) (contourPath, []int, bool) {
	startNode := graph.Nodes[startKey]
	nextNode := graph.Nodes[nextKey]
	if startNode == nil || nextNode == nil {
		return contourPath{}, nil, false
	}

	points := []Point2D{startNode.Point, nextNode.Point}
	pathEdges := []int{startEdgeID}
	prevKey := startKey
	currentKey := nextKey
	seen := map[string]bool{startKey: true, nextKey: true}
	maxSteps := len(graph.Edges) + 1

	for steps := 0; steps < maxSteps; steps++ {
		nextEdgeID, candidateKey, ok := chooseGraphNextEdge(graph, prevKey, currentKey, used, pathEdges, startKey)
		if !ok {
			return contourPath{}, nil, false
		}
		if candidateKey == startKey {
			return contourPath{Points: points, Closed: true}, append(pathEdges, nextEdgeID), true
		}
		if seen[candidateKey] {
			return contourPath{}, nil, false
		}
		seen[candidateKey] = true
		pathEdges = append(pathEdges, nextEdgeID)
		points = append(points, graph.Nodes[candidateKey].Point)
		prevKey = currentKey
		currentKey = candidateKey
	}

	return contourPath{}, nil, false
}

func chooseGraphOpenOrientation(graph contourGraph, edge contourGraphEdge, _ []bool) (string, string) {
	degA := degreeOfNode(graph, edge.AKey)
	degB := degreeOfNode(graph, edge.BKey)
	switch {
	case degA < degB:
		return edge.AKey, edge.BKey
	case degB < degA:
		return edge.BKey, edge.AKey
	case edge.AKey < edge.BKey:
		return edge.AKey, edge.BKey
	default:
		return edge.BKey, edge.AKey
	}
}

func chooseGraphNextEdge(graph contourGraph, prevKey, currentKey string, used []bool, pathEdges []int, _ string) (int, string, bool) {
	node := graph.Nodes[currentKey]
	if node == nil {
		return -1, "", false
	}
	prevNode := graph.Nodes[prevKey]
	if prevNode == nil {
		return -1, "", false
	}
	inAngle := math.Atan2(node.Point.Y-prevNode.Point.Y, node.Point.X-prevNode.Point.X)
	bestEdgeID := -1
	bestKey := ""
	bestTurn := math.Inf(1)
	for _, incident := range node.Incidents {
		if used[incident.EdgeID] || contourEdgeUsed(pathEdges, incident.EdgeID) || incident.OtherKey == prevKey {
			continue
		}
		other := graph.Nodes[incident.OtherKey]
		if other == nil {
			continue
		}
		turn := normalizePositiveAngle(math.Atan2(other.Point.Y-node.Point.Y, other.Point.X-node.Point.X) - inAngle)
		if turn < bestTurn-1e-12 || (nearlyEqualAngle(turn, bestTurn) && (incident.OtherKey < bestKey || (incident.OtherKey == bestKey && incident.EdgeID < bestEdgeID))) {
			bestTurn = turn
			bestEdgeID = incident.EdgeID
			bestKey = incident.OtherKey
		}
	}
	if bestEdgeID == -1 {
		return -1, "", false
	}
	return bestEdgeID, bestKey, true
}

func markContourEdgesUsed(used []bool, edgeIDs []int) {
	for _, edgeID := range edgeIDs {
		if edgeID >= 0 && edgeID < len(used) {
			used[edgeID] = true
		}
	}
}

func contourEdgeUsed(edgeIDs []int, edgeID int) bool {
	for _, id := range edgeIDs {
		if id == edgeID {
			return true
		}
	}
	return false
}

func degreeOfNode(graph contourGraph, key string) int {
	if node := graph.Nodes[key]; node != nil {
		return len(node.Incidents)
	}
	return 0
}

func sortedGraphNodeKeys(graph contourGraph) []string {
	keys := make([]string, 0, len(graph.Nodes))
	for key := range graph.Nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func extractContourPathsWithToleranceLegacy(segments []Segment2D, tol float64) []contourPath {
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

	if da != 2 && db == 2 {
		return a, b
	}
	if db != 2 && da == 2 {
		return b, a
	}
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
	rotated = simplifyCollinearLoop(rotated)
	rotated = dedupeConsecutivePointsWithTolerance(rotated, tol)
	if len(rotated) < 3 || !validateClosedContour(rotated, tol) {
		return nil
	}
	if signedArea(rotated) > 0 {
		reverseLoop(rotated)
	}
	return rotated
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

func validateClosedContour(points []Point2D, tol float64) bool {
	if len(points) < 3 {
		return false
	}
	if math.Abs(signedArea(points)) <= tol*tol {
		return false
	}

	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		if pointsEqualWithin(points[i], points[j], tol) {
			return false
		}
	}

	for i := 0; i < len(points); i++ {
		a1 := points[i]
		a2 := points[(i+1)%len(points)]
		for j := i + 1; j < len(points); j++ {
			if j == i || j == (i+1)%len(points) || (i == 0 && j == len(points)-1) {
				continue
			}
			b1 := points[j]
			b2 := points[(j+1)%len(points)]
			if segmentsIntersectWithinTolerance(a1, a2, b1, b2, tol) {
				return false
			}
		}
	}

	return true
}

func segmentsIntersectWithinTolerance(a1, a2, b1, b2 Point2D, tol float64) bool {
	if pointsEqualWithin(a1, b1, tol) || pointsEqualWithin(a1, b2, tol) || pointsEqualWithin(a2, b1, tol) || pointsEqualWithin(a2, b2, tol) {
		return false
	}

	o1 := contourOrientation(a1, a2, b1)
	o2 := contourOrientation(a1, a2, b2)
	o3 := contourOrientation(b1, b2, a1)
	o4 := contourOrientation(b1, b2, a2)

	if (o1 > tol && o2 < -tol || o1 < -tol && o2 > tol) && (o3 > tol && o4 < -tol || o3 < -tol && o4 > tol) {
		return true
	}

	if math.Abs(o1) <= tol && pointOnSegmentWithinTolerance(b1, a1, a2, tol) {
		return true
	}
	if math.Abs(o2) <= tol && pointOnSegmentWithinTolerance(b2, a1, a2, tol) {
		return true
	}
	if math.Abs(o3) <= tol && pointOnSegmentWithinTolerance(a1, b1, b2, tol) {
		return true
	}
	if math.Abs(o4) <= tol && pointOnSegmentWithinTolerance(a2, b1, b2, tol) {
		return true
	}

	return false
}

func contourOrientation(a, b, c Point2D) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

func buildContourHierarchy(paths []contourPath, tol float64) []ContourResult {
	if len(paths) == 0 {
		return nil
	}

	closedNodes := make([]*contourNode, 0, len(paths))
	openNodes := make([]*contourNode, 0)

	for _, path := range paths {
		if path.Closed {
			normalized := simplifyClosedContour(path.Points, tol)
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

	filteredOpen := openNodes[:0]
	for _, node := range openNodes {
		contained := false
		for _, closed := range closedNodes {
			if pointInPolygon(node.Centroid, closed.Contour.Points, tol) {
				contained = true
				break
			}
		}
		if !contained {
			filteredOpen = append(filteredOpen, node)
		}
	}
	openNodes = filteredOpen

	roots := make([]*contourNode, 0, len(closedNodes)+len(openNodes))
	for _, node := range closedNodes {
		if node.Parent == nil {
			roots = append(roots, node)
		}
	}
	roots = append(roots, openNodes...)
	sortContourNodes(roots, tol)

	results := make([]ContourResult, 0, len(roots))
	for _, root := range roots {
		results = append(results, contourNodeToResult(root, 0))
	}

	return results
}

func sortContourNodes(nodes []*contourNode, tol float64) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return contourNodeLess(nodes[i], nodes[j], tol)
	})
	for _, node := range nodes {
		if len(node.Children) > 0 {
			sortContourNodes(node.Children, tol)
		}
	}
}

func contourNodeLess(a, b *contourNode, tol float64) bool {
	if len(a.Contour.Points) == 0 || len(b.Contour.Points) == 0 {
		return len(a.Contour.Points) < len(b.Contour.Points)
	}
	pa := a.Contour.Points[0]
	pb := b.Contour.Points[0]
	if !pointsEqualWithin(pa, pb, tol) {
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
