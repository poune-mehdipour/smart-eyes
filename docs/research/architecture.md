# Architecture

This document explains *why* the architecture is shaped the way it is. For the
as-built description of Milestone 1, see [`/ARCHITECTURE.md`](../../ARCHITECTURE.md)
at the repo root; this is the reasoning behind it.

## The one decision everything else hangs off

> Frames come from *a* source. Everything after the source is source-agnostic.

```
FrameSource ──▶ DetectionEngine ──▶ EventConfirmationEngine ──▶ AlertEngine
 (CameraX now,      (runs enabled       (multi-frame debounce      (Milestone 4,
  RTSP later)        detectors)          → SecurityEvent)            server-routed)
```

The commercial thesis of this product is "phone camera today, enterprise CCTV
tomorrow, same brain." If that thesis is real, it has to be expressible as a
**type boundary**, not a promise in a slide. It is: `FrameSource` emits an
`AnalysisFrame` — width, height, rotation, timestamp, and NV21 pixel bytes — and
nothing downstream of it knows CameraX exists. Moving to CCTV is adding an
`RtspFrameSource` and rebinding one Hilt provider. The engine, the confirmation
pipeline, storage, and UI do not change.

This is worth stating plainly because it is the single decision that, if wrong,
forces a rewrite later. Everything below is downstream of protecting this seam.

### Why NV21 at the seam

`AnalysisFrame` carries pixels as NV21 (Y plane followed by interleaved V/U). It
is the most broadly accepted camera-frame layout on Android and the cheapest
common denominator between CameraX's `YUV_420_888` output and what an RTSP
decoder produces. Detectors convert NV21 → whatever their runtime needs (RGB
tensor for a vision model, grayscale for classical CV) at the edge of their own
module. The seam stays format-stable; conversion cost lives with the consumer
that incurs it.

One consequence to keep in mind: on-device model runtimes want RGB/float input,
so every ML detector pays a NV21→RGB conversion. That conversion is a real
per-frame cost and belongs in the benchmark budget (see
[benchmarking.md](benchmarking.md)), not hidden.

## Layering, and why two layers carry no Android dependency

| Layer | Module | Android dep? | Why |
|-------|--------|:---:|-----|
| Domain | `:core:model` | No | Pure data. Compiles on a JVM server unchanged. |
| Engine | `:core:detection` | No | Contracts + orchestration + confirmation. Runs on phone *or* server. |
| Platform | `:core:camera` | Yes | The Android-specific frame source lives here and nowhere else. |
| Feature | `:feature:monitoring` | Yes | MVVM screen; one `StateFlow` snapshot drives the UI. |
| App | `:app` | Yes | Composition root; Hilt binds implementations to contracts. |

`:core:model` and `:core:detection` are Android-free **on purpose**. This is what
makes the RTSP/CCTV future a change of frame source rather than a rewrite: the
detection brain already runs on a JVM, so it can run in a server-side worker
against a decoded RTSP stream with zero source changes. The Android-free
constraint is not neatness for its own sake — it is the mechanism that keeps the
"same brain" claim true.

## Extensibility: one capability = one module

A detector is small: implement `Detector` (`type: DetectorType` + `suspend
detect(frame): List<Detection>`), live in your own module, and get contributed to
the engine with a single `@IntoSet` Hilt binding. Adding fire detection touches:

1. a new `:detector:fire` module, and
2. one `@IntoSet` line.

The engine, event pipeline, ViewModel, and UI do not change. The `DetectorType`
enum plus an (initially empty) multibinding are what turn "50 detection scenarios
eventually" into an *additive* exercise instead of a series of refactors. This is
directly relevant to the 50-item scenario list: the architecture's job is to make
each new scenario a new module, never a change to the core.

### Why detector modules are added lazily, not upfront

Milestone 1 ships **zero** detector modules. That is deliberate. Scaffolding 5–50
empty modules upfront is speculative structure that has to be maintained and kept
buildable before it earns anything. Instead the pipeline runs end-to-end *now*
with an empty detector set, so the seam is exercised, not stubbed — and each
detector module lands when its detector does. The cheapest possible first detector
(camera-tampering, classical CV, no model) proves the `Detector → @IntoSet →
engine → confirmation → event` path before any ML or dataset work starts.

## Concurrency model

Continuous camera + inference is a throughput problem, and the defences are
layered cheapest-first:

- **Drop, don't queue.** CameraX analysis runs on a dedicated single-thread
  executor with `STRATEGY_KEEP_ONLY_LATEST`. Slow analysis drops frames instead
  of building a backlog that would blow latency and memory.
- **Throttle at the source.** Frames are rate-limited to `targetFps` (default 4)
  *before* they enter the pipeline. Running inference on every 30/60 fps frame is
  the fastest way to hit thermal throttling and drain the battery for no accuracy
  gain — hazards do not appear and vanish inside 250 ms. 2–5 fps is the working
  range; it is a tuning knob, not a fixed constant.
- **Bounded buffering.** The frame stream is a `SharedFlow` with a 1-slot buffer
  and drop-oldest overflow; the ViewModel collects on `Dispatchers.Default`.

The result: the system sheds load gracefully under pressure rather than falling
over, and the throttle is the primary lever for the battery/thermal budget.

### Why a foreground service is required (and absent from Milestone 1)

Real background monitoring — camera + inference while the app is not in front —
requires an Android **foreground service** with a persistent notification.
`WorkManager` is the wrong tool and worth calling out because it is a common
mis-reach: it is built for *deferrable, periodic* work with a **15-minute minimum
interval**, which is the opposite of continuous real-time monitoring. The
foreground service is intentionally left out of Milestone 1 (scoped to the
monitoring screen) and belongs with the alert engine, because a service that can
detect but not deliver an alert is half a feature.

## Deliberate non-decisions

These are documented so they are not silently "fixed" later by someone who
mistakes them for oversights.

- **Confidence is not calibrated.** A raw detector score is shown as a health
  signal, never surfaced as a "% chance." Calibration is a hard prerequisite
  before any auto-escalation. See [benchmarking.md](benchmarking.md#calibration).
- **No device-side SMS or dialing.** Google Play restricts SMS/Call-Log
  permissions to apps that are the device's default handler; a hazard-detection
  app does not qualify. Alert *delivery* is therefore a server-routed concern (a
  provider like a programmable SMS/voice API), separate from alert *detection*.
  This means a small backend is required from the first shipping alert, and the
  data-collection/retrain loop lives there too — so the backend is core IP, not a
  "phase 2" placeholder. See [roadmap.md](roadmap.md) and [risks.md](risks.md).
- **No auto-dial to emergency services.** Auto-dialling a public emergency number
  (112/110) off a false positive is both a legal liability and, on iOS, not even
  possible from an app. Alert targets are user-defined contacts, never a direct
  emergency-line dial.

## References

- Guardian repo: [`/ARCHITECTURE.md`](../../ARCHITECTURE.md),
  [`/README.md`](../../README.md) — as-built Milestone 1 description.
- Android Developers — foreground services and background execution limits
  (WorkManager minimum periodic interval).
- Android CameraX documentation — `ImageAnalysis` backpressure strategies
  (`STRATEGY_KEEP_ONLY_LATEST`).
- Google Play — SMS and Call Log permissions policy (default-handler restriction).
