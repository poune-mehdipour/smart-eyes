# Model Integration Record — Fire / Smoke

This document is the single source of truth for **which** model the fire/smoke
detector integrates against, **why** it was chosen, its **licensing status**,
its **verified I/O contract**, and the **commercial-clean replacement path**. It
exists so the model choice is auditable and so a later engineer can swap the
weights without reverse-engineering the pipeline.

---

## 1. Summary

| Field | Value |
|-------|-------|
| Task | On-device fire + smoke object detection from camera frames |
| Model family integrated | Ultralytics **YOLOv8** detect head, 2 classes |
| Classes (fixed order) | `["fire", "smoke"]` |
| Runtime | TensorFlow Lite (`org.tensorflow:tensorflow-lite` 2.16.1), GPU delegate when available, CPU fallback |
| License of evaluated weights | **AGPL-3.0** |
| Commercial status | **EVALUATION_ONLY** (surfaced in the UI and in metadata) |
| Weights shipped in repo? | **No** — sidecar spec only; weights are provisioned manually |
| Commercial-clean path | **RF-DETR** (Apache-2.0) — documented in §6, not yet trained/exported |

The pipeline is **real**: preprocessing, TFLite inference, YOLOv8 decode, NMS,
and multi-frame event confirmation are all implemented and exercised. What is
gated is **redistribution of weights**, not the code path.

---

## 2. Why YOLOv8, and the licensing gate

The requirement was a ready-to-run offline fire/smoke model. A survey of
publicly available fire/smoke detection weights (see `docs/research/models.md`
and `docs/research/DECISION.md`) found that the overwhelming majority of
ready-made community weights are **Ultralytics YOLOv8** checkpoints.

Ultralytics YOLOv8 is licensed **AGPL-3.0**. AGPL-3.0 is a strong copyleft
license: shipping those weights (or a derivative) inside a distributed
application triggers source-disclosure obligations for the conveying work, and
is **not** compatible with a closed commercial APK without a separate commercial
license from Ultralytics.

Consequences enforced in this repo:

1. **No weights file is committed.** We cannot license-clear redistribution of
   AGPL-3.0 weights inside this repository or a commercial build, so the `.tflite`
   file is deliberately absent. Only the **spec sidecar** (`fire_smoke.json`) is
   committed.
2. **The model is marked `EVALUATION_ONLY`.** `ModelMetadata.commercialUse`
   carries this through to the UI, which shows an explicit *EVALUATION ONLY —
   not cleared for commercial distribution* banner whenever such a model is
   loaded. There is no way to load an AGPL model and have the app present it as
   commercially clean.
3. **No mock fallback.** When weights are absent the app reports *Model not
   provisioned — detection disabled* and produces zero detections. It never
   fabricates boxes to paper over the missing model.

This keeps the engineering honest: the pipeline can be **evaluated** end-to-end
today with community weights, while the commercial story is explicitly unresolved
until a clean model is dropped in.

---

## 3. Provisioning (how to make it actually detect)

The detector loads its model from module assets at:

```
detector/firesmoke/src/main/assets/models/fire_smoke.tflite   <- YOU add this
detector/firesmoke/src/main/assets/models/fire_smoke.json     <- committed sidecar
```

Steps:

1. Obtain or export a YOLOv8 fire/smoke `.tflite` (float32). To export from a
   `.pt` checkpoint:
   ```
   pip install ultralytics
   yolo export model=path/to/fire_smoke.pt format=tflite imgsz=640
   # use the *_float32.tflite output; rename to fire_smoke.tflite
   ```
2. Copy it to the assets path above.
3. **Verify the export matches the sidecar** (exports vary):
   ```
   python scripts/inspect_tflite_model.py \
       detector/firesmoke/src/main/assets/models/fire_smoke.tflite
   ```
   The script prints input/output shapes, dtypes, quantization, detected layout
   (NHWC/NCHW), and detected output orientation (channels-first vs anchors-first),
   and suggests the sidecar field values. Edit `fire_smoke.json` if they differ
   (most commonly `coordinatesNormalized`).
4. Rebuild. The app now runs real inference offline.

See `detector/firesmoke/src/main/assets/models/PROVISIONING.md` for the
asset-local copy of these instructions.

