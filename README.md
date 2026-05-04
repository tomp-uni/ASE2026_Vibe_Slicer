# ASE2026_Vibe_Slicer

> By: Thomas Martin Pfister  
> Advanced Systems Engineering S2026, University of Salzburg  

<!-- Problem definition: -->
## Problem definition:
Slicers are used to convert 3D-Objects into a series of 2D layers (slices) and generate GCODE-Instructions for 3D printers.
On the surface, this seems like a straightforward task, but in practice it is a quite complex problem that requires a deep understanding of 3D geometry, printer mechanics and material properties to produce accurate and reliable prints.
For this reason, the development of Slicers, which is usually done by printer manufacturers and therefore closed source, takes a lot of time and resources.

Since AI programming tools have made quite significant advancements in terms of code generation and complex problem-solving in recent years, it raises the question, whether it is possible to utilize these tools in order to develop a Slicer, without the need of the resources needed for traditional methods and a deep understanding of the underlying problem.
Moreover, is it possible to achieve sufficiently accurate and reproducible print results.

<!-- Research Questions: -->
## Research Questions:
1. Is it possible to create a basic easy to use Slicer without the need of a deep understanding of the underlying problem, using only AI Tools?
2. If so, how does the resulting program perform in terms of slice time, print time and dimensional accuracy (theoretical and practical) in comparison to a regular Slicer?

<!-- Problem solution: -->
## Problem solution:
Attempt to create a basic easy to use Slicer in the Go programming language for a Fused Deposition Modeling (FDM) Type 3D Printer, using only AI Tools. 
The program will take a 3D-Object in STL format as input, convert it into a series of 2D layers (slices), take custom printer specific parameters and output usable GCODE-Instructions for a Fused Deposition Modeling (FDM) Type 3D printer.
If successful, the resulting Slicer will then be evaluated against the popular open source Slicer Cura, in terms of slice time, print time and dimensional accuracy (theoretical and practical) through specific Micro-benchmarks.

<!-- Current Stage: -->
## Current Stage:
- Verifying that the generated GCODE is valid and safe. Tweaking/fine-tuning GCODE generation.
- Implementing infill patterns.
- Implementing print fan cooling support.
- Tbd.

<!-- Generative AI assisted README: -->
## Generative AI assisted README:

<!-- Overview: -->
## Overview
Simple Go CLI that reads an STL file, slices it from bottom to top every `0.2mm` (or a custom layer height), and outputs 2D `x,y` contour vertices for each layer. 
The output is a JSON file containing an array of layers, where each layer has a `z` height and an array of `points` representing the contour vertices for that layer. 
The points are ordered as a clockwise toolpath and rotated so the first point is the smallest corner (`min x`, then `min y`). 
The program also includes a separate CLI for converting the JSON output into FDM 3D-Printer G-code, with customizable parameters for print settings and start/end G-code blocks.

