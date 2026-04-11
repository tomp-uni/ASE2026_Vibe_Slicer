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

func closeEnough(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}
