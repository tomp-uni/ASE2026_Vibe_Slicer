package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
)

const epsilon = 1e-8

type Point2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
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

type GCodeConfig struct {
	StartGCode            string
	EndGCode              string
	LineWidthMM           float64
	FilamentDiameterMM    float64
	PrintTemperatureC     float64
	BuildPlateTempC       float64
	PrintSpeedMMs         float64
	ZHopSpeedMMs          float64
	ZHopHeightMM          float64
	TravelSpeedMMs        float64
	PrintAccelerationMMs2 float64
	RetractionDistanceMM  float64
	RetractionSpeedMMs    float64
	RetractionMinTravelMM float64
}

type gcodeState struct {
	X           float64
	Y           float64
	Z           float64
	E           float64
	HasPosition bool
}

func main() {
	jsonInput := flag.String("json-in", "", "Path to slicer JSON input")
	gcodeOutput := flag.String("gcode-out", "", "Output .gcode file path")
	startGCode := flag.String("start-gcode", "", "G-code snippet inserted at beginning (supports \\n)")
	endGCode := flag.String("end-gcode", "", "G-code snippet appended at end (supports \\n)")
	lineWidth := flag.Float64("line-width", 0.4, "Extrusion line width in mm")
	filamentDiameter := flag.Float64("filament-diameter", 1.75, "Filament diameter in mm")
	printTemp := flag.Float64("print-temp", 200, "Printhead temperature in Celsius")
	bedTemp := flag.Float64("build-plate-temp", 60, "Build plate temperature in Celsius")
	printSpeed := flag.Float64("print-speed", 50, "XY print speed in mm/s")
	zHopSpeed := flag.Float64("z-hop-speed", 10, "Z axis speed in mm/s")
	zHopHeight := flag.Float64("z-hop-height", 0.2, "Z hop height in mm")
	travelSpeed := flag.Float64("travel-speed", 120, "Travel speed in mm/s")
	printAccel := flag.Float64("print-acceleration", 1000, "Print acceleration in mm/s^2")
	retractDist := flag.Float64("retraction-distance", 1.0, "Retraction distance in mm")
	retractSpeed := flag.Float64("retraction-speed", 35, "Retraction speed in mm/s")
	retractMinTravel := flag.Float64("retraction-min-travel", 2.0, "Minimum travel distance to trigger retraction in mm")
	flag.Parse()

	if strings.TrimSpace(*jsonInput) == "" || strings.TrimSpace(*gcodeOutput) == "" {
		exitWithError(errors.New("both -json-in and -gcode-out are required"))
	}

	cfg := GCodeConfig{
		StartGCode:            decodeEscapedNewlines(*startGCode),
		EndGCode:              decodeEscapedNewlines(*endGCode),
		LineWidthMM:           *lineWidth,
		FilamentDiameterMM:    *filamentDiameter,
		PrintTemperatureC:     *printTemp,
		BuildPlateTempC:       *bedTemp,
		PrintSpeedMMs:         *printSpeed,
		ZHopSpeedMMs:          *zHopSpeed,
		ZHopHeightMM:          *zHopHeight,
		TravelSpeedMMs:        *travelSpeed,
		PrintAccelerationMMs2: *printAccel,
		RetractionDistanceMM:  *retractDist,
		RetractionSpeedMMs:    *retractSpeed,
		RetractionMinTravelMM: *retractMinTravel,
	}

	if err := generateGCodeFromJSON(*jsonInput, *gcodeOutput, cfg); err != nil {
		exitWithError(err)
	}
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func generateGCodeFromJSON(jsonPath, gcodePath string, cfg GCodeConfig) error {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed reading slicer JSON: %w", err)
	}

	var input SliceOutput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("failed parsing slicer JSON: %w", err)
	}
	if input.LayerHeight <= 0 {
		return errors.New("invalid slicer JSON: layer_height_mm must be > 0")
	}
	if err := validateGCodeConfig(cfg); err != nil {
		return err
	}

	gcode := buildGCode(input, cfg)
	if err := os.WriteFile(gcodePath, []byte(gcode), 0o644); err != nil {
		return fmt.Errorf("failed writing G-code file: %w", err)
	}
	return nil
}

func validateGCodeConfig(cfg GCodeConfig) error {
	if cfg.LineWidthMM <= 0 {
		return errors.New("-line-width must be > 0")
	}
	if cfg.FilamentDiameterMM <= 0 {
		return errors.New("-filament-diameter must be > 0")
	}
	if cfg.PrintSpeedMMs <= 0 || cfg.ZHopSpeedMMs <= 0 || cfg.TravelSpeedMMs <= 0 {
		return errors.New("-print-speed, -z-hop-speed and -travel-speed must be > 0")
	}
	if cfg.ZHopHeightMM < 0 {
		return errors.New("-z-hop-height must be >= 0")
	}
	if cfg.PrintAccelerationMMs2 <= 0 {
		return errors.New("-print-acceleration must be > 0")
	}
	if cfg.RetractionDistanceMM < 0 || cfg.RetractionMinTravelMM < 0 {
		return errors.New("-retraction-distance and -retraction-min-travel must be >= 0")
	}
	if cfg.RetractionSpeedMMs <= 0 {
		return errors.New("-retraction-speed must be > 0")
	}
	return nil
}

