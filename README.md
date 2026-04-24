# ASE2026_Vibe_Slicer

> By: Thomas Martin Pfister  
> Advanced Systems Engineering S2026, University of Salzburg  

<!-- Problem definition: -->
## Problem definition:
Slicers for 3D Printers have been around for a long time. They are used to convert 3D-Objects into a series of 2D layers (slices) and generate GCODE-Instructions for 3D printers. 
Popular Slicers are often developed by printer manufacturers, therefore closed source (exceptions being Cura- and Prusa Slicer) and written in the programing languages C++ or Python.
Moreover, because they need to cover a large spectrum of different style printers, they are often quite complex and packed with a lot of specialized features, which can be overwhelming especially for beginners.

Because it is not a trivial task to develop a Slicer and the existing feature rich and complex Slicers have established themselves as the de-facto standard, there have not been many attempts to create a basic Slicer from scratch, especially not in the Go programming language.

<!-- Problem solution: -->
## Problem solution:
Attempt to create a basic easy to use Slicer in the Go programming language for a Fused Deposition Modeling (FDM) Type 3D Printer, using only AI Tools. 
The program will take a 3D-Object in STL format as input, convert it into a series of 2D layers (slices), take custom printer specific parameters and output usable GCODE-Instructions for a Fused Deposition Modeling (FDM) Type 3D printer.

<!-- Current Stage: -->
## Current Stage:
Started GCODE generation/GCODE is already being generated. Verifying that the generated GCODE is valid and safe.

<!-- Generative AI assisted README: -->
## Generative AI assisted README:
Simple Go CLI that reads an STL file, slices it from bottom to top every `0.2mm` (or a custom layer height), and outputs 2D `x,y` contour vertices for each layer. The output is a JSON file containing an array of layers, where each layer has a `z` height and an array of `points` representing the contour vertices for that layer. The points are ordered as a clockwise toolpath and rotated so the first point is the smallest corner (`min x`, then `min y`). The program also includes a separate CLI for converting the JSON output into FDM 3D-Printer G-code, with customizable parameters for print settings and start/end G-code blocks.

## Usage

Current options for input STL file conversion to JSON:

- `cube_10` A cube object in `10 x 10 x 10`mm size
- `cube_20` A cube object in `20 x 20 x 20`mm size

```powershell
go run .\stl_to_json_converter -in .\cube_10.stl
```

Optional flags:

- `-layer` layer height in mm (default `0.2`)
- `-out` write JSON to file instead of stdout

Example:

```powershell
go run .\stl_to_json_converter -in .\cube_10.stl -layer 0.2 -out .\slices.json
```

## Two-step workflow

Currently, the workflow is a two-step process where you first convert STL to JSON, then JSON to G-code.
Current option for JSON to G-code conversion:

```powershell
go run .\stl_to_json_converter -in .\cube_10.stl -layer 0.2 -out .\slices.json
go run .\create_g_code -json-in .\slices.json -gcode-out .\print.gcode
```

## G-code generation from JSON

The project also supports converting slicer JSON into FDM `.gcode` via a separate CLI in `create_g_code/create_g_code.go`.

```powershell
go run .\create_g_code -json-in .\slices.json -gcode-out .\print.gcode
```

Supported G-code parameters:

- `-start-gcode` custom G-code block inserted at file start (`\n` is supported)
- `-end-gcode` custom G-code block appended at file end (`\n` is supported)
- `-offset-x` build plate X offset in mm
- `-offset-y` build plate Y offset in mm
- `-offset-z` build plate Z offset in mm
- `-line-width` extrusion line width in mm
- `-filament-diameter` filament diameter in mm
- `-print-temp` printhead temperature in Celsius
- `-build-plate-temp` build plate temperature in Celsius
- `-print-speed` XY print speed in mm/s
- `-z-hop-speed` Z-axis move speed in mm/s
- `-z-hop-height` Z-hop height in mm
- `-travel-speed` travel move speed in mm/s
- `-print-acceleration` print/travel acceleration in mm/s^2
- `-retraction-distance` retraction distance in mm
- `-retraction-speed` retraction speed in mm/s
- `-retraction-min-travel` minimum travel distance (mm) before retraction

Example with common overrides:

```powershell
go run .\create_g_code -json-in .\slices.json -gcode-out .\print.gcode -start-gcode "G28\nG92 E0" -end-gcode "M104 S0\nM140 S0\nM84" -offset-x 0 -offset-y 0 -offset-z 0.0 -line-width 0.42 -filament-diameter 1.75 -z-hop-height 0.4 -print-temp 205 -build-plate-temp 60 -print-speed 45
```

## Output format

Each layer is processed in 3 steps after triangle-plane intersections:

1. Build intersection segments for that `z`.
2. Stitch connected segments into closed contour loops.
3. Remove collinear points (for example, points introduced by STL face triangulation).

So `points` are contour vertices after stitching/simplification, not raw unordered intersection endpoints.
For closed loops, points are ordered as a clockwise toolpath and rotated so the first point is the smallest corner (`min x`, then `min y`).

For the included axis-aligned cubes (`cube_10.stl`, `cube_20.stl`), you should typically see 4 points per layer (square contour).

```json
{
  "input": "cube.stl",
  "layer_height_mm": 0.2,
  "layers": [
    {
      "z": 0.2,
      "points": [
        { "x": 0.0, "y": 0.0 },
        { "x": 0.0, "y": 10.0 },
        { "x": 10.0, "y": 10.0 },
        { "x": 10.0, "y": 0.0 }
      ]
    }
  ]
}
```

## Known limitations

- `points` are flattened vertices only. The JSON output does not currently preserve explicit loop/group structure per layer.
- For open or non-manifold geometry, loop stitching may fail and the slicer falls back to unique segment endpoints for that layer (ordering is then not guaranteed to be a valid toolpath).
- Floating-point tolerances (`epsilon`) and coordinate rounding can affect point merging and contour simplification for very small features.
- JSON and G-code generation are separate CLIs; ensure your JSON input path and printer parameters are set correctly when running `create_g_code`.