<!-- Usage of the STL to JSON converter: -->
## Usage of the STL to JSON converter

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
- `-solid-bottom-layers` number of fully printed solid layers at the bottom
- `-solid-top-layers` number of fully printed solid layers at the top
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
go run .\create_g_code -json-in .\slices.json -gcode-out .\print.gcode -start-gcode "G28\nG92 E0" -end-gcode "M104 S0\nM140 S0\nM84" -offset-x 0 -offset-y 0 -offset-z 0.0 -outer-wall-lines 2 -solid-bottom-layers 2 -solid-top-layers 2 -line-width 0.42 -filament-diameter 1.75 -z-hop-height 0.4 -print-temp 205 -build-plate-temp 60 -print-speed 45
```

<!-- Known limitations: -->
## Known limitations

- `points` are flattened vertices only. The JSON output does not currently preserve explicit loop/group structure per layer.
- Solid outer wall inset uses a simple polygon-offset approach and currently works best for the mostly convex contours produced by the included examples.
- Solid bottom/top fill currently assumes one closed outer contour per layer, which matches the included cube examples.
- For open or non-manifold geometry, loop stitching may fail and the slicer falls back to unique segment endpoints for that layer (ordering is then not guaranteed to be a valid toolpath).
- Floating-point tolerances (`epsilon`) and coordinate rounding can affect point merging and contour simplification for very small features.
- JSON and G-code generation are separate CLIs; ensure your JSON input path and printer parameters are set correctly when running `create_g_code`.

<!-- AI-generated-Audit: -->
## AI generated Audit:

### Proposal status summary

The current repository has progressed from a proof-of-concept slicer into a functional two-step pipeline (`STL -> JSON -> G-code`) for the included cube models. The core objective from the proposal has been achieved: a simple STL model can be sliced, converted into ordered contour vertices, and translated into printable G-code with configurable printer parameters.

The project is now beyond the initial milestone described in the proposal, but it is still **not yet a full general-purpose slicer**. It works well for the bundled closed cube examples and similar simple geometry, while more advanced geometry handling and slicer features remain incomplete.

### What is already done

- STL input is parsed and sliced into 2D layers.
- Layer vertices are exported as JSON.
- Contours are ordered into a clockwise toolpath and simplified.
- A separate G-code generator CLI converts the JSON into `.gcode`.
- Printer-specific parameters are configurable via CLI flags.
- Outer wall count is configurable, including inward offset shells for dimensional accuracy.
- The first outer wall is centered half a line width inward, which improves outer dimension accuracy.
- Closed bottom and top solid layers are supported with alternating `45°` / `-45°` fill.
- Regression tests exist for command ordering, layer handling, shell placement, and solid-layer behavior.

### Gap analysis against the proposal

| Proposal area | Current state                                                      |
| --- |--------------------------------------------------------------------|
| Basic slicer for a simple cube | Implemented                                                        |
| STL -> G-code pipeline | Implemented via the intermediate JSON step                         |
| Start/end printer parameters | Implemented                                                        |
| Adjustable layer height | Implemented                                                        |
| Adjustable wall / floor / ceiling thickness | Implemented via outer-wall count and solid top/bottom layer counts |
| Adjustable infill pattern | Not yet implemented                                                |
| Complex shapes with holes / overhangs | Not yet robust enough                                              |
| Optimization of slicing speed | Not benchmarked yet                                                |
| Dimensional accuracy improvements | Partially addressed and test-covered                               |
| Add-ons such as brim / raft / skirt | Not yet implemented                                                |
| Paper/presentation-ready evaluation metrics | Not yet collected in a reproducible form                           |

### Findings

- The project currently demonstrates the **main intended pipeline** and is suitable for showing a live demo on simple models.
- The most important remaining technical risk is **general geometry robustness**: the JSON format currently stores flattened contour points, which limits support for multiple loops, holes, and more complex parts.
- The next biggest gap is **infill**. Bottom/top solid layers are present, but there is no general adjustable infill strategy for internal layers.
- A scientific paper will still need **benchmarking data** against a mature slicer (for example Cura) for slice time, print time, and dimensional accuracy.
- The codebase is functional, but still better described as a **focused educational prototype** than a full production slicer.

### Milestones already completed

1. Basic STL parsing and layer slicing.
2. JSON export of slice layers.
3. Clockwise contour ordering and simplification.
4. Separate JSON-to-G-code generation stage.
5. Configurable printer start/end G-code and motion/temperature parameters.
6. Retraction, travel moves, and Z-hop support.
7. Inward outer-wall offset handling for dimensional accuracy.
8. Multiple outer wall lines with inward insetting.
9. Closed bottom and top solid layers with alternating diagonal fill.
10. Automated tests for the main generation behaviors.

### TODO list

#### High priority

- Support multiple contours per layer in the JSON model.
- Preserve explicit loop/group structure instead of flattened points only.
- Make slicing robust for models with holes and more complex outlines.
- Add a real infill strategy for non-solid interior layers.
- Add reproducible benchmark comparisons against Cura for slice time and print time.

#### Medium priority

- Add basic support for brim / skirt / raft build-plate adhesion helpers.
- Improve non-manifold and thin-feature handling.
- Harden printer profile validation and startup/shutdown safety checks.
- Add an output mode for machine-readable debugging or metrics.

#### Low priority

- Add more example models and expected-output fixtures.
- Produce presentation slides and paper figures from the current metrics.
- Document the current limitations more explicitly for users.

### Overall assessment

The project is in a good state for a university presentation: the pipeline is working, the core milestones are implemented, and the code can demonstrate meaningful slicing-to-G-code behavior. For the scientific paper, the remaining work should focus on measurable comparisons, robustness on more complex geometry, and a reproducible evaluation setup.

