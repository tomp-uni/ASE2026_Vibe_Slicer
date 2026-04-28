package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateGCodeFromJSON(t *testing.T) {
	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "slices.json")
	gcodePath := filepath.Join(tempDir, "out.gcode")

	jsonData := `{
  "input": "cube_10.stl",
  "layer_height_mm": 0.2,
  "layers": [
    {
	  "z": 0.0,
      "points": [
        {"x": 0, "y": 0},
        {"x": 0, "y": 10},
        {"x": 10, "y": 10},
        {"x": 10, "y": 0}
      ]
	},
	{
	  "z": 0.2,
	  "points": [
		{"x": 0, "y": 0},
		{"x": 0, "y": 10},
		{"x": 10, "y": 10},
		{"x": 10, "y": 0}
	  ]
    }
  ]
}`

	if err := os.WriteFile(jsonPath, []byte(jsonData), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}

	cfg := GCodeConfig{
		StartGCode:            "G28",
		EndGCode:              "M84",
		BuildPlateOffsetXMM:   2,
		BuildPlateOffsetYMM:   3,
		BuildPlateOffsetZMM:   1,
		OuterWallLines:        1,
		LineWidthMM:           0.4,
		FilamentDiameterMM:    1.75,
		PrintTemperatureC:     200,
		BuildPlateTempC:       60,
		PrintSpeedMMs:         50,
		ZHopSpeedMMs:          10,
		ZHopHeightMM:          0.2,
		TravelSpeedMMs:        120,
		PrintAccelerationMMs2: 1000,
		RetractionDistanceMM:  1.0,
		RetractionSpeedMMs:    35,
		RetractionMinTravelMM: 2.0,
	}

	if err := generateGCodeFromJSON(jsonPath, gcodePath, cfg); err != nil {
		t.Fatalf("generateGCodeFromJSON failed: %v", err)
	}

	out, err := os.ReadFile(gcodePath)
	if err != nil {
		t.Fatalf("read generated gcode: %v", err)
	}
	content := string(out)

	checks := []string{
		"G28",
		"M140 S60",
		"M105",
		"M104 S200",
		"; LAYER 0 Z=0.200",
		"G0 Z1.200 F600",
		"G0 X2.000 Y3.000 F7200",
		"G1 X2.000 Y13.000 E",
		"G1 X12.000 Y13.000 E",
		"G1 X12.000 Y3.000 E",
		"M84",
	}
	for _, needle := range checks {
		if !strings.Contains(content, needle) {
			t.Fatalf("expected generated gcode to contain %q", needle)
		}
	}

	m140Idx := strings.Index(content, "M140 S60")
	firstM105Idx := strings.Index(content, "M105")
	m190Idx := strings.Index(content, "M190 S60")
	m104Idx := strings.Index(content, "M104 S200")
	if m140Idx == -1 || firstM105Idx == -1 || m190Idx == -1 || m104Idx == -1 {
		t.Fatalf("expected temperature setup commands to exist")
	}
	secondSearchStart := m104Idx + len("M104 S200")
	rest := content[secondSearchStart:]
	relSecondM105Idx := strings.Index(rest, "M105")
	if relSecondM105Idx == -1 {
		t.Fatalf("expected second M105 after M104")
	}
	secondM105Idx := secondSearchStart + relSecondM105Idx
	m109Idx := strings.Index(content, "M109 S200")
	if m109Idx == -1 {
		t.Fatalf("expected M109 to exist")
	}
	if !(m140Idx < firstM105Idx && firstM105Idx < m190Idx && m190Idx < m104Idx && m104Idx < secondM105Idx && secondM105Idx < m109Idx) {
		t.Fatalf("expected command order M140 -> M105 -> M190 -> M104 -> M105 -> M109")
	}

	startIdx := strings.Index(content, "G28")
	if startIdx == -1 {
		t.Fatalf("expected both M109 and StartGCode markers to exist")
	}
	if startIdx <= m109Idx {
		t.Fatalf("expected StartGCode to be inserted after M109")
	}

	bedOffIdx := strings.Index(content, "M140 S0")
	endIdx := strings.Index(content, "M84")
	hotendOffAfterEnd := strings.LastIndex(content, "M104 S0")
	if bedOffIdx == -1 || endIdx == -1 || hotendOffAfterEnd == -1 {
		t.Fatalf("expected M140 S0, M84 and M104 S0 to exist")
	}
	if !(bedOffIdx < endIdx && endIdx < hotendOffAfterEnd) {
		t.Fatalf("expected ordering M140 S0 -> endGCode -> M104 S0")
	}
}

func TestBuildGCodeDoesNotShiftAllLayersUp(t *testing.T) {
	input := SliceOutput{
		Input:       "cube_10.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{Z: 0.0, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.2, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 10.0, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        1,
		LineWidthMM:           0.4,
		FilamentDiameterMM:    1.75,
		PrintTemperatureC:     200,
		BuildPlateTempC:       60,
		PrintSpeedMMs:         50,
		ZHopSpeedMMs:          10,
		TravelSpeedMMs:        120,
		PrintAccelerationMMs2: 1000,
		RetractionSpeedMMs:    35,
	}

	gcode := buildGCode(input, cfg)

	if strings.Contains(gcode, "; LAYER 0 Z=0.000") {
		t.Fatalf("expected Z=0.000 layer to be skipped")
	}
	if !strings.Contains(gcode, "; LAYER 0 Z=0.200") {
		t.Fatalf("expected first printed layer at Z=0.200")
	}
	if !strings.Contains(gcode, "; LAYER 1 Z=10.000") {
		t.Fatalf("expected top layer to remain Z=10.000 (no global +layer height shift)")
	}
	if strings.Contains(gcode, "; LAYER 1 Z=10.200") {
		t.Fatalf("did not expect globally shifted top layer at Z=10.200")
	}
}

