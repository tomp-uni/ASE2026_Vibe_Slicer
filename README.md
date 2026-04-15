# ASE2026_Vibe_Slicer

> By: Thomas Martin Pfister  
> Advanced Systems Engineering S2026, University of Salzburg  

<!-- Problem definition: -->
## Problem definition:
A 3D-Object has to be printable on an FDM 3D printer.

<!-- Problem solution: -->
## Problem solution:
Attempt to create a Slicer in the Go programming language for a 3D Printer, using only AI Tools. The program will take a 3D-Object in STL format as input, convert it into a series of 2D layers (slices) and output usable GCODE-Instructions for an FDM Type 3D printer.

<!-- Current Stage: -->
## Current Stage:
Order output to usable toolpath coordinates and start GCODE generation.

<!-- Generative AI assisted README: -->
## Generative AI assisted README:
Simple Go CLI that reads an STL file, slices it from bottom to top every `0.2mm` (or a custom layer height), and outputs 2D `x,y` contour vertices for each layer.

## Usage

Current options:

- `cube_10` A cube object in `10 x 10 x 10`mm size
- `cube_20` A cube object in `20 x 20 x 20`mm size

```powershell
go run . -in .\cube_10.stl
```

Optional flags:

- `-layer` layer height in mm (default `0.2`)
- `-out` write JSON to file instead of stdout

Example:

```powershell
go run . -in .\cube_10.stl -layer 0.2 -out .\slices.json
```

## Output format

Each layer is processed in 3 steps after triangle-plane intersections:

1. Build intersection segments for that `z`.
2. Stitch connected segments into closed contour loops.
3. Remove collinear points (for example, points introduced by STL face triangulation).

So `points` are contour vertices after stitching/simplification, not raw unordered intersection endpoints.

For the included axis-aligned cubes (`cube_10.stl`, `cube_20.stl`), you should typically see 4 points per layer (square contour).

```json
{
  "input": "cube.stl",
  "layer_height_mm": 0.2,
  "layers": [
    {
      "z": 0.2,
      "points": [
        { "x": 10.0, "y": 10.0 },
        { "x": 10.0, "y": 0.0 },
        { "x": 0.0, "y": 0.0 },
        { "x": 0.0, "y": 10.0 }
      ]
    }
  ]
}
```

## Known limitations

- `points` are flattened vertices only. The JSON output does not currently preserve explicit loop/group structure per layer.
- For open or non-manifold geometry, loop stitching may fail and the slicer falls back to unique segment endpoints for that layer.
- Floating-point tolerances (`epsilon`) and coordinate rounding can affect point merging and contour simplification for very small features.
- Output is currently intermediate JSON only. GCODE generation is not implemented yet.

