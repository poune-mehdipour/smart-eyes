# MVP Implementation Report — Offline Fire/Smoke Detection

**Milestone 3.** Real, offline, on-device fire/smoke detection from the phone
camera: CameraX → preprocess → TFLite YOLOv8 inference → decode → NMS →
multi-frame event confirmation → live overlay → local alert. Runs in Airplane
Mode. No mock detections anywhere.

This report describes what was built, what is real versus pending, how it was
verified, and the one hard blocker (no Android SDK / Gradle in the authoring
environment → no APK produced here).

---

## 1. Pipeline (end to end)

```
CameraX frame (YUV_420_888)
  └─ ImageProxy → NV21 (+ rotation metadata)                [:core:camera]
      └─ FramePreprocessor: NV21 → RGB, rotate upright,      [:core:detection]
         letterbox to 640×640 (grey 114), NHWC float32 [0,1]
          └─ TfLiteFireSmokeModel.run(FloatArray)            [:detector:firesmoke]
             GPU delegate if available, else multi-thread CPU
              └─ YoloV8PostProcessor: decode [1,6,8400],     [:core:detection]
                 argmax classes, confidence gate, per-class NMS,
                 letterbox→source normalised boxes
                  └─ DetectionEngine routes by enabled type   [:core:detection]
                      └─ EventConfirmationEngine: sliding-window
                         multi-frame confirmation (debounce)
                          └─ confirmed SecurityEvent
                             ├─ AlertController: vibrate + notification + tone  [:core:alerts]
                             └─ DetectionOverlay: live boxes on preview          [:feature:monitoring]
```

Every stage is implemented. The only thing that stops real boxes from appearing
is the **absence of a weights file** (`fire_smoke.tflite`), which is a deliberate
licensing decision (see `MODEL_INTEGRATION_RECORD.md`), not a missing feature.

---

## 2. Module map

New modules this milestone in **bold**.

| Module | Type | Responsibility |
|--------|------|----------------|
| `:core:model` | Kotlin (JVM) | Domain types + `ModelSpec`, `ModelStatus`, `ModelMetadata`, `InferenceMetrics` |
| `:core:detection` | Kotlin (JVM) | `Detector`/`DetectionEngine` contracts, YOLOv8 decode, NMS, letterbox, preprocessing, post-processing, metrics aggregation, `ViewportMapper` |
| `:core:camera` | Android lib | `FrameSource` seam + CameraX impl; now counts real dropped frames |
| **`:core:alerts`** | **Android lib** | **`AlertController` — vibration, high-importance notification, tone; fires only on confirmed events** |
| **`:detector:firesmoke`** | **Android lib** | **`FireSmokeDetector` + `TfLiteFireSmokeModel` + `ModelSpecJson`; the one real detector** |
| `:feature:monitoring` | Android lib | Compose monitoring screen, overlay, `MonitoringViewModel` wiring the whole pipeline |
| `:app` | Android app | Hilt DI (detector `@IntoSet`, alerts, engine), manifest, theme |

The Android-free boundary is intact: `:core:model` and `:core:detection` carry
**no Android dependency** and are the modules under JVM test. This is what makes
the future CCTV/RTSP path a change of `FrameSource`, not a rewrite.

---

## 3. What is real

- **Real preprocessing.** `FramePreprocessor` does a genuine single-pass NV21→RGB
  conversion, upright rotation for 0/90/180/270°, and letterbox to 640 with
  buffer reuse. Its inverse mapping is unit-tested.
- **Real decode.** `YoloV8Decoder` reads the channels-first `[1, 4+C, N]` tensor,
  does argmax over class channels, and scales coordinates per the sidecar. It
  throws on channel/buffer mismatch rather than guessing.
- **Real NMS.** Per-class IoU suppression with configurable thresholds.
- **Real inference wrapper.** `TfLiteFireSmokeModel` wraps a genuine TFLite
  `Interpreter`, prefers a GPU delegate, validates tensor dtype/shape at load,
  auto-detects and transposes anchors-first output, reuses direct `ByteBuffer`s,
  and measures per-inference latency.
- **Real event confirmation.** Sliding-window multi-frame confirmation debounces
  single-frame noise before anything is called an event.
