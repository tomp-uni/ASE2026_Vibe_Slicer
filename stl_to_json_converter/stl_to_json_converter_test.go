package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSliceTrianglesASCII(t *testing.T) {
	stl := `solid test
  facet normal 0 0 0
    outer loop
      vertex 0 0 0
      vertex 1 0 1
      vertex 0 1 1
    endloop
  endfacet
endsolid test`

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "triangle.stl")
	if err := os.WriteFile(path, []byte(stl), 0o644); err != nil {
		t.Fatalf("write stl: %v", err)
	}

	tris, err := readSTL(path)
	if err != nil {
		t.Fatalf("readSTL failed: %v", err)
	}
	if len(tris) != 1 {
		t.Fatalf("expected 1 triangle, got %d", len(tris))
	}

	layers := sliceTriangles(tris, 0.2)
	if len(layers) == 0 {
		t.Fatal("expected at least one layer")
	}

	var found bool
	for _, layer := range layers {
		if closeEnough(layer.Z, 0.2) {
			found = true
			if len(layer.Points) != 2 {
				t.Fatalf("expected 2 points at z=0.2, got %d", len(layer.Points))
			}
		}
	}

	if !found {
		t.Fatal("expected layer at z=0.2")
	}
}

func TestSliceCubeMidLayerHasFourCorners(t *testing.T) {
	tris, err := readSTL(filepath.Join("..", "cube_10.stl"))
	if err != nil {
		t.Fatalf("readSTL cube_10.stl failed: %v", err)
	}

	layers := sliceTriangles(tris, 0.2)
	var mid *LayerResult
	for i := range layers {
		if closeEnough(layers[i].Z, 0.2) {
			mid = &layers[i]
			break
		}
	}
	if mid == nil {
		t.Fatal("expected layer at z=0.2")
	}
	if len(mid.Points) != 4 {
		t.Fatalf("expected 4 simplified points at z=0.2, got %d", len(mid.Points))
	}
	if len(mid.Contours) != 1 {
		t.Fatalf("expected 1 top-level contour at z=0.2, got %d", len(mid.Contours))
	}
	if !mid.Contours[0].Closed {
		t.Fatal("expected cube contour to be closed")
	}
	if mid.Contours[0].Role != "outer" {
		t.Fatalf("expected cube contour role outer, got %q", mid.Contours[0].Role)
	}

	expected := []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}
	for i, want := range expected {
		got := mid.Points[i]
		if !closeEnough(got.X, want.X) || !closeEnough(got.Y, want.Y) {
			t.Fatalf("point %d mismatch: got %+v want %+v", i, got, want)
		}
	}
}

func TestContourHierarchyPreserved(t *testing.T) {
	segments := append(squareSegments(0, 0, 10), squareSegments(3, 3, 7)...)
	paths := extractContourPathsWithTolerance(segments, 1e-6)
	if len(paths) != 2 {
		t.Fatalf("expected 2 contour paths, got %d", len(paths))
	}

	contours := buildContourHierarchy(paths, 1e-6)
	if len(contours) != 1 {
		t.Fatalf("expected 1 top-level contour, got %d", len(contours))
	}
	if !contours[0].Closed || contours[0].Role != "outer" {
		t.Fatalf("expected top-level contour to be a closed outer contour, got %+v", contours[0])
	}
	if len(contours[0].Children) != 1 {
		t.Fatalf("expected 1 child contour, got %d", len(contours[0].Children))
	}
	if !contours[0].Children[0].Closed || contours[0].Children[0].Role != "hole" {
		t.Fatalf("expected child contour to be a closed hole, got %+v", contours[0].Children[0])
	}

	flat := flattenContourHierarchy(contours)
	if len(flat) != 8 {
		t.Fatalf("expected 8 flattened points, got %d", len(flat))
	}
}

func TestOpenChainPreserved(t *testing.T) {
	segments := []Segment2D{
		{A: Point2D{X: 0, Y: 0}, B: Point2D{X: 1, Y: 0}},
		{A: Point2D{X: 1, Y: 0}, B: Point2D{X: 1, Y: 1}},
	}

	paths := extractContourPathsWithTolerance(segments, 1e-6)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d", len(paths))
	}
	if paths[0].Closed {
		t.Fatal("expected open chain to remain open")
	}

	contours := buildContourHierarchy(paths, 1e-6)
	if len(contours) != 1 {
		t.Fatalf("expected 1 contour, got %d", len(contours))
	}
	if contours[0].Role != "open" {
		t.Fatalf("expected open contour role, got %q", contours[0].Role)
	}
	if len(contours[0].Points) != 3 {
		t.Fatalf("expected 3 points in open chain, got %d", len(contours[0].Points))
	}
}

