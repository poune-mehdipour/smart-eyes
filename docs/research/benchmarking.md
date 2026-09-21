# Benchmarking

What "good" means, and how it is measured. The framing here is deliberate: the
number that decides whether this product is viable is **not** mAP on a public
benchmark. It is the false-positive rate on real footage from your target
environments. Optimize the metric that matches the failure mode.

## The spike that comes before everything

Before writing detector code, architecture documents, or milestone plans, run a
**2–3 day feasibility spike** that answers one question:

> On real phone-camera footage from the actual target environments, can an
> existing fire/smoke model hit an acceptable false-positive rate?

Procedure:

1. Take an existing pre-trained fire/smoke model (a D-Fire-trained YOLO/RF-DETR
   checkpoint, or a public one).
2. Run it over real video from *your* target scenes — kitchens, hallways, shops
   at different times of day, including the adversarial cases: sunset, red/orange
   objects, reflections, steam, brake lights, screens/TVs.
3. Measure the false-positive rate (and note misses).

This single experiment tells you more about whether the product is buildable than
any design document. If a decent model drowns your target scenes in false alarms,
that is the real project — data and FP tuning — surfaced on day 3 instead of month
3. Everything downstream is scheduled around the answer to this.

## Detection quality metrics (offline, on held-out data)

Standard object-detection metrics, reported on a held-out split that includes hard
negatives:

- **mAP@0.5** and **mAP@0.5:0.95** — overall detection quality. Useful for
  comparing models, but do not mistake a good mAP for a shippable product.
- **Precision / Recall, and the PR curve** — precision is the one that maps to
  "how often an alert is real." For a safety alerter, the operating point is
  chosen to keep precision high enough that users trust alerts, accepting some
  recall loss (a missed early wisp that gets caught a few frames later is fine;
  the confirmation debounce assumes exactly this).
- **False positives per hour of benign footage** — the product metric. Report it
  per environment class (kitchen, hallway, retail, outdoor) because FP rate is
  domain-dependent. A model can post a fine mAP and still be unusable in one
  specific lighting condition.
- **Per-confuser breakdown** — FP rate specifically on the adversarial categories
  (sunset, red objects, steam, reflections, screens). This is where regressions
  hide and where the retrain flywheel is targeted.

## On-device performance budget

The model has to be fast *and* sustainable on a phone. Measure on **real target
devices**, spanning a low-end and a mid/high-end phone — not on a datacentre GPU,
and not only on the newest flagship.

- **Per-frame latency (ms)** at the chosen input resolution, per delegate
  (CPU / GPU / NPU). Include the **NV21→RGB conversion** cost — it is part of the
  real per-frame budget, not an externality (see
  [architecture.md](architecture.md)). With YOLO26's NMS-free head, per-frame
  latency should be roughly constant regardless of object count; verify that this
  holds on-device.
- **Sustained throughput at target fps.** The system runs at 2–5 fps by design;
  the question is whether the device holds that rate *continuously* without the
  frame-drop rate climbing.
- **Thermal behaviour.** Run 20–30 minutes continuously and watch for thermal
  throttling — latency creeping up as the SoC heats and downclocks. A model that
  is fast for 60 seconds and throttles after 10 minutes fails the real workload.
  The source-side fps throttle is the primary mitigation.
- **Battery drain (%/hour)** during continuous monitoring, foreground service
  active. This is a headline number for a product people leave running, and a
  direct input to the fps and resolution choices.
- **Cold-start / model-load time and memory footprint (peak RSS).** Affects
  perceived responsiveness and low-RAM-device viability. LiteRT's AOT compilation
  path helps model-load time where the target SoC is known.

Tooling: LiteRT / TFLite ships a model benchmark tool for Android that measures
per-delegate latency on device; use it to build a per-device profile before
enabling any delegate in production, and keep a denylist of device/model
combinations where a delegate underperforms the CPU path.

## Calibration measurement

Calibration is measured, not assumed (see [models.md](models.md#calibration)).
On held-out target-domain footage:

- **Reliability diagram** + **Expected Calibration Error (ECE)** — does a score of
  0.9 actually correspond to ~90% correctness? For a raw or freshly-quantized
  model, usually not.
- Measure **after quantization**, on the shipped model. Quantization shifts the
  score distribution, so a calibration fitted on the float model is invalid.
- Only once ECE is acceptable (via temperature/Platt/isotonic scaling) may a
  probability be surfaced or an auto-escalation be wired. Until then, the score
  stays an internal gating signal.

## Tuning the confirmation pipeline

`ConfirmationPolicy` (per-type `minConfidence`, `requiredHits`, `windowMs`,
`cooldownMs`) is a set of **placeholders for tuning**, not values to ship. Tune
them against real footage as an explicit optimization:

- Sweep `requiredHits` / `windowMs` and plot the resulting **FP-per-hour vs
  detection-latency** trade-off. More required hits ⇒ fewer false alarms but
  slower confirmation. The target from the brief is a 1–3 second confirmed-alert
  latency; find the policy that sits inside that while minimising FP/hour.
- `cooldownMs` is tuned to avoid alert storms on a genuine sustained event without
  masking a second distinct event.
- Validate the tuned policy on a *different* footage set than the one it was tuned
  on, or the thresholds are overfit.

## What gets reported per model candidate

A candidate model is not "evaluated" until this table exists for it, on target
devices and target-domain footage:

| Axis | Metric |
|------|--------|
| Quality | mAP@0.5, mAP@0.5:0.95, precision/recall at chosen operating point |
| Product | FP/hour on benign footage, per environment + per confuser |
| Latency | per-frame ms per delegate (incl. NV21→RGB), constant-latency check |
| Sustainability | drop rate at target fps over 30 min, thermal curve, %battery/hr |
| Footprint | model size, peak memory, load time |
| Calibration | ECE / reliability, post-quantization |
| Licence | model licence + any dataset-licence constraints on the weights |

## References

- Standard detection metrics — COCO mAP definition; precision/recall & PR curves.
- Calibration — reliability diagrams and Expected Calibration Error; temperature
  scaling (Guo et al.), Platt scaling, isotonic regression.
- LiteRT / TFLite — Android model benchmark tool; per-device delegate measurement
  guidance; AOT vs on-device compilation (Google Developers Blog, 28 Jan 2026).
- Guardian repo — `ConfirmationPolicy.defaults()` in `:core:detection` (the
  thresholds under tuning).
