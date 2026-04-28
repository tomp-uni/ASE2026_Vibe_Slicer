package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
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
	BuildPlateOffsetXMM   float64
	BuildPlateOffsetYMM   float64
	BuildPlateOffsetZMM   float64
	SolidBottomLayers     int
	SolidTopLayers        int
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
	startGCode := flag.String("start-gcode", "M107 ;Start with the fan off\n"+
		"G21 ;Set units to millimeters\n"+
		"G91 ;Change to relative positioning mode for retract filament and nozzle lifting\n"+
		"G1 F200 E-3 ;Retract 3mm filament for a clean start\n"+
		"G92 E0 ;Zero the extruded length\n"+
		"G1 F1000 Z5 ;Lift the nozzle 5mm before homing axes\n"+
		"G90 ;Absolute positioning\n"+
		"M82 ;Set extruder to absolute mode too\n"+
		"G28 X0 Y0 ;First move X/Y to min endstops\n"+
		"G28 Z0 ;Then move Z to min endstops\n"+
		"G1 F1000 Z15 ;After homing lift the nozzle 15mm before start printing", "G-code snippet inserted at beginning (supports \\n)")
	endGCode := flag.String("end-gcode", "G91 ;Change to relative positioning mode for filament retraction and nozzle lifting\n"+
		"G1 F200 E-4 ;Retract the filament a bit before lifting the nozzle\n"+
		"G1 F1000 Z5 ;Lift nozzle 5mm\n"+
		"G90 ;Change to absolute positioning mode to prepare for part rermoval\n"+
		"G1 X0 Y400 ;Move the print to max y pos for part rermoval\n"+
		"M104 S0 ; Turn off hotend\n"+
		"M106 S0 ; Turn off cooling fan\n"+
		"M140 S0 ; Turn off bed\n"+
		"M84 ; Disable motors", "G-code snippet appended at end (supports \\n)")
	offsetX := flag.Float64("offset-x", 180, "Build plate X offset in mm")
	offsetY := flag.Float64("offset-y", 180, "Build plate Y offset in mm")
	offsetZ := flag.Float64("offset-z", 0, "Build plate Z offset in mm")
	solidBottomLayers := flag.Int("solid-bottom-layers", 3, "Number of fully printed solid layers at the bottom")
	solidTopLayers := flag.Int("solid-top-layers", 3, "Number of fully printed solid layers at the top")
	lineWidth := flag.Float64("line-width", 0.4, "Extrusion line width in mm")
	filamentDiameter := flag.Float64("filament-diameter", 1.75, "Filament diameter in mm")
	printTemp := flag.Float64("print-temp", 200, "Printhead temperature in Celsius")
	bedTemp := flag.Float64("build-plate-temp", 60, "Build plate temperature in Celsius")
	printSpeed := flag.Float64("print-speed", 30, "XY print speed in mm/s")
	zHopSpeed := flag.Float64("z-hop-speed", 10, "Z axis speed in mm/s")
	zHopHeight := flag.Float64("z-hop-height", 0.075, "Z hop height in mm")
	travelSpeed := flag.Float64("travel-speed", 125, "Travel speed in mm/s")
	printAccel := flag.Float64("print-acceleration", 1800, "Print acceleration in mm/s^2")
	retractDist := flag.Float64("retraction-distance", 3.0, "Retraction distance in mm")
	retractSpeed := flag.Float64("retraction-speed", 70, "Retraction speed in mm/s")
	retractMinTravel := flag.Float64("retraction-min-travel", 1.5, "Minimum travel distance to trigger retraction in mm")
	flag.Parse()

	if strings.TrimSpace(*jsonInput) == "" || strings.TrimSpace(*gcodeOutput) == "" {
		exitWithError(errors.New("both -json-in and -gcode-out are required"))
	}

	cfg := GCodeConfig{
		StartGCode:            decodeEscapedNewlines(*startGCode),
		EndGCode:              decodeEscapedNewlines(*endGCode),
		BuildPlateOffsetXMM:   *offsetX,
		BuildPlateOffsetYMM:   *offsetY,
		BuildPlateOffsetZMM:   *offsetZ,
		SolidBottomLayers:     *solidBottomLayers,
		SolidTopLayers:        *solidTopLayers,
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
	_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
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
	if cfg.SolidBottomLayers < 0 || cfg.SolidTopLayers < 0 {
		return errors.New("-solid-bottom-layers and -solid-top-layers must be >= 0")
	}
	return nil
}

