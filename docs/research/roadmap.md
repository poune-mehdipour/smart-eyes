# Roadmap

The sequencing principle: **retire the biggest risk earliest, on the cheapest
possible target.** The biggest risk is not "can we build an Android app" — it is
"can a model hit an acceptable false-positive rate on real footage" and "is the
licence/backend story sound." The order below is built around attacking those
first.

## Phase 0 — Feasibility spike (before committing to the plan)

A 2–3 day experiment, not a document (see [benchmarking.md](benchmarking.md)):
run an existing fire/smoke model over real target-environment footage and measure
the false-positive rate. Output: a go / adjust decision and a realistic sense of
how much data work the product actually needs. This is the highest-information,
lowest-cost step available, and it precedes everything.

## Milestone 1 — Monitoring shell ✅ (delivered)

Multi-module project, Gradle setup, CameraX integration, working monitoring
screen. The full `FrameSource → DetectionEngine → EventConfirmationEngine`
pipeline runs end-to-end with an **empty** detector set, so the seams are real and
exercised, not stubbed. No detectors yet. This is the platform every later
milestone plugs into.

## Milestone 2 — Camera-tampering detector (classical CV, no model)

First real detector, chosen because it is the cheapest possible proof of the
extension path. Cover / blur / sudden scene-shift via frame differencing and
blur/scene-change metrics — **no model, no dataset, no model licence question.**
It proves the `Detector → @IntoSet → engine → confirmation → event` path on a
target that cannot be blocked by the data problem. If this milestone is clean, the
architecture's core claim is validated.

## Milestone 3 — Fire + smoke detectors (the real work)

The heart of the product, and where the schedule risk actually lives — the code is
small, the data and false-positive tuning are not (see
[datasets.md](datasets.md), [benchmarking.md](benchmarking.md)).

- Pick the model with the licence decision already made (see
  [models.md](models.md) and [DECISION.md](DECISION.md)).
- Bootstrap on D-Fire (→ FASDD_CV), export/quantize to LiteRT, integrate as a
  `:detector:fire` (and `:detector:smoke`) module.
- Tune `ConfirmationPolicy` against real footage to hit the 1–3 s confirmed-alert
  latency at an acceptable FP/hour.
- Establish the on-device performance profile per target device.

## Milestone 4 — Alert engine

On a *confirmed* event: local actions (alarm sound, full-screen alert, flashlight,
vibrate, capture photo + short clip, persist locally). This is also where the
**foreground service** lands — continuous background monitoring needs it, and an
alert engine without a service is half a feature (see
[architecture.md](architecture.md)). Alert *delivery* off-device (SMS / call /
email via a provider) is explicitly a **server-routed** concern that starts here
and is built out in Milestone 7 — device-side SMS is blocked by Google Play
policy. Alert targets are user-defined contacts, never a direct emergency-line
auto-dial.

## Milestone 5 — Event history & configuration

Room-backed event history (with the captured media), per-detector settings, and
emergency-contact management. Turns a detector into a usable product.

## Milestone 6 — Fall + intrusion detectors (deferred difficulty)

Deferred **on purpose** — these are a different, harder difficulty class than
fire/smoke and would drag a reliable MVP out by months if bundled early:

- **Fall detection** needs pose estimation + temporal logic + a long tail of
  edge cases (sitting fast, lying down, bending). Candidate: a pose model (e.g.
  the YOLO26 pose head, licence permitting) plus a temporal state machine — not a
  single-frame object detector.
- **Zone intrusion** needs person detection + user-defined polygon zones +
  entry/exit logic. More tractable than fall, still more than a detector drop-in.

## Milestone 7 — Alert-delivery backend

The server that routes SMS / voice / email via a programmable provider, **and**
hosts the false-alarm capture + retrain flywheel from [datasets.md](datasets.md).
This is core IP, not an afterthought: it is where the "learns from real cases"
promise actually lives (as server-side retraining, not on-device online learning).
The backend footprint effectively begins at Milestone 4's first off-device alert;
Milestone 7 is where it is built out properly.

## Beyond MVP — the enterprise / CCTV tier (and the legal cliff)

The commercial thesis: reuse the same detection brain against RTSP/CCTV streams
by adding an `RtspFrameSource` and rebinding one provider — the engine and
pipeline are untouched because they are already Android-free (see
[architecture.md](architecture.md)).

**Hard boundary:** the fire/smoke MVP processes no personal data and is legally
clean. The moment the roadmap's later scenarios arrive — theft/loitering
(behavioural analytics), and especially apartment access via **face / licence-plate
/ vehicle recognition** — the product crosses into GDPR special-category data and
EU AI Act high-risk (or, for some uses, prohibited) territory. That is not a
distant concern to bolt on later; it must be assumed in the architecture now. See
[risks.md](risks.md).

## Deferred / out of scope (stated so nobody "adds it back" by accident)

- **iOS** — deferred; Android-only for the MVP.
- **Prediction / forecasting** — explicitly not built. Guardian detects what is
  happening now; multi-frame confirmation is a debounce, not a forecast.
- **On-device online learning** — out of scope. "Self-learning" = server-side
  retrain flywheel. Unsupervised on-device weight updates on a safety-critical
  detector are a way to silently break the model in the field.
- **Auto-dialling emergency services (112/110)** — never. Legal liability, and
  impossible from an app on iOS regardless.

## References

- Guardian repo — `/README.md` roadmap; `/ARCHITECTURE.md` deliberate
  non-decisions.
- Cross-refs: [models.md](models.md), [datasets.md](datasets.md),
  [benchmarking.md](benchmarking.md), [risks.md](risks.md).