func buildGCode(input SliceOutput, cfg GCodeConfig) string {
	var b strings.Builder
	state := gcodeState{}

	appendCustomBlock(&b, cfg.StartGCode)
	b.WriteString("; Generated by ASE2026_Vibe_Slicer create_g_code\n")
	b.WriteString(fmt.Sprintf("; Source: %s\n", input.Input))
	b.WriteString("G21 ; mm units\n")
	b.WriteString("G90 ; absolute positioning\n")
	b.WriteString("M82 ; absolute extrusion\n")
	b.WriteString(fmt.Sprintf("M140 S%.0f\n", cfg.BuildPlateTempC))
	b.WriteString(fmt.Sprintf("M104 S%.0f\n", cfg.PrintTemperatureC))
	b.WriteString(fmt.Sprintf("M190 S%.0f\n", cfg.BuildPlateTempC))
	b.WriteString(fmt.Sprintf("M109 S%.0f\n", cfg.PrintTemperatureC))
	b.WriteString(fmt.Sprintf("M204 P%.0f T%.0f\n", cfg.PrintAccelerationMMs2, cfg.PrintAccelerationMMs2))
	b.WriteString("G92 E0\n")

	for idx, layer := range input.Layers {
		if len(layer.Points) < 2 {
			continue
		}

		state.Z = layer.Z
		b.WriteString(fmt.Sprintf("; LAYER %d Z=%.3f\n", idx, layer.Z))
		b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", layer.Z, cfg.ZHopSpeedMMs*60.0))
		moveToLayerStart(&b, &state, layer.Points[0], cfg)

		for i := 1; i <= len(layer.Points); i++ {
			next := layer.Points[i%len(layer.Points)]
			d := distance2D(state.X, state.Y, next.X, next.Y)
			if d <= epsilon {
				continue
			}
			state.E += extrusionForDistance(d, cfg.LineWidthMM, input.LayerHeight, cfg.FilamentDiameterMM)
			b.WriteString(fmt.Sprintf("G1 X%.3f Y%.3f E%.5f F%.0f\n", next.X, next.Y, state.E, cfg.PrintSpeedMMs*60.0))
			state.X, state.Y = next.X, next.Y
			state.HasPosition = true
		}
	}

	appendCustomBlock(&b, cfg.EndGCode)
	return b.String()
}

func moveToLayerStart(b *strings.Builder, state *gcodeState, start Point2D, cfg GCodeConfig) {
	if !state.HasPosition {
		b.WriteString(fmt.Sprintf("G0 X%.3f Y%.3f F%.0f\n", start.X, start.Y, cfg.TravelSpeedMMs*60.0))
		state.X, state.Y = start.X, start.Y
		state.HasPosition = true
		return
	}

	travelDistance := distance2D(state.X, state.Y, start.X, start.Y)
	if travelDistance <= epsilon {
		return
	}

	if travelDistance >= cfg.RetractionMinTravelMM && cfg.RetractionDistanceMM > 0 {
		state.E -= cfg.RetractionDistanceMM
		b.WriteString(fmt.Sprintf("G1 E%.5f F%.0f\n", state.E, cfg.RetractionSpeedMMs*60.0))
		if cfg.ZHopHeightMM > 0 {
			hopZ := state.Z + cfg.ZHopHeightMM
			b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", hopZ, cfg.ZHopSpeedMMs*60.0))
		}
		b.WriteString(fmt.Sprintf("G0 X%.3f Y%.3f F%.0f\n", start.X, start.Y, cfg.TravelSpeedMMs*60.0))
		if cfg.ZHopHeightMM > 0 {
			b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", state.Z, cfg.ZHopSpeedMMs*60.0))
		}
		state.E += cfg.RetractionDistanceMM
		b.WriteString(fmt.Sprintf("G1 E%.5f F%.0f\n", state.E, cfg.RetractionSpeedMMs*60.0))
	} else {
		b.WriteString(fmt.Sprintf("G0 X%.3f Y%.3f F%.0f\n", start.X, start.Y, cfg.TravelSpeedMMs*60.0))
	}

	state.X, state.Y = start.X, start.Y
}

func appendCustomBlock(b *strings.Builder, block string) {
	block = strings.TrimSpace(block)
	if block == "" {
		return
	}
	b.WriteString(block)
	if !strings.HasSuffix(block, "\n") {
		b.WriteString("\n")
	}
}

func extrusionForDistance(distance, lineWidth, layerHeight, filamentDiameter float64) float64 {
	filamentArea := math.Pi * (filamentDiameter / 2.0) * (filamentDiameter / 2.0)
	lineArea := lineWidth * layerHeight
	return (lineArea * distance) / filamentArea
}

func distance2D(x1, y1, x2, y2 float64) float64 {
	return math.Hypot(x2-x1, y2-y1)
}

func decodeEscapedNewlines(s string) string {
	return strings.ReplaceAll(s, "\\n", "\n")
}