- **Real alerts.** Vibration + high-importance notification + `ToneGenerator`
  beep, each capability guarded, firing **only** on confirmed events.
- **Real overlay alignment.** `DetectionOverlay` maps normalised boxes through
  `ViewportMapper`, which reproduces `PreviewView` FILL_CENTER cover/crop math —
  and that math is unit-tested, so boxes line up with what the camera shows.
- **Real metrics.** Latency, FPS, dropped frames (an actually-measured counter in
  `CameraXFrameSource`), confirmed-event counts — surfaced live in the UI.
- **Real offline guarantee.** No `INTERNET` permission is declared (see the
  manifest comment). The model is memory-mapped from assets; nothing is fetched
  at runtime. This is what makes Airplane-Mode operation structural, not a claim.

## 4. What is pending / not real yet

- **Weights are not shipped.** AGPL-3.0 blocks redistribution; the app shows
  *Model not provisioned — detection disabled* and returns zero detections until
  a `fire_smoke.tflite` is provided. **No mock fills the gap.**
- **No commercial-clean model yet.** RF-DETR (Apache-2.0) is the documented path
  but needs a training/export run (see `MODEL_INTEGRATION_RECORD.md` §6).
- **Thresholds are untuned placeholders.** Confidence, IoU, and frames-to-confirm
  are reasonable defaults, not values validated against a labelled
  false-positive set.
- **Sidecar values are defaults-to-verify.** `coordinatesNormalized` and output
  orientation in `fire_smoke.json` are standard-export expectations; they must be
  confirmed against the actual provisioned file with
  `scripts/inspect_tflite_model.py`.

---

## 5. Verification performed

- **JVM unit/integration tests: `OK (49 tests)`.** Covering YOLOv8 decode, NMS,
  letterbox transform + inverse, frame preprocessing, sliding-window
  confirmation, metrics aggregation, detection-engine routing, and viewport
  mapping. Run against `:core:model` + `:core:detection` (the Android-free
  modules) with kotlinc + JUnit 4.
- **Sidecar parser verified in isolation.** `ModelSpecJson.parse` was compiled
  against `org.json` and run against the committed `fire_smoke.json` plus a
  minimal-defaults case — both parse to the expected `ModelSpec`.
- **YOLOv8 output format cross-checked** against Ultralytics' documented TFLite
  detect export (channels-first `[1, 4+C, N]`, sigmoid class scores, no
  objectness) before the decoder was written.

### What could NOT be verified here

- **No APK build.** The authoring environment has **no Android SDK and no
  Gradle** (`ANDROID_SDK_ROOT`/`ANDROID_HOME` unset). Android-module Kotlin
  (anything depending on the Android framework, TFLite, or Compose) therefore
  **cannot be compiled here**, and `:app:assembleDebug` cannot run. This is a
  hard environment limitation, disclosed rather than worked around.
- **No on-device inference run.** With no weights and no device, no real
  detection has been observed executing on hardware. The pipeline is complete and
  the JVM-testable parts are green, but "a real model producing real detections
  on a device" has **not** been demonstrated from this environment.

---

## 6. How to finish the last mile (on a real machine)

1. Open the project in Android Studio (Ladybug+) / have the Android SDK + JDK 17.
   The Gradle wrapper JAR is **now committed** (Gradle 8.11.1), so `./gradlew`
   works directly.
2. Provision a float32 YOLOv8 fire/smoke `.tflite` per
   `MODEL_INTEGRATION_RECORD.md` §3 and run the inspection script to reconcile the
   sidecar.
3. `./gradlew :app:assembleDebug`, install on a camera device, grant camera (and
   notification, API 33+) permissions.
4. Point at a fire/smoke source (or a screen showing one). Confirmed events raise
   boxes + a local alert. Enable Airplane Mode to prove offline operation.

---

## 7. Honest status

**PARTIALLY COMPLETE.** The full real pipeline and Android app are implemented;
the Android-free core is unit/integration-tested (49 passing); the model
licensing is resolved honestly (evaluation-only, no shipped weights, documented
commercial path). It is **not COMPLETE** because no APK could be built and no
real on-device detection could be demonstrated in this environment (no
SDK/Gradle/device), and because the model must be manually provisioned before the
app detects anything.