func TestNearlyCoincidentVerticesStitch(t *testing.T) {
	segments := []Segment2D{
		{A: Point2D{X: 0, Y: 0}, B: Point2D{X: 10, Y: 0}},
		{A: Point2D{X: 10 + 2e-7, Y: 0}, B: Point2D{X: 10, Y: 10}},
		{A: Point2D{X: 10, Y: 10}, B: Point2D{X: 0, Y: 10}},
		{A: Point2D{X: 0, Y: 10}, B: Point2D{X: 0, Y: 0}},
	}

	paths := extractContourPathsWithTolerance(segments, 1e-6)
	if len(paths) != 1 {
		t.Fatalf("expected a single stitched contour, got %d", len(paths))
	}
	if !paths[0].Closed {
		t.Fatal("expected nearly coincident vertices to stitch into a closed contour")
	}
}

func TestCoplanarTrianglesProduceClosedLoop(t *testing.T) {
	tris := []Triangle{
		{A: Vec3{X: 0, Y: 0, Z: 0}, B: Vec3{X: 10, Y: 0, Z: 0}, C: Vec3{X: 10, Y: 10, Z: 0}},
		{A: Vec3{X: 0, Y: 0, Z: 0}, B: Vec3{X: 10, Y: 10, Z: 0}, C: Vec3{X: 0, Y: 10, Z: 0}},
	}

	layers := sliceTriangles(tris, 0.2)
	if len(layers) != 1 {
		t.Fatalf("expected 1 layer for coplanar triangles, got %d", len(layers))
	}
	if len(layers[0].Contours) != 1 {
		t.Fatalf("expected 1 contour from coplanar triangles, got %d", len(layers[0].Contours))
	}
	if !layers[0].Contours[0].Closed || layers[0].Contours[0].Role != "outer" {
		t.Fatalf("expected a closed outer contour, got %+v", layers[0].Contours[0])
	}
	if len(layers[0].Points) != 4 {
		t.Fatalf("expected 4 points from coplanar square, got %d", len(layers[0].Points))
	}
}

func TestShallowAngleContourExtractionRemainsFinite(t *testing.T) {
	segments := []Segment2D{
		{A: Point2D{X: 0, Y: 0}, B: Point2D{X: 100, Y: 0.001}},
		{A: Point2D{X: 100, Y: 0.001}, B: Point2D{X: 100.2, Y: 10}},
		{A: Point2D{X: 100.2, Y: 10}, B: Point2D{X: 0.2, Y: 10.001}},
		{A: Point2D{X: 0.2, Y: 10.001}, B: Point2D{X: 0, Y: 0}},
	}

	paths := extractContourPathsWithTolerance(segments, 1e-5)
	if len(paths) != 1 {
		t.Fatalf("expected 1 contour path, got %d", len(paths))
	}
	if !paths[0].Closed {
		t.Fatal("expected shallow-angle contour to remain closed")
	}
	for _, p := range paths[0].Points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			t.Fatalf("unexpected non-finite point: %+v", p)
		}
		if math.Abs(p.X) > 200 || math.Abs(p.Y) > 200 {
			t.Fatalf("unexpected runaway coordinate: %+v", p)
		}
	}
}

func TestBranchingGraphPreservesLoopAndSpur(t *testing.T) {
	segments := append(squareSegments(0, 0, 10), Segment2D{A: Point2D{X: 10, Y: 10}, B: Point2D{X: 15, Y: 15}})

	paths := extractContourPathsWithTolerance(segments, 1e-6)
	var closedCount, openCount int
	for _, path := range paths {
		if path.Closed {
			closedCount++
		} else {
			openCount++
		}
		for _, p := range path.Points {
			if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
				t.Fatalf("unexpected non-finite point in branching graph: %+v", p)
			}
		}
	}
	if closedCount != 1 {
		t.Fatalf("expected 1 closed contour, got %d (paths=%+v)", closedCount, paths)
	}
	if openCount != 1 {
		t.Fatalf("expected 1 open spur, got %d (paths=%+v)", openCount, paths)
	}
}

func squareSegments(minX, minY, maxCoord float64) []Segment2D {
	return []Segment2D{
		{A: Point2D{X: minX, Y: minY}, B: Point2D{X: minX, Y: maxCoord}},
		{A: Point2D{X: minX, Y: maxCoord}, B: Point2D{X: maxCoord, Y: maxCoord}},
		{A: Point2D{X: maxCoord, Y: maxCoord}, B: Point2D{X: maxCoord, Y: minY}},
		{A: Point2D{X: maxCoord, Y: minY}, B: Point2D{X: minX, Y: minY}},
	}
}

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}
