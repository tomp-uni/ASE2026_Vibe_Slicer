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
		"G0 X2.200 Y3.200 F7200",
		"G1 X2.200 Y12.800 E",
		"G1 X11.800 Y12.800 E",
		"G1 X11.800 Y3.200 E",
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

func TestBuildGCodeUsesContourHierarchyWhenPointsMissing(t *testing.T) {
	input := SliceOutput{
		Input:       "hierarchy.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{
				Z: 0.2,
				Contours: []ContourResult{
					{
						Closed: true,
						Role:   "outer",
						Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}},
						Children: []ContourResult{
							{Closed: true, Role: "hole", Points: []Point2D{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}}},
						},
					},
				},
			},
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
		RetractionMinTravelMM: 100,
	}

	gcode := buildGCode(input, cfg)

	if !strings.Contains(gcode, "; LAYER 0 Z=0.200") {
		t.Fatalf("expected hierarchy-only input to generate a layer")
	}
	if !strings.Contains(gcode, "G0 X0.200 Y0.200 F7200") {
		t.Fatalf("expected outer contour from hierarchy to be used for toolpath generation")
	}
	if strings.Contains(gcode, "X3.200 Y3.200") {
		t.Fatalf("did not expect the hole contour to be merged into the first toolpath")
	}
}

func TestBuildGCodeHandlesHierarchyHolesExplicitly(t *testing.T) {
	input := SliceOutput{
		Input:       "hole.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{
				Z: 0.2,
				Contours: []ContourResult{
					{
						Closed: true,
						Role:   "outer",
						Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}},
						Children: []ContourResult{{
							Closed: true,
							Role:   "hole",
							Points: []Point2D{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}},
						}},
					},
				},
			},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        1,
		SolidBottomLayers:     1,
		SolidTopLayers:        0,
		Infill:                false,
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

	for _, needle := range []string{
		"G0 X0.200 Y0.200 F7200",
		"G0 X2.800 Y2.800 F7200",
	} {
		if !strings.Contains(gcode, needle) {
			t.Fatalf("expected hierarchy-aware gcode to contain %q", needle)
		}
	}

	outerWallIdx := strings.Index(gcode, "G0 X0.200 Y0.200 F7200")
	holeWallIdx := strings.Index(gcode, "G0 X2.800 Y2.800 F7200")
	fillIdx := strings.Index(gcode, "; SOLID BOTTOM LAYER 0 ANGLE=45")
	if outerWallIdx == -1 || holeWallIdx == -1 || fillIdx == -1 {
		t.Fatalf("expected outer wall, hole wall and solid fill sections to exist")
	}
	if !(outerWallIdx < holeWallIdx && holeWallIdx < fillIdx) {
		t.Fatalf("expected walls to be emitted before fill, got outer=%d hole=%d fill=%d", outerWallIdx, holeWallIdx, fillIdx)
	}
}

func TestBuildSolidFillSegmentsFromContourHierarchyRespectsHoles(t *testing.T) {
	contours := []ContourResult{
		{
			Closed: true,
			Role:   "outer",
			Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}},
			Children: []ContourResult{{
				Closed: true,
				Role:   "hole",
				Points: []Point2D{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}},
			}},
		},
	}

	segments := buildSolidFillSegmentsFromContours(contours, 1, 0)
	if len(segments) == 0 {
		t.Fatal("expected fill segments for contour hierarchy")
	}

	var holeScanline int
	for _, seg := range segments {
		if math.Abs(seg.Start.Y-5) <= 1e-6 && math.Abs(seg.End.Y-5) <= 1e-6 {
			holeScanline++
			if !(math.Abs(seg.Start.X-0) <= 1e-6 && math.Abs(seg.End.X-3) <= 1e-6 || math.Abs(seg.Start.X-7) <= 1e-6 && math.Abs(seg.End.X-10) <= 1e-6 || math.Abs(seg.Start.X-3) <= 1e-6 && math.Abs(seg.End.X-0) <= 1e-6 || math.Abs(seg.Start.X-10) <= 1e-6 && math.Abs(seg.End.X-7) <= 1e-6) {
				t.Fatalf("unexpected hole scanline segment: %+v", seg)
			}
		}
	}

	if holeScanline != 2 {
		t.Fatalf("expected two fill segments around the hole at y=5, got %d", holeScanline)
	}
	for _, seg := range segments {
		if math.Abs(seg.Start.Y-5) <= 1e-6 && math.Abs(seg.End.Y-5) <= 1e-6 {
			if math.Abs(seg.Start.X-3) <= 1e-6 || math.Abs(seg.End.X-7) <= 1e-6 {
				t.Fatalf("did not expect fill to cross the hole interior: %+v", seg)
			}
		}
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

	checkSlope(pos[len(pos)/2], true)
	checkSlope(neg[len(neg)/2], false)
}

