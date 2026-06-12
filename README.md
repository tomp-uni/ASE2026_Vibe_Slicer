# ASE2026_Vibe_Slicer

> By: Thomas Martin Pfister  
> Advanced Systems Engineering S2026, University of Salzburg  

<!-- Problem definition: -->
## Problem definition:
Slicers are used to convert 3D-Objects into a series of 2D layers (slices) and generate GCODE-Instructions for 3D printers.
On the surface, this seems like a straightforward task, but the development of slicing software for 3D printing remains a resource-intensive undertaking, typically requiring significant domain expertise in computational geometry, manufacturing processes, and software engineering. Despite significant advances in generative AI tools in terms of code generation and algorithmic problem-solving, it remains unclear whether such tools can effectively address the complexities of this domain. Specifically, it is not well understood whether contemporary large language models and AI-assisted development methodologies can produce implementations that generate valid, safe, dimensionally accurate, and efficient GCODE-Instructions for 3D printer workflows.

<!-- Research Questions: -->
## Research Questions:
1. **Feasibility**: Is it possible to develop a functionally correct STL-to-GCODE slicer implementation without domain expertise, relying exclusively on AI-assisted code generation?
2. **Comparative Performance**: How does the dimensional accuracy (both theoretically and practically) and efficiency (slice-, and print time) of an AI-developed slicer compare to a mature reference implementation (Cura) on identical 3D-Object inputs?

<!-- Problem solution: -->
## Problem solution:
Attempt to create a basic easy to use Slicer in the Go programming language for a Fused Deposition Modeling (FDM) Type 3D Printer, using only AI Tools. 
The program will take a 3D-Object in STL format as input, convert it into a series of 2D layers (slices), take custom printer specific parameters and output usable GCODE-Instructions for a Fused Deposition Modeling (FDM) Type 3D printer.
If successful, the resulting Slicer will then be evaluated against the popular open source Slicer Cura, in terms of slice time, print time and dimensional accuracy (theoretical and practical) through specific Micro-benchmarks.

<!-- Current Stage: -->
## Current Stage:
- Writing the Paper.
- Presentation slides.
- Ran out of tokens to create support for more complex shapes.

<!-- Generative AI assisted README: -->
## Generative AI assisted README:

<!-- Overview: -->
## Overview
Simple Go CLI that reads an STL file, slices it from bottom to top every `0.2mm` (or a custom layer height), and outputs 2D `x,y` contour vertices for each layer. 
The output is a JSON file containing an array of layers, where each layer has a `z` height, a legacy flattened `points` toolpath, and a structured `contours` tree that preserves loop hierarchy, holes, and open chains. 
The contour points are ordered in a deterministic clockwise-style toolpath and rotated so the first point is the smallest corner (`min x`, then `min y`). 
The program also includes a separate CLI for converting the JSON output into FDM 3D-Printer G-code, with customizable parameters for print settings and start/end G-code blocks.

<!-- Usage of the STL to JSON converter: -->
## Usage of the STL to JSON converter

Current options for input STL file conversion to JSON:

- `cube_10` A cube object in `10 x 10 x 10`mm size
- `cube_20` A cube object in `20 x 20 x 20`mm size
- `Hole_Structure` A block object in `40 x 40 x 5`mm size with one `10`mm diameter hole and one `20`mm diameter hole through the entire height.

