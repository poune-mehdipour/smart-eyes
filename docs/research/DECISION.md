# DECISION — Recommended Stack & Justifications

The consolidated recommendation. Every major choice appears once, with the
reason it was made and a pointer to the deeper analysis. Choices already
implemented in Milestone 1 are marked **[locked]**; choices that still need an
explicit human call are in [Open decisions](#open-decisions).

## The stack at a glance

| Layer | Decision | Status |
|-------|----------|--------|
| Platform | Native Android (Kotlin), Android-only for MVP | [locked] |
| Architecture | Clean Architecture + MVVM, multi-module | [locked] |
| DI | Hilt (Dagger) | [locked] |
| UI | Jetpack Compose (Material 3) | [locked] |
| Camera | CameraX (`LifecycleCameraController`) behind a `FrameSource` seam | [locked] |
| Async | Kotlin Coroutines / Flow | [locked] |
| Frame format | NV21 at the seam; convert to RGB per-detector | [locked] |
| Detection engine | Pure-Kotlin, Android-free (`:core:model`, `:core:detection`) | [locked] |
| Confirmation | Multi-frame sliding-window debounce (`ConfirmationPolicy`) | [locked] |
| Persistence | Room (Milestone 5) | planned |
| Background | Foreground service (Milestone 4) — **not** WorkManager | planned |
| Detector model | RF-DETR (Apache-2.0) **or** licensed YOLO26 — see open decision | **open** |
| On-device runtime | LiteRT (formerly TFLite), ONNX interchange, CPU floor | recommended |
| Quantization | INT8 (PTQ, or QAT if needed) | recommended |
| Bootstrap data | D-Fire → FASDD_CV, plus a field-collected retrain flywheel | recommended |
| Alert delivery | Server-routed (provider SMS/voice/email) — **not** device-side | [locked] |
| MVP detectors | Fire + smoke + camera-tampering (fall/intrusion deferred) | [locked] |

## Justifications

### Native Android (Kotlin), not Flutter / React Native — [locked]
This workload is continuous camera + on-device inference + a background service +
foreground alerting. That is exactly the profile where a cross-platform layer adds
friction and loses control over camera, threading, and native ML runtimes. Native
Kotlin is the controllable, best-supported path. iOS is deferred to avoid paying
for two platforms before the core detection risk is retired. *(A prior
React-Native suggestion was explicitly reversed for this reason.)*

### Multi-module Clean Architecture + MVVM — [locked]
The product's commercial thesis ("phone today, CCTV tomorrow, same brain") only
survives if it is a type boundary rather than a slogan. The module split enforces
it: `:core:model` and `:core:detection` carry **no Android dependency**, so the
detection brain runs unchanged on a JVM server against RTSP later. See
[architecture.md](architecture.md).

### The `FrameSource` seam + NV21 frame — [locked]
`FrameSource` emits a normalised `AnalysisFrame` (NV21 + geometry + timestamp);
nothing downstream knows CameraX exists. CCTV support = add `RtspFrameSource`,
rebind one Hilt provider. NV21 is the cheapest common denominator across CameraX
and RTSP decoders; per-detector RGB conversion keeps the seam format-stable.

### One capability = one module (`Detector` + `@IntoSet`) — [locked]
Adding a detection scenario is a new module plus one Hilt binding; the engine,
pipeline, and UI never change. This is what makes the 50-scenario list an additive
exercise instead of a refactor — and why detector modules are added lazily as each
detector lands, not scaffolded upfront.

### Foreground service, not WorkManager — planned, but decided
Continuous real-time monitoring needs a foreground service. WorkManager is for
deferrable periodic work (15-min minimum) — using it here would be a reliability
bug. Lands with the alert engine (Milestone 4). See [architecture.md](architecture.md).

### Model — RF-DETR (Apache 2.0) as the licence-safe default; YOLO26 only if licensed — recommended, one open call
- **YOLO26** is the strongest *edge/CPU* model (NMS-free → deterministic per-frame
  latency, DFL removed → quantization-friendly, nano ~40.9 mAP at ~38.9 ms CPU,
  "up to ~43%" faster than YOLO11n). **But it is AGPL-3.0** — unusable in a
  closed-source product without a paid Ultralytics Enterprise licence.
- **RF-DETR** is **Apache 2.0** (core sizes), data-efficient (DINOv2 backbone,
  converges in fewer epochs on small custom data, strong on domain shift) — ideal
  for the small self-collected dataset this product depends on. Tradeoff: DETR
  transformers are heavier for pure on-device CPU than a YOLO-nano; its headline
  numbers are GPU-measured.
- **Recommendation:** default to **RF-DETR** for licence safety and data
  efficiency; adopt **YOLO26** only with a deliberate Enterprise-licence purchase.
  A viable split is RF-DETR server-side for the CCTV tier (GPU available) and a
  small quantized model on the phone. See [models.md](models.md).
- **Camera-tampering uses no model** — classical CV (frame-diff, blur/scene-shift).

### On-device runtime — LiteRT, ONNX interchange, guaranteed CPU path — recommended
LiteRT (renamed from TFLite in 2024; production NPU acceleration and ~1.4× faster
GPU than TFLite as of Jan 2026) is the default. Keep the model in ONNX so the
runtime is not a one-way door. NNAPI is deprecated — do not build on it; use
LiteRT GPU/NPU delegates behind capability checks, with CPU as the floor.

### Quantization — INT8 — recommended
Required to hit the latency/battery/thermal budget. Calibrate the model *after*
quantization, since it shifts the score distribution.

### Data — D-Fire → FASDD_CV + a field retrain flywheel — recommended
D-Fire (21.5k images, ~half deliberate negatives, surveillance-flavoured,
YOLO-format) is the pragmatic bootstrap; FASDD_CV scales generalization later. The
durable asset is the **server-side retrain flywheel** fed by field false alarms —
this is what "learns from real cases" means (not on-device online learning, which
is out of scope for a safety-critical detector). Verify dataset licences before
commercial training. See [datasets.md](datasets.md).

### Alert delivery is server-routed, not device-side — [locked]
Google Play restricts SMS/Call-Log permissions to default-handler apps; a
hazard-detection app does not qualify. Delivery goes through a backend + provider.
No auto-dial to emergency numbers (legal risk; impossible on iOS). This makes the
backend core IP from the first shipping alert, not a later add-on.

### MVP = fire + smoke + camera-tampering — [locked]
Fire/smoke is a mature object-detection target; tampering is near-free classical
CV. Fall (pose + temporal + edge cases) and zone-intrusion are a harder difficulty
class and are deferred to Milestone 6 so they don't sink the timeline of a
*reliable* MVP. Depth before breadth.

### Confidence is a health signal, not a probability — [locked]
Raw model scores aren't calibrated probabilities; showing "91%" is overclaiming.
Score stays internal until ECE/reliability is measured on the quantized model and
calibrated — a hard prerequisite before any auto-escalation. See
[models.md](models.md#calibration).

### No prediction — [locked]
Guardian detects what is happening now. Multi-frame confirmation is a debounce to
kill single-frame noise, not a forecast. Target confirmed-alert latency 1–3 s.

## Open decisions

These need an explicit human call and should not be defaulted silently:

1. **Model licence path.** Buy an Ultralytics Enterprise licence to use YOLO26, or
   commit to RF-DETR / a permissive architecture? *This gates Milestone 3 and is a
   business decision with legal weight.* Recommended default: RF-DETR.
2. **On-device vs server inference for the CCTV tier.** Phone tier is on-device;
   the CCTV tier could run the heavier/more-accurate model server-side. Decide
   before the RTSP work so the frame-source and deployment story line up.
3. **Legal review trigger.** Fire/smoke MVP is clean. Face / licence-plate /
   behavioural features cross into GDPR special-category + EU AI Act high-risk (or
   prohibited) territory — a face-learning-from-footage feature is the sharpest
   edge. A DPIA/FRIA + qualified legal review is a hard gate before any such
   feature. See [risks.md](risks.md).
4. **Target-device floor.** Which minimum phone spec must sustain continuous
   monitoring? This bounds the fps/resolution/model-size budget and needs a
   number, set against the Phase-0 spike results.

## Verification note

Model versions, runtime capabilities, and EU AI Act enforcement dates were checked
against primary sources in **July 2026** and are moving targets. Re-verify the
licence terms, model benchmarks, and regulatory dates before they are relied on in
a commit. Per-document references are in each research file; the highest-stakes
items to re-check are the Ultralytics licence terms, the RF-DETR licence tiers,
and the AI Act biometric high-risk timeline.