func TestSolidLayerPlacementAlternatesBottomAndTop(t *testing.T) {
	cases := []struct {
		name         string
		printedIdx   int
		totalPrinted int
		bottomLayers int
		topLayers    int
		wantActive   bool
		wantRegion   string
		wantSeq      int
		wantAngle    float64
	}{
		{name: "bottom first", printedIdx: 0, totalPrinted: 4, bottomLayers: 2, topLayers: 2, wantActive: true, wantRegion: "BOTTOM", wantSeq: 0, wantAngle: 45},
		{name: "bottom second", printedIdx: 1, totalPrinted: 4, bottomLayers: 2, topLayers: 2, wantActive: true, wantRegion: "BOTTOM", wantSeq: 1, wantAngle: -45},
		{name: "top first", printedIdx: 2, totalPrinted: 4, bottomLayers: 2, topLayers: 2, wantActive: true, wantRegion: "TOP", wantSeq: 0, wantAngle: 45},
		{name: "top second", printedIdx: 3, totalPrinted: 4, bottomLayers: 2, topLayers: 2, wantActive: true, wantRegion: "TOP", wantSeq: 1, wantAngle: -45},
		{name: "middle not solid", printedIdx: 1, totalPrinted: 5, bottomLayers: 1, topLayers: 1, wantActive: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := solidLayerPlacementForIndex(tc.printedIdx, tc.totalPrinted, tc.bottomLayers, tc.topLayers)
			if got.Active != tc.wantActive {
				t.Fatalf("expected active=%v got=%v", tc.wantActive, got.Active)
			}
			if !tc.wantActive {
				return
			}
			if got.Region != tc.wantRegion || got.SequenceIndex != tc.wantSeq || got.AngleDeg != tc.wantAngle {
				t.Fatalf("unexpected placement: got=%+v want region=%s seq=%d angle=%.0f", got, tc.wantRegion, tc.wantSeq, tc.wantAngle)
			}
		})
	}
}

func TestBuildSolidFillSegmentsFollowsAngle(t *testing.T) {
	square := []Point2D{
		{X: 0, Y: 0},
		{X: 0, Y: 10},
		{X: 10, Y: 10},
		{X: 10, Y: 0},
	}

	pos := buildSolidFillSegments(square, 5, 45)
	neg := buildSolidFillSegments(square, 5, -45)

	if len(pos) == 0 || len(neg) == 0 {
		t.Fatalf("expected solid fill segments for both angles")
	}

	checkSlope := func(seg fillSegment, wantPositive bool) {
		dx := seg.End.X - seg.Start.X
		dy := seg.End.Y - seg.Start.Y
		if math.Abs(dx) <= epsilon {
			t.Fatalf("expected diagonal segment, got dx=0 segment=%+v", seg)
		}
		if wantPositive {
			if math.Abs((dy/dx)-1) > 1e-6 {
				t.Fatalf("expected +45 degree segment, got slope=%.6f segment=%+v", dy/dx, seg)
			}
			return
		}
		if math.Abs((dy/dx)+1) > 1e-6 {
			t.Fatalf("expected -45 degree segment, got slope=%.6f segment=%+v", dy/dx, seg)
		}
	}

	checkSlope(pos[0], true)
	checkSlope(neg[0], false)
}

func TestOuterWallLinesInsetInward(t *testing.T) {
	input := SliceOutput{
		Input:       "cube_10.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{Z: 0.2, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        2,
		SolidBottomLayers:     0,
		SolidTopLayers:        0,
		LineWidthMM:           0.4,
		FilamentDiameterMM:    1.75,
		PrintTemperatureC:     200,
		BuildPlateTempC:       60,
		PrintSpeedMMs:         50,
		ZHopSpeedMMs:          10,
		TravelSpeedMMs:        120,
		PrintAccelerationMMs2: 1000,
		RetractionSpeedMMs:    35,
		RetractionMinTravelMM: 100,
	}

	gcode := buildGCode(input, cfg)

	want := []string{
		"G0 X0.000 Y0.000 F7200",
		"G1 X0.000 Y10.000",
		"G1 X10.000 Y10.000",
		"G1 X10.000 Y0.000",
		"G0 X0.400 Y0.400 F7200",
		"G1 X0.400 Y9.600",
		"G1 X9.600 Y9.600",
		"G1 X9.600 Y0.400",
	}
	for _, needle := range want {
		if !strings.Contains(gcode, needle) {
			t.Fatalf("expected gcode to contain %q", needle)
		}
	}

	outerIdx := strings.Index(gcode, "G0 X0.000 Y0.000 F7200")
	innerIdx := strings.Index(gcode, "G0 X0.400 Y0.400 F7200")
	if outerIdx == -1 || innerIdx == -1 || innerIdx <= outerIdx {
		t.Fatalf("expected inner wall to be emitted after the outer wall")
	}
}
