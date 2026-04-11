# ASE2026_Vibe_Slicer

> By: Thomas Martin Pfister  
> Advanced Systems Engineering S2026, University of Salzburg  

An attempt to create a Slicer in the Go programming language for a 3D Printer, using only AI Tools. The program will take a 3D Object in STL format as input, and output usable GCODE for a FDM 3D printer.

Current Stage: Conversion of STL file to usable intermediate JSON data in layers for further use.

AI genereated Readme:

Simple Go CLI that reads an STL file, slices it from bottom to top every `0.2mm` (or a custom layer height), and outputs 2D `x,y` coordinates for each layer.

## Usage

```powershell
go run . -in .\model.stl
```

Optional flags:

- `-layer` layer height in mm (default `0.2`)
- `-out` write JSON to file instead of stdout

Example:

```powershell
go run . -in .\model.stl -layer 0.2 -out .\slices.json
```

## Output format

```json
{
  "input": "model.stl",
  "layer_height_mm": 0.2,
  "layers": [
    {
      "z": 0,
      "points": [
        { "x": 10.0, "y": 4.2 },
        { "x": 11.1, "y": 5.0 }
      ]
    }
  ]
}
```
