package main

import (
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
	if m109Idx == -1 || startIdx == -1 {
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