func buildGCode(input SliceOutput, cfg GCodeConfig) string {
	var b strings.Builder
	state := gcodeState{}
	printableLayers := filterPrintableLayers(input.Layers, input.LayerHeight)

	b.WriteString("; Generated by ASE2026_Vibe_Slicer create_g_code\n")
	b.WriteString(fmt.Sprintf("; Source: %s\n", input.Input))
	b.WriteString("G21 ; mm units\n")
	b.WriteString("G90 ; absolute positioning\n")
	b.WriteString("M82 ; absolute extrusion\n")
	b.WriteString(fmt.Sprintf("M140 S%.0f\n", cfg.BuildPlateTempC))
	b.WriteString("M105\n")
	b.WriteString(fmt.Sprintf("M190 S%.0f\n", cfg.BuildPlateTempC))
	b.WriteString(fmt.Sprintf("M104 S%.0f\n", cfg.PrintTemperatureC))
	b.WriteString("M105\n")
	b.WriteString(fmt.Sprintf("M109 S%.0f\n", cfg.PrintTemperatureC))
	appendCustomBlock(&b, cfg.StartGCode)
	b.WriteString(fmt.Sprintf("M204 P%.0f T%.0f\n", cfg.PrintAccelerationMMs2, cfg.PrintAccelerationMMs2))
	b.WriteString("G92 E0\n")

	for emittedLayerIdx, layer := range printableLayers {

		state.Z = layer.Z
		b.WriteString(fmt.Sprintf("; LAYER %d Z=%.3f\n", emittedLayerIdx, layer.Z))
		b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", layer.Z+cfg.BuildPlateOffsetZMM, cfg.ZHopSpeedMMs*60.0))
		emitContourLoop(&b, &state, layer.Points, cfg, input.LayerHeight)

		if solid := solidLayerPlacementForIndex(emittedLayerIdx, len(printableLayers), cfg.SolidBottomLayers, cfg.SolidTopLayers); solid.Active {
			b.WriteString(fmt.Sprintf("; SOLID %s LAYER %d ANGLE=%.0f\n", solid.Region, solid.SequenceIndex, solid.AngleDeg))
			emitSolidFill(&b, &state, layer.Points, cfg, input.LayerHeight, solid.AngleDeg)
		}
	}

	b.WriteString("M140 S0\n")
	appendCustomBlock(&b, cfg.EndGCode)
	b.WriteString("M104 S0\n")
	return b.String()
}

func filterPrintableLayers(layers []LayerResult, layerHeight float64) []LayerResult {
	printable := make([]LayerResult, 0, len(layers))
	for _, layer := range layers {
		if len(layer.Points) < 2 {
			continue
		}
		if layer.Z < layerHeight-epsilon {
			continue
		}
		printable = append(printable, layer)
	}
	return printable
}

type solidLayerPlacement struct {
	Active        bool
	Region        string
	SequenceIndex int
	AngleDeg      float64
}

func solidLayerPlacementForIndex(printedIdx, totalPrinted, bottomCount, topCount int) solidLayerPlacement {
	if totalPrinted <= 0 {
		return solidLayerPlacement{}
	}
	if bottomCount > totalPrinted {
		bottomCount = totalPrinted
	}
	if topCount > totalPrinted {
		topCount = totalPrinted
	}

	if bottomCount > 0 && printedIdx < bottomCount {
		return solidLayerPlacement{Active: true, Region: "BOTTOM", SequenceIndex: printedIdx, AngleDeg: solidAngleForIndex(printedIdx)}
	}

	if topCount > 0 {
		topStart := totalPrinted - topCount
		if printedIdx >= topStart {
			seq := printedIdx - topStart
			return solidLayerPlacement{Active: true, Region: "TOP", SequenceIndex: seq, AngleDeg: solidAngleForIndex(seq)}
		}
	}

	return solidLayerPlacement{}
}

func solidAngleForIndex(idx int) float64 {
	if idx%2 == 0 {
		return 45
	}
	return -45
}

func emitContourLoop(b *strings.Builder, state *gcodeState, points []Point2D, cfg GCodeConfig, layerHeight float64) {
	if len(points) < 2 {
		return
	}

	moveToLayerStart(b, state, points[0], cfg)
	for i := 1; i <= len(points); i++ {
		next := points[i%len(points)]
		d := distance2D(state.X, state.Y, next.X, next.Y)
		if d <= epsilon {
			continue
		}
		state.E += extrusionForDistance(d, cfg.LineWidthMM, layerHeight, cfg.FilamentDiameterMM)
		b.WriteString(fmt.Sprintf("G1 X%.3f Y%.3f E%.5f F%.0f\n", next.X+cfg.BuildPlateOffsetXMM, next.Y+cfg.BuildPlateOffsetYMM, state.E, cfg.PrintSpeedMMs*60.0))
		state.X, state.Y = next.X, next.Y
		state.HasPosition = true
	}
}

type fillSegment struct {
	Start Point2D
	End   Point2D
}

