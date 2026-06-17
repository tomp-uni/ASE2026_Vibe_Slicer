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

type GCodeConfig struct {
	StartGCode            string
	EndGCode              string
	BuildPlateOffsetXMM   float64
	BuildPlateOffsetYMM   float64
	BuildPlateOffsetZMM   float64
	OuterWallLines        int
	Skirt                 bool
	SkirtLines            int
	Brim                  bool
	BrimLines             int
	SolidBottomLayers     int
	SolidTopLayers        int
	Infill                bool
	InfillDensity         float64
	CoolingFan            bool
	CoolingFanLayer       int
	CoolingFanSpeed       float64
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
		"M140 S0 ; Turn off bed\n"+
		"M107; Turn off cooling fan\n"+
		"M84 ; Disable motors", "G-code snippet appended at end (supports \\n)")
	offsetX := flag.Float64("offset-x", 180, "Build plate X offset in mm")
	offsetY := flag.Float64("offset-y", 180, "Build plate Y offset in mm")
	offsetZ := flag.Float64("offset-z", 0, "Build plate Z offset in mm")
	outerWallLines := flag.Int("outer-wall-lines", 3, "Number of solid outer wall lines (minimum 1)")
	skirt := flag.Bool("skirt", true, "Print a skirt on the initial layer")
	skirtLines := flag.Int("skirt-lines", 3, "Number of skirt lines")
	brim := flag.Bool("brim", true, "Print a brim on the initial layer")
	brimLines := flag.Int("brim-lines", 5, "Number of brim lines")
	solidBottomLayers := flag.Int("solid-bottom-layers", 3, "Number of fully printed solid layers at the bottom")
	solidTopLayers := flag.Int("solid-top-layers", 3, "Number of fully printed solid layers at the top")
	infill := flag.Bool("infill", true, "Print zig-zag infill in the middle layers")
	infillDensity := flag.Float64("infill-density", 25, "Infill density in percent (0 to 100)")
	coolingFan := flag.Bool("cooling-fan", true, "Use the print cooling fan")
	coolingFanLayer := flag.Int("cooling-fan-layer", 1, "Layer index at which to turn on the print cooling fan (0 = first layer)")
	coolingFanSpeed := flag.Float64("cooling-fan-speed", 100, "Cooling fan speed in percent (0 to 100)")
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
		OuterWallLines:        *outerWallLines,
		Skirt:                 *skirt,
		SkirtLines:            *skirtLines,
		Brim:                  *brim,
		BrimLines:             *brimLines,
		SolidBottomLayers:     *solidBottomLayers,
		SolidTopLayers:        *solidTopLayers,
		Infill:                *infill,
		InfillDensity:         *infillDensity,
		CoolingFan:            *coolingFan,
		CoolingFanLayer:       *coolingFanLayer,
		CoolingFanSpeed:       *coolingFanSpeed,
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
	if cfg.OuterWallLines < 1 {
		return errors.New("-outer-wall-lines must be >= 1")
	}
	if cfg.SkirtLines < 0 {
		return errors.New("-skirt-lines must be >= 0")
	}
	if cfg.BrimLines < 0 {
		return errors.New("-brim-lines must be >= 0")
	}
	if cfg.SolidBottomLayers < 0 || cfg.SolidTopLayers < 0 {
		return errors.New("-solid-bottom-layers and -solid-top-layers must be >= 0")
	}
	if cfg.InfillDensity < 0 || cfg.InfillDensity > 100 {
		return errors.New("-infill-density must be between 0 and 100")
	}
	if cfg.CoolingFanLayer < 0 {
		return errors.New("-cooling-fan-layer must be >= 0")
	}
	if cfg.CoolingFanSpeed < 0 || cfg.CoolingFanSpeed > 100 {
		return errors.New("-cooling-fan-speed must be between 0 and 100")
	}
	return nil
}

