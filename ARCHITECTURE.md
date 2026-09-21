# Architecture

## One idea holds the whole thing together

> Frames come from *a* source. Everything after the source is source-agnostic.

```
FrameSource ──▶ DetectionEngine ──▶ EventConfirmationEngine ──▶ AlertEngine
 (CameraX now,      (runs enabled       (multi-frame debounce      (milestone 4)
  RTSP later)        detectors)          → SecurityEvent)
```

`FrameSource` emits `AnalysisFrame` — a plain, normalised frame (NV21 bytes +
geometry + timestamp). No CameraX type crosses that boundary. The enterprise
CCTV tier adds an `RtspFrameSource` and rebinds one Hilt provider; the engine,
pipeline, storage and UI are untouched. That is the commercial thesis expressed
as a type boundary.

## Layering

- **Domain** (`:core:model`) — pure data. No Android, no coroutines.
- **Engine** (`:core:detection`) — contracts + orchestration + confirmation.
  Pure Kotlin + coroutines so it runs on a phone *or* a server.
- **Platform** (`:core:camera`) — the Android-specific frame source.
- **Feature** (`:feature:monitoring`) — MVVM screen; a single `StateFlow`
  snapshot drives the UI.
- **App** (`:app`) — composition root; Hilt binds implementations to contracts.

## Extensibility: one capability = one module

A detector implements `Detector` (`type` + `suspend detect(frame)`), lives in its
own module, and is contributed with `@IntoSet`. Adding fire detection touches:

1. a new `:detector:fire` module, and
2. one `@IntoSet` line in a Hilt module.

The engine, event pipeline, ViewModel and UI do not change. The `DetectorType`
enum plus the empty multibinding are what make "50 scenarios later" an additive
exercise rather than a refactor.

## Concurrency

- CameraX analysis runs on a dedicated single-thread executor with
  `STRATEGY_KEEP_ONLY_LATEST`; slow analysis drops frames instead of queueing.
- Frames are further throttled to `targetFps` (default 4) at the source — the
  cheapest defence against battery drain and thermal throttling.
- The frame stream is a `SharedFlow` with a 1-slot buffer and drop-oldest
  overflow. The ViewModel collects on `Dispatchers.Default`.

## Edge ↔ cloud (added with the Cloud Connector milestone)

The same seam philosophy extends off the device. `:core:cloud` is another
pure-JVM module: `CloudReporter` is the contract the monitoring pipeline
sees, `NoOpCloudReporter` is bound in the `offline` flavor (which keeps no
INTERNET permission) and a queueing, retrying reporter in the `connected`
flavor. Reporting happens strictly *after* the local alert and is
fire-and-forget — cloud availability can never affect detection. The cloud
side lives in `cloud-connector/` (Go) and has its own documentation; the
wire contract is mirrored by `CloudContract.kt` and
`cloud-connector/internal/ingest`.

## Deliberate non-decisions (documented, not forgotten)

- **Confidence is not calibrated.** The model score is shown as a health signal,
  never surfaced as a "% chance". Calibration is a prerequisite before any
  auto-escalation.
- **No device-side SMS/dialing.** Google Play restricts SMS/Call-Log permissions
  to default-handler apps; alert *delivery* is therefore a server-routed concern
  (later milestone), separate from alert *detection*.
- **Single-module-per-concern, not per-detector, yet.** Detector modules are
  added as detectors are, to keep milestone 1 buildable and reviewable.