func TestBuildSolidFillSegmentsFromWallOffsetsUsesInnermostWallReference(t *testing.T) {
	contours := []ContourResult{{
		Closed: true,
		Role:   "outer",
		Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}},
	}}

	segments := buildSolidFillSegmentsFromWallOffsets(contours, 1, 0.4, 0.4, 0)
	if len(segments) == 0 {
		t.Fatal("expected fill segments from wall-offset contours")
	}

	first := segments[0]
	last := segments[len(segments)-1]
	if math.Abs(first.Start.Y-0.6) > 1e-6 || math.Abs(first.End.Y-0.6) > 1e-6 {
		t.Fatalf("expected fill to start half a line width inside the wall stack (0.6), got %+v", first)
	}
	if math.Abs(last.Start.Y-9.4) > 1e-6 || math.Abs(last.End.Y-9.4) > 1e-6 {
		t.Fatalf("expected fill to end half a line width inside the far wall stack (9.4), got %+v", last)
	}
	for _, seg := range segments {
		if math.Abs(seg.Start.Y-0.2) <= 1e-6 || math.Abs(seg.End.Y-0.2) <= 1e-6 {
			t.Fatalf("did not expect fill to overlap the inner-most wall centerline: %+v", seg)
		}
	}
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
		"G0 X0.200 Y0.200 F7200",
		"G1 X0.200 Y9.800",
		"G1 X9.800 Y9.800",
		"G1 X9.800 Y0.200",
		"G0 X0.600 Y0.600 F7200",
		"G1 X0.600 Y9.400",
		"G1 X9.400 Y9.400",
		"G1 X9.400 Y0.600",
	}
	for _, needle := range want {
		if !strings.Contains(gcode, needle) {
			t.Fatalf("expected gcode to contain %q", needle)
		}
	}

	outerIdx := strings.Index(gcode, "G0 X0.200 Y0.200 F7200")
	innerIdx := strings.Index(gcode, "G0 X0.600 Y0.600 F7200")
	if outerIdx == -1 || innerIdx == -1 || innerIdx <= outerIdx {
		t.Fatalf("expected inner wall to be emitted after the outer wall")
	}
}

func TestOffsetPolygonRejectsRunawaySecondWallOnComplexContour(t *testing.T) {
	points := []Point2D{
		{X: 0, Y: 0},
		{X: 0, Y: 20},
		{X: 20, Y: 20},
		{X: 20, Y: 10.509696},
		{X: 18.081898, Y: 10.508877},
		{X: 18.053923, Y: 10.508866},
		{X: 18.054669, Y: 8.828077},
		{X: 20, Y: 8.828908},
		{X: 20, Y: 0},
		{X: 14.241419, Y: 0},
		{X: 14.240423, Y: 1.891448},
		{X: 12.461678, Y: 1.891448},
		{X: 12.462613, Y: 0.046147},
		{X: 12.462637, Y: 0},
		{X: 6.72339, Y: 0},
		{X: 6.724323, Y: 1.891448},
		{X: 5.107357, Y: 1.891448},
		{X: 5.106483, Y: 0},
	}

	first := offsetPolygon(points, 0.2)
	if first == nil {
		t.Fatal("expected first wall offset to succeed")
	}
	second := offsetPolygon(first, 0.4)
	if second != nil {
		for _, p := range second {
			if math.Abs(p.X) > 300 || math.Abs(p.Y) > 300 {
				t.Fatalf("expected runaway second offset to be rejected, got point %+v", p)
			}
		}
		t.Fatalf("expected runaway second offset to be rejected for complex contour")
	}
}

