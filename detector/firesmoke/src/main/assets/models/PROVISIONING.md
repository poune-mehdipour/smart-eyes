# Model provisioning — fire/smoke weights

The app loads its detection model **from these assets, at build time**. It never
downloads a model at runtime (there is no INTERNET permission).

This folder ships the **spec sidecar** (`fire_smoke.json`) but **not** the
weights file, because the evaluated community weights are **AGPL-3.0** and are
**not cleared for redistribution inside this repository or a commercial APK**.
See `docs/implementation/MODEL_INTEGRATION_RECORD.md` for the full licensing
record.

## What you must add

Drop a TensorFlow Lite model here:

```
detector/firesmoke/src/main/assets/models/fire_smoke.tflite
```

Until that file exists, the app runs normally but shows **"Model not
provisioned — detection disabled"** and produces **zero** detections. This is
deliberate: there is no mock/fake detection path.

## Requirements the file must meet

The committed `fire_smoke.json` describes what the pipeline expects:

- YOLOv8 **detect** head, 2 classes in order `["fire", "smoke"]`
- Input `640 x 640 x 3`, **NHWC**, **float32**, RGB, normalised to `[0,1]`
- Output `[1, 6, 8400]` (channels-first) **or** `[1, 8400, 6]` (auto-detected),
  float32
- Box coords normalised to `[0,1]` of the input (`coordinatesNormalized: true`)

## Verify before trusting the overlay

Different exports differ (coord normalisation, output orientation, int8 vs
float). **Run the inspection script against your actual file** and edit
`fire_smoke.json` to match:

```
python scripts/inspect_tflite_model.py detector/firesmoke/src/main/assets/models/fire_smoke.tflite
```

If the script reports pixel-space coordinates, set `coordinatesNormalized:false`.
If it reports an int8 input/output tensor, this MVP will surface a clear
`Model failed to load` error rather than mis-decode — re-export a float32 model.

## Producing a float32 .tflite from YOLOv8 weights

```
pip install ultralytics
yolo export model=path/to/fire_smoke.pt format=tflite imgsz=640
# use the ...float32.tflite output; rename to fire_smoke.tflite
```