func emitSolidFill(b *strings.Builder, state *gcodeState, points []Point2D, cfg GCodeConfig, layerHeight, angleDeg float64) {
	segments := buildSolidFillSegments(points, cfg.LineWidthMM, angleDeg)
	for i, segment := range segments {
		start := segment.Start
		end := segment.End
		if i%2 == 1 {
			start, end = end, start
		}
		moveToLayerStart(b, state, start, cfg)
		d := distance2D(start.X, start.Y, end.X, end.Y)
		if d <= epsilon {
			continue
		}
		state.E += extrusionForDistance(d, cfg.LineWidthMM, layerHeight, cfg.FilamentDiameterMM)
		b.WriteString(fmt.Sprintf("G1 X%.3f Y%.3f E%.5f F%.0f\n", end.X+cfg.BuildPlateOffsetXMM, end.Y+cfg.BuildPlateOffsetYMM, state.E, cfg.PrintSpeedMMs*60.0))
		state.X, state.Y = end.X, end.Y
		state.HasPosition = true
	}
}

func buildSolidFillSegments(points []Point2D, spacing, angleDeg float64) []fillSegment {
	if len(points) < 3 || spacing <= 0 {
		return nil
	}

	rotated := rotatePolygon(points, -angleDeg)
	minY := rotated[0].Y
	maxY := rotated[0].Y
	for _, p := range rotated[1:] {
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}

	startY := minY + spacing/2.0
	endY := maxY - spacing/2.0
	if startY > endY+epsilon {
		y := (minY + maxY) / 2.0
		xs := polygonLineIntersections(rotated, y)
		if len(xs) < 2 {
			return nil
		}
		sort.Float64s(xs)
		var segments []fillSegment
		for i := 0; i+1 < len(xs); i += 2 {
			start := rotatePoint(Point2D{X: xs[i], Y: y}, angleDeg)
			end := rotatePoint(Point2D{X: xs[i+1], Y: y}, angleDeg)
			segments = append(segments, fillSegment{Start: start, End: end})
		}
		return segments
	}

	var segments []fillSegment
	for y := startY; y <= endY+epsilon; y += spacing {
		xs := polygonLineIntersections(rotated, y)
		if len(xs) < 2 {
			continue
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			start := rotatePoint(Point2D{X: xs[i], Y: y}, angleDeg)
			end := rotatePoint(Point2D{X: xs[i+1], Y: y}, angleDeg)
			segments = append(segments, fillSegment{Start: start, End: end})
		}
	}

	return segments
}

func rotatePolygon(points []Point2D, angleDeg float64) []Point2D {
	rotated := make([]Point2D, len(points))
	for i, p := range points {
		rotated[i] = rotatePoint(p, angleDeg)
	}
	return rotated
}

func rotatePoint(p Point2D, angleDeg float64) Point2D {
	if angleDeg == 0 {
		return p
	}
	angleRad := angleDeg * math.Pi / 180.0
	cosA := math.Cos(angleRad)
	sinA := math.Sin(angleRad)
	return Point2D{
		X: p.X*cosA - p.Y*sinA,
		Y: p.X*sinA + p.Y*cosA,
	}
}

func polygonLineIntersections(points []Point2D, y float64) []float64 {
	intersections := make([]float64, 0)
	for i := 0; i < len(points); i++ {
		p1 := points[i]
		p2 := points[(i+1)%len(points)]
		if (p1.Y <= y && p2.Y > y) || (p2.Y <= y && p1.Y > y) {
			if math.Abs(p2.Y-p1.Y) <= epsilon {
				continue
			}
			x := p1.X + (y-p1.Y)*(p2.X-p1.X)/(p2.Y-p1.Y)
			intersections = append(intersections, x)
		}
	}
	return intersections
}

func moveToLayerStart(b *strings.Builder, state *gcodeState, start Point2D, cfg GCodeConfig) {
	if !state.HasPosition {
		b.WriteString(fmt.Sprintf("G0 X%.3f Y%.3f F%.0f\n", start.X+cfg.BuildPlateOffsetXMM, start.Y+cfg.BuildPlateOffsetYMM, cfg.TravelSpeedMMs*60.0))
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
			b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", hopZ+cfg.BuildPlateOffsetZMM, cfg.ZHopSpeedMMs*60.0))
		}
		b.WriteString(fmt.Sprintf("G0 X%.3f Y%.3f F%.0f\n", start.X+cfg.BuildPlateOffsetXMM, start.Y+cfg.BuildPlateOffsetYMM, cfg.TravelSpeedMMs*60.0))
		if cfg.ZHopHeightMM > 0 {
			b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", state.Z+cfg.BuildPlateOffsetZMM, cfg.ZHopSpeedMMs*60.0))
		}
		state.E += cfg.RetractionDistanceMM
		b.WriteString(fmt.Sprintf("G1 E%.5f F%.0f\n", state.E, cfg.RetractionSpeedMMs*60.0))
	} else {
		b.WriteString(fmt.Sprintf("G0 X%.3f Y%.3f F%.0f\n", start.X+cfg.BuildPlateOffsetXMM, start.Y+cfg.BuildPlateOffsetYMM, cfg.TravelSpeedMMs*60.0))
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