func TestBrimPrintsOnInitialLayerWithOutwardOffset(t *testing.T) {
	input := SliceOutput{
		Input:       "cube_10.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{Z: 0.2, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.4, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        1,
		Brim:                  true,
		BrimLines:             2,
		SolidBottomLayers:     0,
		SolidTopLayers:        0,
		Infill:                false,
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

	if strings.Count(gcode, "; BRIM LAYER 0 LINES=2") != 1 {
		t.Fatalf("expected one brim section on the initial layer")
	}
	if strings.Count(gcode, "; BRIM LAYER") != 1 {
		t.Fatalf("expected brim to print only on the initial layer")
	}

	want := []string{
		"G0 X-0.200 Y-0.200 F7200",
		"G1 X-0.200 Y10.200",
		"G1 X10.200 Y10.200",
		"G1 X10.200 Y-0.200",
		"G0 X-0.600 Y-0.600 F7200",
		"G1 X-0.600 Y10.600",
		"G1 X10.600 Y10.600",
		"G1 X10.600 Y-0.600",
	}
	for _, needle := range want {
		if !strings.Contains(gcode, needle) {
			t.Fatalf("expected gcode to contain %q", needle)
		}
	}

	brimIdx := strings.Index(gcode, "; BRIM LAYER 0 LINES=2")
	if brimIdx == -1 {
		t.Fatalf("expected brim comment")
	}
	outerIdx := strings.Index(gcode, "G0 X0.200 Y0.200 F7200")
	if outerIdx == -1 || brimIdx <= outerIdx {
		t.Fatalf("expected brim to be emitted after the outer wall")
	}
}

func TestSkirtPrintsFirstAndKeepsFiveMillimeterGap(t *testing.T) {
	input := SliceOutput{
		Input:       "cube_10.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{Z: 0.2, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        1,
		Skirt:                 true,
		SkirtLines:            2,
		Brim:                  true,
		BrimLines:             2,
		SolidBottomLayers:     0,
		SolidTopLayers:        0,
		Infill:                false,
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

	if strings.Count(gcode, "; SKIRT LAYER 0 LINES=2") != 1 {
		t.Fatalf("expected one skirt section on the initial layer")
	}
	if strings.Count(gcode, "; SKIRT LAYER") != 1 {
		t.Fatalf("expected skirt to print only on the initial layer")
	}

	want := []string{
		"G0 X-6.000 Y-6.000 F7200",
		"G1 X-6.000 Y16.000",
		"G0 X-6.400 Y-6.400 F7200",
		"G1 X-6.400 Y16.400",
	}
	for _, needle := range want {
		if !strings.Contains(gcode, needle) {
			t.Fatalf("expected gcode to contain %q", needle)
		}
	}

	skirtIdx := strings.Index(gcode, "; SKIRT LAYER 0 LINES=2")
	wallIdx := strings.Index(gcode, "G0 X0.200 Y0.200 F7200")
	brimIdx := strings.Index(gcode, "; BRIM LAYER 0 LINES=2")
	if skirtIdx == -1 || wallIdx == -1 || brimIdx == -1 {
		t.Fatalf("expected skirt, wall and brim sections to exist")
	}
	if !(skirtIdx < wallIdx && skirtIdx < brimIdx) {
		t.Fatalf("expected skirt to be printed before walls and brim")
	}

	firstSkirt := strings.Index(gcode, "G0 X-6.000 Y-6.000 F7200")
	outerBrim := strings.Index(gcode, "G0 X-0.200 Y-0.200 F7200")
	if firstSkirt == -1 || outerBrim == -1 || firstSkirt >= outerBrim {
		t.Fatalf("expected skirt to remain outside the brim by at least 5 mm")
	}
}

func TestSolidFillBoundaryInsetByHalfLineWidth(t *testing.T) {
	square := []Point2D{
		{X: 0, Y: 0},
		{X: 0, Y: 10},
		{X: 10, Y: 10},
		{X: 10, Y: 0},
	}

	boundary := solidFillBoundary(square, 0.4)
	want := []Point2D{
		{X: 0.2, Y: 0.2},
		{X: 0.2, Y: 9.8},
		{X: 9.8, Y: 9.8},
		{X: 9.8, Y: 0.2},
	}

	if len(boundary) != len(want) {
		t.Fatalf("expected %d boundary points, got %d", len(want), len(boundary))
	}
	for i := range want {
		if math.Abs(boundary[i].X-want[i].X) > 1e-6 || math.Abs(boundary[i].Y-want[i].Y) > 1e-6 {
			t.Fatalf("unexpected boundary point %d: got=%+v want=%+v", i, boundary[i], want[i])
		}
	}
}

func TestSolidFillSegmentsReachInsetBoundary(t *testing.T) {
	boundary := []Point2D{
		{X: 0.2, Y: 0.2},
		{X: 0.2, Y: 9.8},
		{X: 9.8, Y: 9.8},
		{X: 9.8, Y: 0.2},
	}

	segments := buildSolidFillSegments(boundary, 0.4, 0)
	if len(segments) != 25 {
		t.Fatalf("expected 25 hatch segments, got %d", len(segments))
	}

	first := segments[0]
	last := segments[len(segments)-1]
	if math.Abs(first.Start.Y-0.2) > 1e-6 || math.Abs(first.End.Y-0.2) > 1e-6 {
		t.Fatalf("expected first hatch line to reach Y=0.2, got %+v", first)
	}
	if math.Abs(last.Start.Y-9.8) > 1e-6 || math.Abs(last.End.Y-9.8) > 1e-6 {
		t.Fatalf("expected last hatch line to reach Y=9.8, got %+v", last)
	}
}

func TestInfillSpacingFromDensity(t *testing.T) {
	if got := infillSpacingFromDensity(0.4, 100); math.Abs(got-0.4) > 1e-6 {
		t.Fatalf("expected 100%% density spacing to equal line width, got %.6f", got)
	}
	if got := infillSpacingFromDensity(0.4, 50); math.Abs(got-0.8) > 1e-6 {
		t.Fatalf("expected 50%% density spacing to be 0.8, got %.6f", got)
	}
	if got := infillSpacingFromDensity(0.4, 0); got != 0 {
		t.Fatalf("expected 0%% density spacing to be 0, got %.6f", got)
	}
}

func TestInfillOnlyPrintsBetweenBottomAndTopSolids(t *testing.T) {
	input := SliceOutput{
		Input:       "cube_10.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{Z: 0.2, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.4, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.6, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.8, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        1,
		SolidBottomLayers:     1,
		SolidTopLayers:        1,
		Infill:                true,
		InfillDensity:         50,
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

	if !strings.Contains(gcode, "; SOLID BOTTOM LAYER 0 ANGLE=45") {
		t.Fatalf("expected a solid bottom layer")
	}
	if !strings.Contains(gcode, "; INFILL LAYER 0 DENSITY=50 ANGLE=45") {
		t.Fatalf("expected first infill layer to use the first alternating angle")
	}
	if !strings.Contains(gcode, "; INFILL LAYER 1 DENSITY=50 ANGLE=-45") {
		t.Fatalf("expected second infill layer to alternate angle")
	}
	if !strings.Contains(gcode, "; SOLID TOP LAYER 0 ANGLE=45") {
		t.Fatalf("expected a solid top layer")
	}
	if strings.Count(gcode, "; INFILL LAYER") != 2 {
		t.Fatalf("expected infill only on the two middle layers")
	}
	if strings.Index(gcode, "; INFILL LAYER 0 DENSITY=50 ANGLE=45") < strings.Index(gcode, "; SOLID BOTTOM LAYER 0 ANGLE=45") {
		t.Fatalf("expected infill to start after the bottom solid layer")
	}
	if strings.Index(gcode, "; SOLID TOP LAYER 0 ANGLE=45") < strings.Index(gcode, "; INFILL LAYER 1 DENSITY=50 ANGLE=-45") {
		t.Fatalf("expected the top solid layer to come after infill")
	}
}

func TestCoolingFanPwmFromPercent(t *testing.T) {
	if got := coolingFanPwmFromPercent(0); got != 0 {
		t.Fatalf("expected 0%% to map to 0, got %d", got)
	}
	if got := coolingFanPwmFromPercent(50); got != 128 {
		t.Fatalf("expected 50%% to map to 128, got %d", got)
	}
	if got := coolingFanPwmFromPercent(100); got != 255 {
		t.Fatalf("expected 100%% to map to 255, got %d", got)
	}
}

func TestCoolingFanTurnsOnAtConfiguredLayerAndOffAtEnd(t *testing.T) {
	input := SliceOutput{
		Input:       "cube_10.stl",
		LayerHeight: 0.2,
		Layers: []LayerResult{
			{Z: 0.2, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.4, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
			{Z: 0.6, Points: []Point2D{{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0}}},
		},
	}

	cfg := GCodeConfig{
		OuterWallLines:        1,
		SolidBottomLayers:     0,
		SolidTopLayers:        0,
		Infill:                false,
		CoolingFan:            true,
		CoolingFanLayer:       1,
		CoolingFanSpeed:       50,
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

	if strings.Count(gcode, "M106 S128") != 1 {
		t.Fatalf("expected the cooling fan to turn on once with M106 S128")
	}
	if strings.Contains(gcode, "; LAYER 0 Z=0.200\nG0 Z0.200 F600\nM106 S128") {
		t.Fatalf("did not expect cooling fan to turn on before the configured layer")
	}
	if !strings.Contains(gcode, "; LAYER 1 Z=0.400\nG0 Z0.400 F600\nM106 S128") {
		t.Fatalf("expected cooling fan to turn on at layer 1")
	}
	bedOffIdx := strings.LastIndex(gcode, "M140 S0")
	fanOffIdx := strings.LastIndex(gcode, "M107")
	if bedOffIdx == -1 || fanOffIdx == -1 {
		t.Fatalf("expected both M140 S0 and M107 at shutdown")
	}
	if fanOffIdx <= bedOffIdx {
		t.Fatalf("expected M107 after M140 S0")
	}
}