func buildGCode(input SliceOutput, cfg GCodeConfig) string {
	var b strings.Builder
	state := gcodeState{}
	printableLayers := filterPrintableLayers(input.Layers, input.LayerHeight)
	infillLayerIdx := 0
	coolingFanEnabled := false

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
		roots := layerContourRoots(layer)
		primaryBoundary := layerPrimaryBoundary(layer)

		state.Z = layer.Z
		b.WriteString(fmt.Sprintf("; LAYER %d Z=%.3f\n", emittedLayerIdx, layer.Z))
		b.WriteString(fmt.Sprintf("G0 Z%.3f F%.0f\n", layer.Z+cfg.BuildPlateOffsetZMM, cfg.ZHopSpeedMMs*60.0))
		if emittedLayerIdx == 0 && cfg.Skirt && cfg.SkirtLines > 0 {
			b.WriteString(fmt.Sprintf("; SKIRT LAYER %d LINES=%d\n", emittedLayerIdx, cfg.SkirtLines))
			emitSkirt(&b, &state, primaryBoundary, cfg, input.LayerHeight)
		}
		if cfg.CoolingFan && !coolingFanEnabled && emittedLayerIdx >= cfg.CoolingFanLayer {
			b.WriteString(fmt.Sprintf("M106 S%d\n", coolingFanPwmFromPercent(cfg.CoolingFanSpeed)))
			coolingFanEnabled = true
		}
		emitContourWallsRecursive(&b, &state, roots, cfg, input.LayerHeight, 0)
		if emittedLayerIdx == 0 && cfg.Brim && cfg.BrimLines > 0 {
			b.WriteString(fmt.Sprintf("; BRIM LAYER %d LINES=%d\n", emittedLayerIdx, cfg.BrimLines))
			emitBrim(&b, &state, primaryBoundary, cfg, input.LayerHeight)
		}

		if solid := solidLayerPlacementForIndex(emittedLayerIdx, len(printableLayers), cfg.SolidBottomLayers, cfg.SolidTopLayers); solid.Active {
			b.WriteString(fmt.Sprintf("; SOLID %s LAYER %d ANGLE=%.0f\n", solid.Region, solid.SequenceIndex, solid.AngleDeg))
			emitSolidFill(&b, &state, roots, cfg, input.LayerHeight, solid.AngleDeg)
		} else if cfg.Infill && cfg.InfillDensity > 0 {
			angle := infillAngleForIndex(infillLayerIdx)
			b.WriteString(fmt.Sprintf("; INFILL LAYER %d DENSITY=%.0f ANGLE=%.0f\n", infillLayerIdx, cfg.InfillDensity, angle))
			emitInfill(&b, &state, roots, cfg, input.LayerHeight, cfg.InfillDensity, angle)
			infillLayerIdx++
		}
	}

	b.WriteString("M140 S0\n")
	if cfg.CoolingFan {
		b.WriteString("M107\n")
	}
	appendCustomBlock(&b, cfg.EndGCode)
	b.WriteString("M104 S0\n")
	return b.String()
}

func filterPrintableLayers(layers []LayerResult, layerHeight float64) []LayerResult {
	printable := make([]LayerResult, 0, len(layers))
	for _, layer := range layers {
		if !layerHasGeometry(layer) {
			continue
		}
		if layer.Z < layerHeight-epsilon {
			continue
		}
		printable = append(printable, layer)
	}
	return printable
}

func layerHasGeometry(layer LayerResult) bool {
	return len(layer.Points) >= 2 || len(layer.Contours) > 0
}

func layerContourRoots(layer LayerResult) []ContourResult {
	if len(layer.Contours) > 0 {
		return layer.Contours
	}
	if len(layer.Points) >= 2 {
		return []ContourResult{{Closed: true, Role: "outer", Points: append([]Point2D(nil), layer.Points...)}}
	}
	return nil
}

func layerPrimaryBoundary(layer LayerResult) []Point2D {
	if points := firstRenderableContourPoints(layer.Contours); len(points) > 0 {
		return points
	}
	if len(layer.Points) > 0 {
		return layer.Points
	}
	return nil
}

func firstRenderableContourPoints(contours []ContourResult) []Point2D {
	for _, contour := range contours {
		if contour.Closed && len(contour.Points) > 0 {
			return contour.Points
		}
		if len(contour.Children) > 0 {
			if points := firstRenderableContourPoints(contour.Children); len(points) > 0 {
				return points
			}
		}
	}
	return nil
}

