# Guardian

On-device, real-time hazard detection for Android. The phone camera is the first
sensor; the same detection engine is designed to be reused later against RTSP /
CCTV streams for the enterprise tier.

> **Scope of this milestone.** This is **Milestone 3: real offline fire/smoke
> detection**. The full pipeline runs end-to-end on-device — CameraX → preprocess
> → **TensorFlow Lite YOLOv8 inference** → decode → NMS → multi-frame event
> confirmation → live bounding-box overlay → local alert — with **no network
> access** and **no mock detections**. The one real detector (`:detector:firesmoke`)
> loads a genuine TFLite model from assets; when no model is provisioned it
> reports *detection disabled* and produces zero detections rather than faking
> any.

## What real-time means here

Guardian is a **detection** system, not a prediction system. It reports hazards
that are happening now. The multi-frame *Event Confirmation* stage is a debounce
to suppress single-frame noise (a reflection, a puff of steam) — it is explicitly
**not** forecasting.

## Offline by construction

The app declares **no `INTERNET` permission** (see `app/src/main/AndroidManifest.xml`).
The model is memory-mapped from APK assets; nothing is downloaded at runtime.
Airplane-Mode operation is therefore structural, not a promise — see the test
procedure below.

## Modules

| Module                 | Type            | Responsibility                                                        |
|------------------------|-----------------|-----------------------------------------------------------------------|
| `:core:model`          | Kotlin (JVM)    | Domain types + `ModelSpec`, `ModelStatus`, `ModelMetadata`, metrics.  |
| `:core:detection`      | Kotlin (JVM)    | `Detector`/`DetectionEngine` contracts, YOLOv8 decode, NMS, letterbox, preprocessing, `ViewportMapper`. |
| `:core:camera`         | Android library | `FrameSource` seam + CameraX impl; measures real dropped frames.      |
| `:core:alerts`         | Android library | `AlertController` — vibration, high-importance notification, tone; fires only on confirmed events. |
| `:detector:firesmoke`  | Android library | `FireSmokeDetector` + `TfLiteFireSmokeModel`; the one real detector.  |
| `:feature:monitoring`  | Android library | Monitoring screen (Compose), detection overlay, `MonitoringViewModel`.|
| `:app`                 | Android app     | Application, activity, theme, Hilt wiring (`@IntoSet` detectors).     |

`:core:model` and `:core:detection` carry **no Android dependency** on purpose:
they compile and run unchanged on a JVM, which is what makes the future
CCTV/RTSP reuse a change of frame source rather than a rewrite — and what lets
the decode/NMS/preprocessing logic be unit-tested without a device.

## Tech

Kotlin · Clean Architecture + MVVM · Hilt · Jetpack Compose (Material 3) ·
CameraX (`LifecycleCameraController`) · **TensorFlow Lite** (GPU delegate, CPU
fallback) · Coroutines / Flow. Single source of version truth in
`gradle/libs.versions.toml`.

## Build & run

Requires Android Studio (Ladybug or newer), the Android SDK, and JDK 17. The
Gradle wrapper (8.11.1) **is committed**, so:

```
./gradlew :app:assembleDebug
```

Then run the `app` configuration on a device with a camera and grant the camera
permission (and, on Android 13+, the notification permission) when prompted.

> **Not build-verified in the authoring environment.** These sources were
> authored without an Android SDK / Gradle run available; the JVM-only core is
> unit-tested (49 passing tests) but no APK was produced there. Verify a Gradle
> sync on first open.

## Provisioning the detection model

The fire/smoke detector needs a TensorFlow Lite model that is **not shipped in
this repo** — the evaluated community weights are **AGPL-3.0** and are not
cleared for redistribution. See
[`docs/implementation/MODEL_INTEGRATION_RECORD.md`](docs/implementation/MODEL_INTEGRATION_RECORD.md)
for the full licensing record and the Apache-2.0 (RF-DETR) commercial path.

Quick version:

```
# 1. Export / obtain a float32 YOLOv8 fire/smoke model
pip install ultralytics
yolo export model=path/to/fire_smoke.pt format=tflite imgsz=640

# 2. Drop it in
cp .../fire_smoke_float32.tflite \
   detector/firesmoke/src/main/assets/models/fire_smoke.tflite

# 3. Verify the export matches the sidecar and edit fire_smoke.json if needed
python scripts/inspect_tflite_model.py \
   detector/firesmoke/src/main/assets/models/fire_smoke.tflite
```

Until the `.tflite` exists the app runs normally and shows
*Model not provisioned — detection disabled*. There is **no** mock detection
path. Full instructions:
[`detector/firesmoke/src/main/assets/models/PROVISIONING.md`](detector/firesmoke/src/main/assets/models/PROVISIONING.md).

## Verifying offline (Airplane-Mode test)

1. Provision the model (above) and install the debug APK.
2. Enable **Airplane Mode** on the device (Wi-Fi and mobile data off).
3. Start monitoring and point the camera at a fire/smoke source (a real one, or a
   screen/video showing one).
4. Confirmed events raise on-screen boxes **and** a local alert (vibration +
   notification + tone) with no connectivity. The status panel shows the model as
   loaded and the metrics line updating (latency / FPS / dropped / detections).

Because there is no `INTERNET` permission, the app *cannot* reach a network even
if it tried — the offline property is enforced by the manifest, not by the toggle.

## Replacing the model / adding a detector

**Swap the model:** drop a new `.tflite` into the firesmoke assets, run the
inspection script, and update `fire_smoke.json` (input size, normalization,
`outputFormat` — `YOLOV8_DETECT` or the NMS-free `NMS_FREE_DETECT` for
DETR-style models — and `coordinatesNormalized`). No code change needed for a
compatible export.

**Add a new detector:** implement `Detector` in a new `:detector:<name>` module
and bind it into the engine with Hilt:

```kotlin
@Provides @Singleton @IntoSet
fun provideMyDetector(...): Detector = MyDetector(...)
```

The `DetectionEngine` unions every detector's `types`, routes frames to the ones
whose type is enabled, and exposes their `modelStatus` — so a new hazard type is
additive and needs no changes to the monitoring UI or the pipeline.

## Documentation

- [`docs/implementation/MODEL_INTEGRATION_RECORD.md`](docs/implementation/MODEL_INTEGRATION_RECORD.md) — model choice, licensing gate, verified I/O contract, commercial path.
- [`docs/implementation/MVP_IMPLEMENTATION_REPORT.md`](docs/implementation/MVP_IMPLEMENTATION_REPORT.md) — architecture, what's real vs pending, verification, build blocker.
- [`docs/research/`](docs/research/) — pre-implementation research (models, datasets, decision matrix, acceptance plan).

## Roadmap (post-milestone-3)

4. Alert engine hardening (full-screen alert, flashlight, capture, persist).
5. Room-backed event history; per-detector settings; emergency contacts.
6. Camera-tampering detector (classic CV — cover / blur / scene-shift; no ML).
7. Fall + intrusion (zones) detectors.
8. RF-DETR (Apache-2.0) trained fire/smoke model for the commercial tier.
9. Alert delivery backend (SMS / call / email via provider — device-side SMS is
   blocked by Google Play policy, so this is routed server-side).