```powershell
go run .\stl_to_json_converter -in .\cube_10.stl
```
> **Note:** The examples above use Windows (PowerShell) path syntax. On Linux/macOS, replace `.\` with `./`, for example: `go run ./stl_to_json_converter -in ./cube_10.stl`

Optional flags:

- `-layer` layer height in mm (default `0.2`)
- `-out` write JSON to file instead of stdout

Example:

```powershell
go run .\stl_to_json_converter -in .\cube_10.stl -layer 0.2 -out .\slices.json
```

<!-- Output format of the conversion from STL to JSON: -->
## Output format of the conversion from STL to JSON

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

<!-- G-code generation from JSON: -->
## G-code generation from JSON

The project also supports converting slicer JSON into FDM type 3D-Printer `.gcode` via a separate CLI in `create_g_code/create_g_code.go`.
By default it prints contour perimeters for all layers, and it can also generate fully filled bottom and top solid layers with alternating `45°` / `-45°` toolpaths.
You can also choose how many solid outer wall lines are printed; additional walls are inset inward to preserve the outer dimensions of the part.
Another option is the generation of a zig-zag infill pattern in the middle layers between the bottom and top solid regions.
The infill direction alternates between `45°` and `-45°` on successive infill layers, similar to the alternating direction of the floor and ceiling layers.
A print cooling fan can also be enabled, turned on from a configurable layer onward with a customizable speed, and switched off again at the end of the print.
Brim support is also available for the first layer to increase bed adhesion by printing additional outline lines around the part.
A skirt can also be printed before the initial layer to prime the nozzle; it is generated with a configurable number of lines and kept at least 5mm away from the object or brim.

<!-- Two-step workflow: -->
## Two-step workflow

Currently, the workflow is a two-step process where you first convert STL to JSON, then JSON to G-code.
Current option for JSON to G-code conversion:

```powershell
go run .\stl_to_json_converter -in .\cube_10.stl -layer 0.2 -out .\slices.json
go run .\create_g_code -json-in .\slices.json -gcode-out .\print.gcode
```

Supported G-code parameters:

- `-start-gcode` custom G-code block inserted at file start (`\n` is supported)
- `-end-gcode` custom G-code block appended at file end (`\n` is supported)
- `-offset-x` build plate X offset in mm
- `-offset-y` build plate Y offset in mm
- `-offset-z` build plate Z offset in mm
- `-outer-wall-lines` number of solid outer wall lines to print (minimum `1`)
- `-skirt` enable skirt generation for nozzle priming on the initial layer 
- `-skirt-lines` number of skirt lines to print
- `-brim` enable brim generation on the initial layer for increased bed adhesion
- `-brim-lines` number of brim lines to print
- `-solid-bottom-layers` number of fully printed solid layers at the bottom
- `-solid-top-layers` number of fully printed solid layers at the top
- `-infill` enable zig-zag infill generation for middle layers
- `-infill-density` infill density in percent (`0` to `100`)
- `-cooling-fan` enable print cooling fan control
- `-cooling-fan-layer` layer index at which the print cooling fan turns on (starting from `0` which is the first layer)
- `-cooling-fan-speed` cooling fan speed in percent (`0` to `100`)
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
go run .\create_g_code -json-in .\slices.json -gcode-out .\print.gcode -start-gcode "G28\nG92 E0" -end-gcode "M104 S0\nM140 S0\nM84" -offset-x 0 -offset-y 0 -offset-z 0.0 -outer-wall-lines 2 -skirt true -skirt-lines 4 -brim true -brim-lines 4 -solid-bottom-layers 2 -solid-top-layers 2 -infill true -infill-density 20 -cooling-fan true -cooling-fan-layer 2 -cooling-fan-speed 80 -line-width 0.42 -filament-diameter 1.75 -z-hop-height 0.4 -print-temp 205 -build-plate-temp 60 -print-speed 45
```

<!-- Known limitations: -->
## Known limitations

- `points` remains a legacy flattened toolpath for backward compatibility; the structured `contours` field now preserves loop hierarchy, holes, and open chains per layer.
- Solid outer wall inset uses a simple polygon-offset approach and currently works best for the mostly convex contours produced by the included examples.
- Solid fill and inset/outset shell generation are still tuned for relatively simple contours; very complex meshes can still produce degenerate offsets or ambiguous topology.
- For highly non-manifold or self-intersecting geometry, the converter preserves open chains and closed contour groups, but the exact interpretation of ambiguous topology may still depend on the input mesh quality.
- Floating-point tolerances (`epsilon`) and coordinate rounding can affect point merging and contour simplification for very small features.

<!-- AI-generated-Audit: -->
## AI generated Audit (Updated 12-06-2026):

### Proposal status summary

The repository has progressed from a proof-of-concept slicer into a working two-step pipeline (`STL -> JSON -> G-code`) for the bundled examples and similar simple geometry. The current code supports structured contour data with outer loops, holes, and open chains, and the G-code generator supports configurable walls, solid top/bottom layers, infill, skirt, brim, cooling fan control, retraction, travel moves, and Z-hop.

### What is done

- STL input is parsed and sliced into 2D layers.
- Slice layers are exported as JSON with both legacy `points` and structured `contours`.
- Closed contours are ordered and simplified consistently.
- A separate G-code generator CLI converts the JSON into `.gcode`.
- Printer-specific parameters are configurable via CLI flags.
- Outer wall count is configurable, including inward offset shells for dimensional accuracy.
- Closed bottom and top solid layers are supported with alternating `45°` / `-45°` fill.
- Zig-zag infill is supported with adjustable density.
- Skirt, brim, cooling fan control, retraction, travel moves, and Z-hop are implemented.

### Gap analysis against the proposal

| Proposal area | Current state                                                                   |
|---|---------------------------------------------------------------------------------|
| Basic slicer for a simple cube | Implemented                                                                     |
| STL -> G-code pipeline | Implemented via the intermediate JSON step                                      |
| Start/end printer parameters | Implemented                                                                     |
| Adjustable layer height | Implemented                                                                     |
| Adjustable wall / floor / ceiling thickness | Implemented via outer-wall count and solid top/bottom layer counts              |
| Adjustable infill pattern | Implemented                                                                     |
| Print cooling fan support | Implemented                                                                     |
| Brim / skirt | Implemented                                                                     |
| Complex shapes with holes / overhangs | Partially implemented; holes are supported, broader robustness is still limited |
| Optimization of slicing speed | Comparable for the included objects                                             |
| Dimensional accuracy improvements | Implemented for the supported contour types                                     |
| Benchmarks on cube_10 and Hole_Structure | Done                                                                            |
| Paper/presentation-ready evaluation metrics | Done                                                                            |

### Findings

- The main intended pipeline is in place and suitable for a live demo on the included models.
- The main technical risks are general geometry robustness and offset stability on complex meshes.
- The codebase is better described as a focused educational prototype than a full production slicer.