func emitContourWallsRecursive(b *strings.Builder, state *gcodeState, contours []ContourResult, cfg GCodeConfig, layerHeight float64, depth int) {
	for _, contour := range contours {
		if contour.Closed && len(contour.Points) >= 2 {
			emitContourWallsForDepth(b, state, contour.Points, cfg, layerHeight, depth)
		}
		if len(contour.Children) > 0 {
			emitContourWallsRecursive(b, state, contour.Children, cfg, layerHeight, depth+1)
		}
	}
}

func emitContourWallsForDepth(b *strings.Builder, state *gcodeState, points []Point2D, cfg GCodeConfig, layerHeight float64, depth int) {
	if len(points) < 2 {
		return
	}

	offsetFn := insetPolygon
	if depth%2 == 1 {
		offsetFn = outsetPolygon
	}

	wallPoints := offsetFn(points, cfg.LineWidthMM/2.0)
	if len(wallPoints) < 3 {
		wallPoints = points
	}
	for wallIdx := 0; wallIdx < cfg.OuterWallLines; wallIdx++ {
		if len(wallPoints) < 2 {
			break
		}
		emitContourLoop(b, state, wallPoints, cfg, layerHeight)
		if wallIdx == cfg.OuterWallLines-1 {
			break
		}
		next := offsetFn(wallPoints, cfg.LineWidthMM)
		if len(next) < 3 {
			break
		}
		wallPoints = next
	}
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

func infillAngleForIndex(idx int) float64 { return solidAngleForIndex(idx) }

func coolingFanPwmFromPercent(percent float64) int {
	if percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return 255
	}
	return int(math.Round(percent * 255.0 / 100.0))
}

func emitOuterWalls(b *strings.Builder, state *gcodeState, points []Point2D, cfg GCodeConfig, layerHeight float64) []Point2D {
	wallPoints := insetPolygon(points, cfg.LineWidthMM/2.0)
	if len(wallPoints) < 3 {
		wallPoints = points
	}
	var innermost []Point2D
	for wallIdx := 0; wallIdx < cfg.OuterWallLines; wallIdx++ {
		if len(wallPoints) < 2 {
			break
		}
		emitContourLoop(b, state, wallPoints, cfg, layerHeight)
		innermost = wallPoints
		if wallIdx == cfg.OuterWallLines-1 {
			break
		}
		next := insetPolygon(wallPoints, cfg.LineWidthMM)
		if len(next) < 3 {
			break
		}
		wallPoints = next
	}
	return innermost
}

func emitBrim(b *strings.Builder, state *gcodeState, points []Point2D, cfg GCodeConfig, layerHeight float64) {
	brimPoints := outsetPolygon(points, cfg.LineWidthMM/2.0)
	if len(brimPoints) < 3 {
		return
	}
	for brimIdx := 0; brimIdx < cfg.BrimLines; brimIdx++ {
		if len(brimPoints) < 2 {
			break
		}
		emitContourLoop(b, state, brimPoints, cfg, layerHeight)
		if brimIdx == cfg.BrimLines-1 {
			break
		}
		next := outsetPolygon(brimPoints, cfg.LineWidthMM)
		if len(next) < 3 {
			break
		}
		brimPoints = next
	}
}