---

## 4. Verified I/O contract

The decode path was written against the **documented and cross-checked** YOLOv8
detect output format, not an assumption:

**Input**
- Shape `[1, 640, 640, 3]`, **NHWC**, **float32**
- RGB channel order
- Normalised to `[0, 1]` (`SCALE_0_1`)
- Letterboxed to 640×640 with grey pad value 114, aspect ratio preserved

**Output**
- Shape `[1, 4 + C, N]` — **channels-first**, no objectness channel
  - For 2 classes: `[1, 6, 8400]`
  - Channel layout per anchor: `cx, cy, w, h, score_class0, score_class1`
  - Class scores are **already sigmoid-activated** (no separate objectness to
    multiply in)
- The loader **auto-detects orientation**: if the tensor is anchors-first
  (`[1, N, 4+C]`, i.e. `[1, 8400, 6]`) it is transposed to channels-first before
  decode, by matching the `4 + classCount` dimension.
- Box coordinates: TFLite exports typically normalise `cx,cy,w,h` to `[0, 1]` of
  the input. The sidecar flag `coordinatesNormalized` controls this; when true,
  the decoder scales by input W/H. **This must be confirmed per-model** with the
  inspection script — a wrong value produces mislocated boxes, which is exactly
  the kind of silent error we refuse to ship blind.

**Post-processing**
- Argmax over class channels → class id + score
- Confidence gate (`DetectionConfig.confidence`, default 0.35; per-type override
  supported)
- Per-class NMS (`DetectionConfig.iou`, default 0.45)
- Letterbox coordinates mapped back to the original frame, then normalised to
  `[0, 1]` of the source frame for overlay-independent rendering

**Load-time validation (fail loud, never mis-decode)**
- Input tensor dtype and element count are checked against the spec
- Output tensor dtype must be `FLOAT32`; an int8/quantized output raises a clear
  `ModelLoadException` → `ModelStatus.Error` with an actionable message rather
  than silently producing garbage boxes

---

## 5. Runtime / delegate behaviour

- Loader prefers a **GPU delegate** via `CompatibilityList` when the device
  supports it, and falls back to **multi-threaded CPU** otherwise.
- If the GPU interpreter constructor throws, it retries once on CPU.
- The bound delegate (GPU/CPU/NNAPI) is recorded in `ModelMetadata.delegate` and
  shown in diagnostics.
- The model is memory-mapped from assets (`FileChannel.map`), and TFLite build
  is configured with `noCompress += "tflite"` so the asset can be mapped directly.

---

## 6. Commercial-clean replacement path — RF-DETR (Apache-2.0)

The blocker for commercial use is **only** the AGPL weights, not the app. The
documented clean path:

- **RF-DETR** (Roboflow) is **Apache-2.0** for both code and released weights —
  redistributable inside a commercial APK.
- It is a DETR-style detector; its output is **NMS-free** (`NMS_FREE_DETECT`),
  which the `ModelSpec.OutputFormat` enum and the post-processor already account
  for. Swapping to RF-DETR is therefore: train/fine-tune on a fire/smoke dataset,
  export to TFLite float32, set the sidecar `outputFormat` to `NMS_FREE_DETECT`,
  drop in the weights, run the inspection script, done.
- **Status: not yet trained/exported.** RF-DETR ships no ready-made fire/smoke
  checkpoint, so this path requires a training run against a fire/smoke dataset
  (candidate datasets are catalogued in `docs/research/datasets.md`). Until then,
  YOLOv8-evaluation is the only runnable option.

This is the honest state: **runnable now under AGPL/evaluation; commercially
clean once RF-DETR is trained.** The code supports both without modification.

---

## 7. What is NOT claimed

- No accuracy / mAP numbers are asserted — none were measured on-device here.
- The confirmation thresholds (frames-to-confirm, per-type confidence) are
  **untuned placeholders**, not validated against a labelled false-positive set.
- The `coordinatesNormalized` flag and output orientation in the committed
  sidecar are the **expected** values for a standard YOLOv8 TFLite export; they
  are asserted as defaults to verify, not as measured facts about a specific
  weights file (no weights file exists in the repo to measure).