func emitSkirt(b *strings.Builder, state *gcodeState, points []Point2D, cfg GCodeConfig, layerHeight float64) {
	startDistance := 5.0 + cfg.LineWidthMM/2.0
	if cfg.Brim && cfg.BrimLines > 0 {
		startDistance += float64(cfg.BrimLines) * cfg.LineWidthMM
	}
	skirtPoints := outsetPolygon(points, startDistance)
	if len(skirtPoints) < 3 {
		return
	}
	for skirtIdx := 0; skirtIdx < cfg.SkirtLines; skirtIdx++ {
		if len(skirtPoints) < 2 {
			break
		}
		emitContourLoop(b, state, skirtPoints, cfg, layerHeight)
		if skirtIdx == cfg.SkirtLines-1 {
			break
		}
		next := outsetPolygon(skirtPoints, cfg.LineWidthMM)
		if len(next) < 3 {
			break
		}
		skirtPoints = next
	}
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

func emitSolidFill(b *strings.Builder, state *gcodeState, contours []ContourResult, cfg GCodeConfig, layerHeight, angleDeg float64) {
	segments := buildSolidFillSegmentsFromWallOffsets(contours, cfg.OuterWallLines, cfg.LineWidthMM, cfg.LineWidthMM, angleDeg)
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

func emitInfill(b *strings.Builder, state *gcodeState, contours []ContourResult, cfg GCodeConfig, layerHeight, density, angleDeg float64) {
	spacing := infillSpacingFromDensity(cfg.LineWidthMM, density)
	if spacing <= 0 {
		return
	}
	segments := buildSolidFillSegmentsFromWallOffsets(contours, cfg.OuterWallLines, cfg.LineWidthMM, spacing, angleDeg)
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

func infillSpacingFromDensity(lineWidth, density float64) float64 {
	if density <= 0 {
		return 0
	}
	if density > 100 {
		density = 100
	}
	return lineWidth * 100.0 / density
}

func solidFillBoundary(points []Point2D, lineWidth float64) []Point2D {
	boundary := insetPolygon(points, lineWidth/2.0)
	if len(boundary) < 3 {
		return points
	}
	return boundary
}

func buildSolidFillSegments(points []Point2D, spacing, angleDeg float64) []fillSegment {
	return buildSolidFillSegmentsFromContours([]ContourResult{{Closed: true, Points: points}}, spacing, angleDeg)
}

func buildSolidFillSegmentsFromContours(contours []ContourResult, spacing, angleDeg float64) []fillSegment {
	loops := collectClosedContourLoops(contours)
	return buildSolidFillSegmentsFromLoops(loops, spacing, angleDeg)
}

func buildSolidFillSegmentsFromWallOffsets(contours []ContourResult, outerWallLines int, lineWidth, spacing, angleDeg float64) []fillSegment {
	loops := collectWallOffsetLoops(contours, outerWallLines, lineWidth)
	return buildSolidFillSegmentsFromLoopsWithPhase(loops, spacing, angleDeg, spacing/2.0)
}

func buildSolidFillSegmentsFromLoops(loops [][]Point2D, spacing, angleDeg float64) []fillSegment {
	return buildSolidFillSegmentsFromLoopsWithPhase(loops, spacing, angleDeg, 0)
}

func buildSolidFillSegmentsFromLoopsWithPhase(loops [][]Point2D, spacing, angleDeg, phase float64) []fillSegment {
	if len(loops) == 0 || spacing <= 0 {
		return nil
	}

	rotatedLoops := make([][]Point2D, 0, len(loops))
	minY := math.Inf(1)
	maxY := math.Inf(-1)
	for _, loop := range loops {
		if len(loop) < 3 {
			continue
		}
		rotated := rotatePolygon(loop, -angleDeg)
		rotatedLoops = append(rotatedLoops, rotated)
		for _, p := range rotated {
			if p.Y < minY {
				minY = p.Y
			}
			if p.Y > maxY {
				maxY = p.Y
			}
		}
	}
	if len(rotatedLoops) == 0 {
		return nil
	}

	startY := minY + phase
	endY := maxY
	if startY > endY+epsilon {
		y := (minY + maxY) / 2.0
		xs := polygonLineIntersectionsAcrossLoops(rotatedLoops, y)
		return intersectionsToSegmentsWithinLoops(xs, y, angleDeg, rotatedLoops)
	}

	var segments []fillSegment
	for y := startY; y <= endY+epsilon; y += spacing {
		sampleY := y
		if sampleY >= maxY {
			sampleY = maxY - epsilon
		}
		xs := polygonLineIntersectionsAcrossLoops(rotatedLoops, sampleY)
		segments = append(segments, intersectionsToSegmentsWithinLoops(xs, sampleY, angleDeg, rotatedLoops)...)
	}

	return segments
}

func collectWallOffsetLoops(contours []ContourResult, outerWallLines int, lineWidth float64) [][]Point2D {
	// Align fill boundaries with the inner edge of the last wall line.
	offsetDistance := float64(outerWallLines) * lineWidth
	return collectWallOffsetLoopsAtDepth(contours, offsetDistance, 0)
}

func collectWallOffsetLoopsAtDepth(contours []ContourResult, offsetDistance float64, depth int) [][]Point2D {
	loops := make([][]Point2D, 0)
	for _, contour := range contours {
		if contour.Closed && len(contour.Points) >= 3 {
			offsetFn := insetPolygon
			if depth%2 == 1 {
				offsetFn = outsetPolygon
			}
			if loop := safeOffsetPolygon(contour.Points, offsetFn, offsetDistance); len(loop) >= 3 {
				loops = append(loops, loop)
			}
		}
		if len(contour.Children) > 0 {
			loops = append(loops, collectWallOffsetLoopsAtDepth(contour.Children, offsetDistance, depth+1)...)
		}
	}
	return loops
}

func intersectionsToSegments(xs []float64, y, angleDeg float64) []fillSegment {
	if len(xs) < 2 {
		return nil
	}
	sort.Float64s(xs)
	segments := make([]fillSegment, 0, len(xs)/2)
	for i := 0; i+1 < len(xs); i += 2 {
		start := rotatePoint(Point2D{X: xs[i], Y: y}, angleDeg)
		end := rotatePoint(Point2D{X: xs[i+1], Y: y}, angleDeg)
		segments = append(segments, fillSegment{Start: start, End: end})
	}
	return segments
}

func intersectionsToSegmentsWithinLoops(xs []float64, y, angleDeg float64, loops [][]Point2D) []fillSegment {
	if len(xs) < 2 {
		return nil
	}
	sort.Float64s(xs)
	segments := make([]fillSegment, 0, len(xs)/2)
	for i := 0; i+1 < len(xs); i += 2 {
		mid := Point2D{X: (xs[i] + xs[i+1]) / 2.0, Y: y}
		if !pointInPolygonSet(mid, loops) {
			continue
		}
		start := rotatePoint(Point2D{X: xs[i], Y: y}, angleDeg)
		end := rotatePoint(Point2D{X: xs[i+1], Y: y}, angleDeg)
		if distance2D(start.X, start.Y, end.X, end.Y) <= epsilon {
			continue
		}
		segments = append(segments, fillSegment{Start: start, End: end})
	}
	return segments
}

func polygonLineIntersectionsAcrossLoops(loops [][]Point2D, y float64) []float64 {
	intersections := make([]float64, 0)
	for _, loop := range loops {
		intersections = append(intersections, polygonLineIntersections(loop, y)...)
	}
	return dedupeSortedFloat64s(intersections)
}

func dedupeSortedFloat64s(values []float64) []float64 {
	if len(values) == 0 {
		return values
	}
	sort.Float64s(values)
	result := make([]float64, 0, len(values))
	for _, v := range values {
		if len(result) == 0 || math.Abs(result[len(result)-1]-v) > epsilon {
			result = append(result, v)
		}
	}
	return result
}

func safeOffsetPolygon(points []Point2D, offsetFn func([]Point2D, float64) []Point2D, distance float64) []Point2D {
	loop := offsetFn(points, distance)
	if len(loop) < 3 {
		return nil
	}
	loop = pruneDuplicateConsecutivePoints(loop)
	if len(loop) < 3 || !validateClosedPolygonLoop(loop) {
		return nil
	}
	return loop
}

func validateClosedPolygonLoop(points []Point2D) bool {
	if len(points) < 3 {
		return false
	}
	if math.Abs(polygonSignedArea(points)) <= epsilon {
		return false
	}
	for i := 0; i < len(points); i++ {
		j := (i + 1) % len(points)
		if distance2D(points[i].X, points[i].Y, points[j].X, points[j].Y) <= epsilon {
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
			if segmentsIntersectWithinTolerance(a1, a2, b1, b2) {
				return false
			}
		}
	}
	return true
}

func segmentsIntersectWithinTolerance(a1, a2, b1, b2 Point2D) bool {
	if pointsEqual(a1, b1) || pointsEqual(a1, b2) || pointsEqual(a2, b1) || pointsEqual(a2, b2) {
		return false
	}
	o1 := orientation(a1, a2, b1)
	o2 := orientation(a1, a2, b2)
	o3 := orientation(b1, b2, a1)
	o4 := orientation(b1, b2, a2)
	if ((o1 > 0 && o2 < 0) || (o1 < 0 && o2 > 0)) && ((o3 > 0 && o4 < 0) || (o3 < 0 && o4 > 0)) {
		return true
	}
	if math.Abs(o1) <= epsilon && pointOnSegmentWithinTolerance(b1, a1, a2) {
		return true
	}
	if math.Abs(o2) <= epsilon && pointOnSegmentWithinTolerance(b2, a1, a2) {
		return true
	}
	if math.Abs(o3) <= epsilon && pointOnSegmentWithinTolerance(a1, b1, b2) {
		return true
	}
	if math.Abs(o4) <= epsilon && pointOnSegmentWithinTolerance(a2, b1, b2) {
		return true
	}
	return false
}

func orientation(a, b, c Point2D) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

func pointOnSegmentWithinTolerance(p, a, b Point2D) bool {
	minX := math.Min(a.X, b.X) - epsilon
	maxX := math.Max(a.X, b.X) + epsilon
	minY := math.Min(a.Y, b.Y) - epsilon
	maxY := math.Max(a.Y, b.Y) + epsilon
	if p.X < minX || p.X > maxX || p.Y < minY || p.Y > maxY {
		return false
	}
	return math.Abs(orientation(a, b, p)) <= math.Max(epsilon*10, 1e-9)
}

func pointInPolygonSet(p Point2D, loops [][]Point2D) bool {
	inside := false
	for _, loop := range loops {
		if pointOnPolygon(loop, p) {
			return true
		}
		if pointInPolygon(loop, p) {
			inside = !inside
		}
	}
	return inside
}

func pointInPolygon(points []Point2D, p Point2D) bool {
	inside := false
	for i, j := 0, len(points)-1; i < len(points); j, i = i, i+1 {
		a := points[j]
		b := points[i]
		if ((a.Y > p.Y) != (b.Y > p.Y)) && (p.X <= (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X+epsilon) {
			inside = !inside
		}
	}
	return inside
}

func pointOnPolygon(points []Point2D, p Point2D) bool {
	for i := 0; i < len(points); i++ {
		a := points[i]
		b := points[(i+1)%len(points)]
		if pointOnSegmentWithinTolerance(p, a, b) {
			return true
		}
	}
	return false
}

func pointsEqual(a, b Point2D) bool {
	return distance2D(a.X, a.Y, b.X, b.Y) <= epsilon
}

func collectClosedContourLoops(contours []ContourResult) [][]Point2D {
	loops := make([][]Point2D, 0)
	for _, contour := range contours {
		if contour.Closed && len(contour.Points) >= 3 {
			loops = append(loops, append([]Point2D(nil), contour.Points...))
		}
		if len(contour.Children) > 0 {
			loops = append(loops, collectClosedContourLoops(contour.Children)...)
		}
	}
	return loops
}

func insetPolygon(points []Point2D, distance float64) []Point2D {
	return offsetPolygon(points, distance)
}

func outsetPolygon(points []Point2D, distance float64) []Point2D {
	return offsetPolygon(points, -distance)
}

func offsetPolygon(points []Point2D, distance float64) []Point2D {
	if len(points) < 3 || math.Abs(distance) <= epsilon {
		return nil
	}
	minX, maxX, minY, maxY := polygonBounds(points)

	clockwise := polygonSignedArea(points) < 0
	inwardSign := 1.0
	if !clockwise {
		inwardSign = -1.0
	}
	if distance < 0 {
		inwardSign = -inwardSign
		distance = -distance
	}

	inset := make([]Point2D, 0, len(points))
	for i := 0; i < len(points); i++ {
		prev := points[(i-1+len(points))%len(points)]
		next := points[(i+1)%len(points)]
		metrics := newCornerOffsetMetrics(prev, points[i], next, distance)

		p1, d1 := offsetEdge(prev, points[i], distance, inwardSign)
		p2, d2 := offsetEdge(points[i], next, distance, inwardSign)
		p, ok := intersectLines(p1, d1, p2, d2, metrics.localScale, distance)
		if ok && isReasonableOffsetVertex(p, prev, points[i], next, distance) && !shouldBevelJoin(p, points[i], distance, metrics) {
			inset = append(inset, p)
			continue
		}
		if bevel := buildBevelJoin(p1, d1, p2, d2, prev, points[i], distance, metrics); len(bevel) > 0 {
			inset = append(inset, bevel...)
			continue
		}
		return nil
	}

	inset = pruneDuplicateConsecutivePoints(inset)
	if len(inset) < 3 || !offsetLoopWithinBounds(inset, minX, maxX, minY, maxY, distance) {
		return nil
	}
	return inset
}

type cornerOffsetMetrics struct {
	prevLen          float64
	nextLen          float64
	shortEdge        float64
	localScale       float64
	maxMiterLength   float64
	maxBevelDistance float64
}

func newCornerOffsetMetrics(prev, cur, next Point2D, distance float64) cornerOffsetMetrics {
	prevLen := distance2D(prev.X, prev.Y, cur.X, cur.Y)
	nextLen := distance2D(cur.X, cur.Y, next.X, next.Y)
	shortEdge := math.Min(prevLen, nextLen)
	localScale := math.Max(prevLen, nextLen)
	localScale = math.Max(localScale, distance2D(prev.X, prev.Y, next.X, next.Y))
	localScale = math.Max(localScale, distance2D(prev.X, prev.Y, cur.X, cur.Y)+distance2D(cur.X, cur.Y, next.X, next.Y))
	absDistance := math.Abs(distance)
	maxMiterLength := math.Min(6.0*absDistance, math.Min(0.95*shortEdge, 0.95*localScale))
	if maxMiterLength <= epsilon {
		maxMiterLength = absDistance
	}
	maxBevelDistance := math.Max(localScale+2*absDistance, shortEdge+3*absDistance)
	return cornerOffsetMetrics{
		prevLen:          prevLen,
		nextLen:          nextLen,
		shortEdge:        shortEdge,
		localScale:       localScale,
		maxMiterLength:   maxMiterLength,
		maxBevelDistance: maxBevelDistance,
	}
}

func offsetEdge(a, b Point2D, distance, inwardSign float64) (Point2D, Point2D) {
	dx := b.X - a.X
	dy := b.Y - a.Y
	length := math.Hypot(dx, dy)
	if length <= epsilon {
		return a, Point2D{}
	}
	unitX := dx / length
	unitY := dy / length
	normalX := inwardSign * unitY
	normalY := inwardSign * -unitX
	offsetPoint := Point2D{X: a.X + normalX*distance, Y: a.Y + normalY*distance}
	return offsetPoint, Point2D{X: unitX, Y: unitY}
}

func intersectLines(p1, d1, p2, d2 Point2D, localScale, distance float64) (Point2D, bool) {
	len1 := math.Hypot(d1.X, d1.Y)
	len2 := math.Hypot(d2.X, d2.Y)
	if len1 <= epsilon || len2 <= epsilon {
		return Point2D{}, false
	}
	denom := d1.X*d2.Y - d1.Y*d2.X
	sinTheta := math.Abs(denom) / (len1 * len2)
	scale := math.Max(localScale, math.Abs(distance))
	parallelTol := math.Max(1e-6, math.Min(0.02, math.Abs(distance)/(scale+epsilon)))
	if sinTheta <= parallelTol || math.Abs(denom) <= epsilon {
		return Point2D{}, false
	}
	deltaX := p2.X - p1.X
	deltaY := p2.Y - p1.Y
	t := (deltaX*d2.Y - deltaY*d2.X) / denom
	u := (deltaX*d1.Y - deltaY*d1.X) / denom
	maxTravel := math.Max(4*scale, 24*math.Abs(distance))
	if math.IsNaN(t) || math.IsInf(t, 0) || math.IsNaN(u) || math.IsInf(u, 0) || math.Abs(t) > maxTravel || math.Abs(u) > maxTravel {
		return Point2D{}, false
	}
	return Point2D{X: p1.X + t*d1.X, Y: p1.Y + t*d1.Y}, true
}

func shouldBevelJoin(p, cur Point2D, distance float64, metrics cornerOffsetMetrics) bool {
	if distance <= epsilon {
		return false
	}
	if metrics.shortEdge <= epsilon || metrics.localScale <= epsilon {
		return true
	}
	miterLen := cornerMiterLength(cur, p)
	miterLimit := metrics.maxMiterLength
	return miterLen > miterLimit
}

func cornerMiterLength(cur, p Point2D) float64 {
	return distance2D(cur.X, cur.Y, p.X, p.Y)
}

func buildBevelJoin(p1, d1, p2, d2, prev, cur Point2D, distance float64, metrics cornerOffsetMetrics) []Point2D {
	if distance <= epsilon {
		return nil
	}
	if math.Hypot(d1.X, d1.Y) <= epsilon || math.Hypot(d2.X, d2.Y) <= epsilon {
		return nil
	}
	if !isReasonableBevelVertex(p1, prev, cur, prev, distance, metrics) || !isReasonableBevelVertex(p2, prev, cur, cur, distance, metrics) {
		return nil
	}
	if distance2D(p1.X, p1.Y, p2.X, p2.Y) > metrics.maxBevelDistance {
		return nil
	}
	join := []Point2D{p1, p2}
	join = pruneDuplicateConsecutivePoints(join)
	if len(join) < 2 {
		return nil
	}
	return join
}

func isReasonableOffsetVertex(p, prev, cur, next Point2D, distance float64) bool {
	metrics := newCornerOffsetMetrics(prev, cur, next, distance)
	return isReasonableMiterVertex(p, prev, cur, next, distance, metrics)
}

func isReasonableMiterVertex(p, prev, cur, next Point2D, distance float64, metrics cornerOffsetMetrics) bool {
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return false
	}

	if metrics.shortEdge <= epsilon || metrics.localScale <= epsilon {
		return false
	}
	if distance2D(cur.X, cur.Y, p.X, p.Y) > metrics.maxMiterLength {
		return false
	}
	limit := math.Max(metrics.maxMiterLength, 2*math.Abs(distance))
	minX := math.Min(prev.X, math.Min(cur.X, next.X)) - limit
	maxX := math.Max(prev.X, math.Max(cur.X, next.X)) + limit
	minY := math.Min(prev.Y, math.Min(cur.Y, next.Y)) - limit
	maxY := math.Max(prev.Y, math.Max(cur.Y, next.Y)) + limit
	return p.X >= minX && p.X <= maxX && p.Y >= minY && p.Y <= maxY
}

func isReasonableBevelVertex(p, prev, cur, source Point2D, distance float64, metrics cornerOffsetMetrics) bool {
	if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
		return false
	}
	absDistance := math.Abs(distance)
	if absDistance <= epsilon {
		return false
	}
	if distance2D(source.X, source.Y, p.X, p.Y) > math.Max(absDistance*1.6, epsilon*100) {
		return false
	}
	if distance2D(cur.X, cur.Y, p.X, p.Y) > metrics.maxBevelDistance {
		return false
	}
	if distance2D(prev.X, prev.Y, p.X, p.Y) > metrics.maxBevelDistance {
		return false
	}
	return true
}

func offsetLoopWithinBounds(loop []Point2D, minX, maxX, minY, maxY, distance float64) bool {
	if len(loop) == 0 {
		return false
	}
	spanX := maxX - minX
	spanY := maxY - minY
	span := math.Max(spanX, spanY)
	margin := math.Max(6.0*math.Abs(distance), 0.3*span)
	for _, p := range loop {
		if p.X < minX-margin || p.X > maxX+margin || p.Y < minY-margin || p.Y > maxY+margin {
			return false
		}
	}
	return true
}

func polygonBounds(points []Point2D) (minX, maxX, minY, maxY float64) {
	if len(points) == 0 {
		return 0, 0, 0, 0
	}
	minX, maxX = points[0].X, points[0].X
	minY, maxY = points[0].Y, points[0].Y
	for _, p := range points[1:] {
		if p.X < minX {
			minX = p.X
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	return minX, maxX, minY, maxY
}

func polygonSignedArea(points []Point2D) float64 {
	if len(points) < 3 {
		return 0
	}
	area := 0.0
	for i := 0; i < len(points); i++ {
		p1 := points[i]
		p2 := points[(i+1)%len(points)]
		area += p1.X*p2.Y - p2.X*p1.Y
	}
	return area / 2.0
}

func pruneDuplicateConsecutivePoints(points []Point2D) []Point2D {
	if len(points) < 2 {
		return points
	}
	result := make([]Point2D, 0, len(points))
	for _, p := range points {
		if len(result) == 0 || distance2D(result[len(result)-1].X, result[len(result)-1].Y, p.X, p.Y) > epsilon {
			result = append(result, p)
		}
	}
	if len(result) > 1 && distance2D(result[0].X, result[0].Y, result[len(result)-1].X, result[len(result)-1].Y) <= epsilon {
		result = result[:len(result)-1]
	}
	return result
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
