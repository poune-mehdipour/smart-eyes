# Engineering Decision Matrix

**Status:** Design review, pre-implementation. **Audience:** CTO, Principal AI
Engineer, CV Research Lead, technical investors. **Purpose:** make every major
technical decision defensible *before* production code is written.

This is the single source of truth for *why* each technology was chosen. It sits
on top of the deeper analysis already written — it does not replace it. Where a
claim needs backing, it points to [`models.md`](models.md),
[`datasets.md`](datasets.md), [`benchmarking.md`](benchmarking.md),
[`risks.md`](risks.md), [`roadmap.md`](roadmap.md), [`DECISION.md`](DECISION.md).

> **Read Section 8 first if you only read one section.** Every on-device
> performance number in this document is an **engineering estimate**, not a
> measurement. Nothing has been benchmarked on target hardware yet — the
> Milestone-1 code is not even build-verified. The estimates are grounded in
> vendor reference benchmarks and comparable deployments, and are labelled as
> estimates wherever they appear. Do not treat them as results.

**Scoring convention for 1–10 columns.** Two directions, stated per table:
- *Cost/risk/difficulty* columns (implementation, research, FP risk, maintenance,
  engineering risk): **higher = worse.**
- *Quality/fit* columns (dataset quality, CCTV compatibility, scalability,
  business value, user value): **higher = better.**

---

## Section 1 — Executive Summary

Guardian is an on-device, real-time hazard-detection system for Android. The
phone camera is the first sensor; the detection engine is deliberately
source-agnostic so the same brain runs against RTSP/CCTV later. The MVP is
**fire + smoke + camera-tampering**. Fall and intrusion are a harder difficulty
class and are deferred on purpose.

### Final recommended stack

| Concern | Recommendation | Confidence |
|---|---|---|
| App platform | Native Android / Kotlin (no cross-platform layer) | High |
| Architecture | Multi-module Clean Architecture + MVVM; Android-free engine | High |
| Fire / smoke model | **RF-DETR (Apache-2.0)** default; YOLO26n only with a paid Ultralytics licence | Medium |
| Camera tampering | Classical CV (OpenCV) — no model | High |
| Fall (deferred) | Pose (MediaPipe Pose / MoveNet / RTMPose, all Apache-2.0) + temporal state machine | Low |
| Intrusion (deferred) | Person detector (reuse fire-tier detector, COCO person class) + polygon zone logic | Medium |
| Inference runtime | **LiteRT** (formerly TFLite); ONNX as interchange; CPU floor, GPU/NPU opportunistic | Medium-High |
| Model format | ONNX (portable) → LiteRT (`.tflite`) for deployment | High |
| Training framework | PyTorch (RF-DETR/YOLO native) | High |
| Bootstrap data | D-Fire → FASDD_CV + server-side field retrain flywheel | Medium |
| Alert delivery | Server-routed (provider SMS/voice/email) — never device-side | High |

### Why these, in one line each
- **Native Kotlin:** continuous camera + on-device inference + foreground service is exactly where a cross-platform layer loses control; it was the correct reversal of an earlier React-Native suggestion.
- **RF-DETR over YOLO:** licence. All Ultralytics YOLO (v8/v10/v11/v12/26) is AGPL-3.0 — unusable in a closed-source product without a paid licence. RF-DETR core is Apache-2.0 and is data-efficient (DINOv2 backbone) on the small custom dataset this product depends on.
- **Classical CV for tampering:** no model, no dataset, no licence question, negligible compute — the cheapest possible proof of the detector→pipeline path.
- **LiteRT:** the current, actively-developed on-device runtime on Android, with a CPU floor and GPU/NPU acceleration; NNAPI is deprecated and avoided.

### Biggest technical risks (full treatment in [risks.md](risks.md))
1. **False-positive rate on real footage** — the product-killer. mAP on public data says nothing about FP/hour in *your* rooms at dusk.
2. **Model licence** — building on AGPL YOLO in a proprietary product is a legal exposure; must be decided before Milestone 3.
3. **The biometric legal cliff** — fire/smoke is clean; face/plate/behaviour on the CCTV roadmap crosses into GDPR special-category + EU AI Act high-risk (or prohibited).
4. **On-device sustainability** — thermal throttling and battery drain under continuous inference, especially on low-end devices.

### Highest-priority engineering work
1. **Phase-0 feasibility spike** (2–3 days): run an existing fire/smoke model over real target footage, measure FP rate. This decides viability before more is built.
2. **Milestone 2 — camera-tampering detector** (classical CV): proves the extension path on the cheapest target.
3. **Milestone 3 — fire/smoke**: model integration is small; the data + FP-tuning + on-device profiling is the real work.

### Expected MVP complexity
- **Fire + smoke:** moderate code, **high** data/ML effort (domain gap + FP tuning + retrain loop). Weeks of code, months of data.
- **Camera tampering:** low, self-contained.
- **Backend (alert delivery + retrain flywheel):** required from the first shipping alert; core IP, not a "later" item.

### Confidence by subsystem
| Subsystem | Confidence | Note |
|---|---|---|
| App architecture & seam | **High** | Sound, exercised end-to-end in M1 (not yet build-verified). |
| Camera-tampering detector | **High** | Well-understood classical CV. |
| Inference runtime (LiteRT) | **Medium-High** | Right choice; on-device numbers unproven. |
| Fire/smoke detection quality | **Medium** | Hinges on data + FP tuning + the Phase-0 result. |
| On-device perf (FPS/thermal/battery) | **Low** | Entirely estimated; unmeasured. |
| Fall detection | **Low** | Hard problem, poor datasets — correctly deferred. |
| Legal posture (biometric roadmap) | **Low** | Needs qualified legal review before any biometric feature. |

---

## Section 2 — Detector Decision Matrix

Grouped into four sub-tables for readability (a single 20-column table is
unreadable and would render badly). Every detector appears in each sub-table, so
rows cross-reference directly. **All performance figures are estimates — see
Section 8.**

### 2a — Approach, model, data, licensing

| Detector | Recommended approach | Primary model | Alternatives | Dataset maturity | Commercial licensing risk |
|---|---|---|---|---|---|
| **Fire** | On-device object detection | RF-DETR-N (Apache-2.0) | YOLO26n *(AGPL — needs licence)*, YOLOX, RT-DETR (Apache) | Good — D-Fire + FASDD_CV | **Low** with RF-DETR; High if YOLO chosen unlicensed |
| **Smoke** | On-device object detection (same model, multi-class) | RF-DETR-N (Apache-2.0) | YOLO26n *(AGPL)*, YOLOX | Good — same datasets, smoke well-represented | **Low** (shares fire model) |
| **Camera tampering** | Classical CV (frame-diff, blur/variance, scene-change) | None (OpenCV) | Optical-flow variance; lightweight autoencoder (overkill) | N/A — no dataset needed | **None** |
| **Fall** *(deferred)* | Pose estimation + temporal state machine | MediaPipe Pose / MoveNet (Apache-2.0) | RTMPose (Apache), YOLO26-pose *(AGPL)* | **Poor** — small, lab-staged fall datasets | Low (Apache pose models) |
| **Intrusion** *(deferred)* | Person detection + polygon zone geometry | Reuse fire-tier detector, COCO person class | Dedicated person detector; MediaPipe object detection | Good — person detection is mature (COCO) | Low (shares detector) |

### 2b — Performance estimates *(mid-range phone, INT8, GPU delegate where available)*

> **ESTIMATES. Unmeasured.** "Model-capable FPS" is what the model could sustain;
> the system **operates at 2–5 fps by design** (see [architecture.md](architecture.md)).

| Detector | Model-capable FPS (est.) | Latency/frame (est., incl. NV21→RGB) | RAM (est.) | Battery impact (est.) | Model size (est., INT8) |
|---|---|---|---|---|---|
| **Fire** | 8–25 | 60–140 ms (RF-DETR-N) / 40–100 ms (YOLO26n) | 150–350 MB | High (continuous) | RF-DETR-N ~25–50 MB / YOLO26n ~6–10 MB |
| **Smoke** | 8–25 (shares fire model) | as fire | shared with fire | shared | shared with fire |
| **Camera tampering** | ≫ operating fps | < 15 ms | < 50 MB | Low | 0 (no model) |
| **Fall** | 15–30 (pose) | 15–40 ms (MoveNet-class) + temporal logic | 100–250 MB | Moderate–High | MoveNet ~3–7 MB / MediaPipe ~13–16 MB bundle |
| **Intrusion** | 8–25 | 40–120 ms | 150–350 MB | High | shares detector / ~10–50 MB |

### 2c — Engineering risk scores (1–10)

*Cost/risk columns: higher = worse. Quality/fit columns: higher = better.*

| Detector | Impl. complexity ↑bad | AI-research complexity ↑bad | False-positive risk ↑bad | Dataset quality ↑good | Maintenance difficulty ↑bad | CCTV compatibility ↑good | Long-term scalability ↑good | Overall engineering risk ↑bad |
|---|:--:|:--:|:--:|:--:|:--:|:--:|:--:|:--:|
| **Fire** | 5 | 6 | 8 | 7 | 4 | 9 | 8 | 6 |
| **Smoke** | 5 | 7 | 9 | 7 | 4 | 9 | 8 | 7 |
| **Camera tampering** | 2 | 1 | 4 | N/A | 2 | 9 | 8 | 2 |
| **Fall** | 7 | 9 | 8 | 3 | 6 | 7 | 6 | 9 |
| **Intrusion** | 5 | 4 | 5 | 8 | 4 | 9 | 8 | 5 |

### 2d — Priority & recommendation

| Detector | Development priority | Engineering recommendation |
|---|---|---|
| **Camera tampering** | **P0** | Build first. Cheapest proof of the detector→pipeline path; no model/data/licence risk. |
| **Fire** | **P1** | Core value. Model integration is small; budget the effort for data + FP tuning + on-device profiling, not code. |
| **Smoke** | **P1** | Ship with fire as a multi-class head. Expect it to be the harder of the two on false positives (steam/fog/haze). |
| **Intrusion** | **P2** | Tractable after fire/smoke: mature person detection + bespoke zone geometry. The CCTV-tier headline feature. |
| **Fall** | **P3** | Defer. Hardest problem, weakest datasets. Approach as pose + temporal logic, never a single-frame detector. |

---

## Section 3 — Technology Decision Matrix

Every candidate the review evaluated. Licence and maintenance are current-state
facts checked mid-2026 (see references) — the columns most likely to go stale.
"Mobile" and "offline" suitability are for *on-device Android* specifically.

### Object detectors (fire / smoke / intrusion)

| Model | Strengths | Weaknesses | Mobile | Offline | Inference speed | Memory | Model size | License | Maintenance | Community | Future roadmap | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **RF-DETR** (Roboflow) | DINOv2 backbone → data-efficient on small custom data; strong on domain shift; NMS-free | DETR transformer heavier than YOLO-nano on CPU; smaller mobile track record | Medium | Yes | High (GPU); moderate (mobile) | Medium-High | Medium | **Apache-2.0** (core N–L) | Active | Growing | Active (ICLR 2026) | **ACCEPTED — default** |
| **YOLO26** (Ultralytics) | Edge-first; NMS-free, DFL removed → quantization-friendly; fastest CPU nano; multi-task incl. pose | **AGPL-3.0**; commercial needs paid licence | High | Yes | Very high (nano) | Low | Very small | **AGPL-3.0** | Very active | Very large | Active | **CONDITIONAL — only if licensed** |
| **YOLO11** (Ultralytics) | Mature, fast, huge ecosystem | Superseded by YOLO26 on edge metrics; **AGPL-3.0** | High | Yes | High | Low | Small | **AGPL-3.0** | Very active | Very large | Maintenance | **REJECTED — licence + superseded** |
| **YOLO10 / YOLOv10** (THU-MIG) | NMS-free (dual assignment); low latency | Built on Ultralytics → **AGPL-3.0**; superseded | High | Yes | High | Low | Small | **AGPL-3.0** | Moderate | Large | Low | **REJECTED — licence** |
| **YOLOv8** (Ultralytics) | Very mature, ubiquitous tooling | **AGPL-3.0**; older generation | High | Yes | High | Low | Small | **AGPL-3.0** | Very active | Very large | Maintenance | **REJECTED — licence + older** |
| **RT-DETR** (Baidu) | First real-time DETR; NMS-free; **Apache-2.0** (original) | Superseded by RF-DETR (its successor); Ultralytics port is AGPL — use the Paddle/Apache original | Medium | Yes | High (GPU) | Medium-High | Medium | **Apache-2.0** (original) | Moderate | Moderate | Low (superseded) | **REJECTED — RF-DETR is the better same-lineage choice** |
| **EfficientDet** (Google) | Apache-2.0; scalable compound-scaled family | 2019–2020 era; slower on mobile than modern nanos; effectively legacy | Medium (Lite) | Yes | Low-Medium | Medium | Medium | **Apache-2.0** | Low | Moderate (legacy) | None | **REJECTED — superseded/slow** |
| **NanoDet / NanoDet-Plus** | Tiny (~1 MB int8), ~97 fps on phone; Apache-2.0; anchor-free | **Effectively unmaintained since ~2021**; author moved on; open issues unanswered | Very high | Yes | Very high | Very low | Tiny | **Apache-2.0** | **Stale** | Small | **None** | **REJECTED — dead upstream** (fallback only) |

*Permissively-licensed alternatives held in reserve (not primary):* **YOLOX**
(Apache-2.0, Megvii) and **RTMDet** (Apache-2.0, OpenMMLab) are viable Apache
object detectors if RF-DETR proves too heavy on-device — keep them as the
licence-clean CNN fallback.

### Pose models (fall detection — deferred)

| Model | Strengths | Weaknesses | Mobile | Offline | Speed | Memory | Size | License | Maintenance | Community | Roadmap | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **MediaPipe Pose** (Google, BlazePose GHUM) | Best Android integration; 33 3D landmarks; on-device real-time; LiteRT-native | Single-person focus; landmark-only (no multi-person scene) | High | Yes | High | Low-Medium | ~13–16 MB | **Apache-2.0** | Active | Large | Active | **ACCEPTED — mobile default** |
| **MoveNet** (Google) | Ultra-light (Lightning <7 ms mobile ref); 17 keypoints; runs without GPU | Single-person; 2D only | Very high | Yes | Very high | Low | ~3–7 MB | **Apache-2.0** | Moderate | Large | Low | **ACCEPTED — ultra-light option** |
| **RTMPose** (OpenMMLab) | Strong accuracy; real-time on CPU edge; whole-body (RTMW) variant | Heavier integration; better fit server/CCTV than phone | Medium | Yes | High (CPU edge) | Medium | Medium | **Apache-2.0** | Active | Large | Active | **ACCEPTED — CCTV/server tier** |
| **YOLO-Pose** (Ultralytics) | Unified with YOLO detection stack; multi-person | **AGPL-3.0** | High | Yes | High | Low | Small | **AGPL-3.0** | Very active | Very large | Active | **REJECTED — licence** |

### Runtime / classical

| Technology | Strengths | Weaknesses | Mobile | Offline | Speed | Memory | Size | License | Maintenance | Community | Roadmap | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **MediaPipe** (as a full solutions runtime) | Turnkey vision tasks; on-device; Google-maintained | Opinionated; less control than raw LiteRT for custom models | High | Yes | High | Low-Medium | Small | **Apache-2.0** | Active | Large | Active | **ACCEPTED — for pose task; raw LiteRT for custom detectors** |
| **OpenCV classical CV** | No model/data/licence; deterministic; tiny; perfect for tampering | Not suitable for semantic detection (fire/smoke/pose) | High | Yes | Very high | Very low | Tiny | **Apache-2.0** | Very active | Very large | Active | **ACCEPTED — camera tampering** |

**Runtime decision (not a detector, but the load-bearing platform choice):**
**LiteRT** (formerly TensorFlow Lite; renamed 2024) is the recommended inference
engine — actively developed, multi-framework import (PyTorch/TF/JAX), production
NPU acceleration, GPU/CPU delegates, CPU as guaranteed floor. **NNAPI is
deprecated** and avoided. **ONNX Runtime Mobile** and **NCNN** (Tencent) are
noted as fallbacks if a specific model resists LiteRT conversion. See
[models.md](models.md).

---

## Section 4 — Risk Assessment

*Probability / Impact: Low / Medium / High.*

| Decision | Risk | Probability | Impact | Mitigation | Fallback |
|---|---|---|---|---|---|
| Fire/smoke on-device detection | Unacceptable false-positive rate in target scenes | High | **Critical** | Phase-0 spike measures it first; FP/hour per environment tracked; multi-frame confirmation debounce; retrain flywheel on confusers | Human-in-the-loop confirmation before any alert escalates; raise `requiredHits` |
| Choose RF-DETR (Apache) | RF-DETR too heavy for low-end phones | Medium | High | Ship smallest size; INT8 quantize; GPU/NPU delegate; profile early | Swap to YOLOX/RTMDet (Apache CNN) or licensed YOLO26n |
| Defer YOLO26 unless licensed | Team reaches for AGPL YOLO under deadline pressure | Medium | **Critical (legal)** | Licence decision locked before M3; RF-DETR is the default in code | Purchase Ultralytics Enterprise licence deliberately |
| On-device continuous inference | Thermal throttling / battery drain; killed by OEM battery manager | High | High | 2–5 fps throttle; INT8; drop-don't-queue; foreground service; battery-exemption guidance; 30-min thermal test | Lower fps/resolution; smaller model; duty-cycle the detector |
| LiteRT + delegates | Delegate misbehaves on specific devices; NNAPI removed | Medium | Medium | CPU floor always works; delegates behind capability checks; per-device denylist | CPU-only path; NCNN for problem models |
| Fire/smoke public data | Domain gap: trained on internet fire ≠ target rooms | High | High | Field retrain flywheel from day one; weight hard negatives | Aggressive on-site fine-tuning per deployment |
| Fall detection (deferred) | Underestimated as "week 3"; sinks timeline if pulled early | Medium | High | Deferred to P3; scoped as pose + temporal state machine | Ship without fall; partner/third-party for fall if needed |
| CCTV/biometric roadmap | GDPR special-category + EU AI Act high-risk / **prohibited** (face-from-footage) | High (if built naively) | **Critical (legal)** | Assume in architecture now; DPIA/FRIA + legal review gate; prefer verification over identification | Drop/limit biometric features to stay outside high-risk |
| Alert delivery | Device-side SMS blocked by Google Play | High (certain) | High | Server-routed delivery via provider from the first alert | N/A — this is a hard platform constraint, designed around |
| Confidence display | Overclaiming from uncalibrated scores erodes trust | Medium | Medium | Score is internal until calibrated (ECE on quantized model); no number shown in MVP | Keep confidence internal indefinitely |
| Dataset licences | NC-licensed data taints commercial weights | Medium | High | Verify each dataset licence before commercial training | Exclude NC data; retrain on clean sources |

---

## Section 5 — Engineering Priority Matrix

*Value/availability columns: higher = better. Cost/difficulty columns: higher =
worse. "Final priority" is the synthesis, not an average.*

| Detector | Business value ↑good | Technical difficulty ↑bad | Dataset availability ↑good | Implementation cost ↑bad | Research cost ↑bad | Expected user value ↑good | Maintenance cost ↑bad | Final priority |
|---|:--:|:--:|:--:|:--:|:--:|:--:|:--:|:--:|
| **Camera tampering** | 6 | 2 | N/A | 2 | 1 | 6 | 2 | **P0** |
| **Fire** | 10 | 6 | 7 | 5 | 7 | 10 | 5 | **P1** |
| **Smoke** | 9 | 7 | 7 | 5 | 8 | 9 | 5 | **P1** |
| **Intrusion** | 8 | 5 | 8 | 6 | 4 | 7 | 4 | **P2** |
| **Fall** | 8 | 9 | 3 | 8 | 9 | 8 | 6 | **P3** |

**Reading the ranking:** tampering is P0 *despite* modest business value because
it is near-free and de-risks the architecture. Fire/smoke are P1 on
value-times-feasibility. Fall carries high user value but its difficulty and
dataset scarcity push it to P3 — high value does not override the fact that it is
the least de-riskable detector today.

---

## Section 6 — Technology Stack Lock

Final locked stack. Items marked *(open)* require an explicit human decision
(see [DECISION.md](DECISION.md) open decisions) and are locked to a **default**
pending that call.

**Fire Detection**
- Selected: **RF-DETR-N (Apache-2.0)** *(open — vs licensed YOLO26n)*
- Reason: object detection on a mature dataset (D-Fire/FASDD); RF-DETR is
  licence-clean and data-efficient on small custom data. YOLO26n is faster/smaller
  but AGPL — permitted only with a paid Ultralytics licence.

**Smoke Detection**
- Selected: **RF-DETR-N, multi-class head shared with fire**
- Reason: same modality, same datasets, same runtime; no reason to run a second
  model. Budget extra FP-tuning effort — smoke is the harder class.

**Fall Detection** *(deferred, P3)*
- Selected: **MediaPipe Pose / MoveNet (Apache-2.0) + temporal state machine**
- Reason: keypoints (33 or 17) are sufficient for fall logic; both are Apache and
  mobile-proven. Fall is temporal, not single-frame — the state machine is the
  hard part, not the pose model. RTMPose for the server/CCTV tier.

**Intrusion** *(deferred, P2)*
- Selected: **Reuse fire-tier detector (COCO person class) + polygon zone geometry**
- Reason: person detection is mature; the work is user-defined zones and
  entry/exit logic (engineering, not research). No new model needed.

**Camera Tampering**
- Selected: **Classical CV (OpenCV) — frame-diff, blur/variance, scene-change**
- Reason: no model, no dataset, no licence, negligible compute; deterministic and
  explainable. Correct first detector.

**Android Runtime**
- Selected: **LiteRT** (formerly TensorFlow Lite)
- Reason: actively developed on-device runtime; multi-framework import; GPU/NPU
  delegates with a CPU floor. **NNAPI rejected** (deprecated).

**Inference Engine**
- Selected: **LiteRT interpreter**, CPU guaranteed + GPU/NPU behind capability
  checks; **NCNN** as per-model fallback.
- Reason: portability and a working CPU path beat peak throughput on one delegate.

**Training Framework**
- Selected: **PyTorch**
- Reason: RF-DETR and YOLO are PyTorch-native; the whole training/fine-tune/export
  toolchain is PyTorch-first.

**Model Format**
- Selected: **ONNX (interchange) → LiteRT `.tflite` (deployment)**
- Reason: ONNX keeps the runtime from being a one-way door; `.tflite` is the
  on-device artifact. INT8 quantized for the shipped model.

---

## Section 7 — Things We Explicitly Reject

| Rejected | Why |
|---|---|
| **YOLOv8 / YOLO10 / YOLO11 / YOLO-Pose** (Ultralytics/THU-MIG) | **AGPL-3.0.** Unusable in a closed-source, distributed/SaaS product without a paid licence, and all superseded by YOLO26 on the metrics that matter here. |
| **YOLO26 as an unconditional default** | Best edge model technically, but **AGPL-3.0**. Allowed only as a *deliberate, licensed* choice — not the default. |
| **RT-DETR** (as primary) | Original is Apache-2.0, but it is the predecessor RF-DETR was built to improve on; and the convenient Ultralytics port is AGPL. Choose RF-DETR instead — same lineage, better, permissive. |
| **EfficientDet** | 2019–2020 generation; slower on mobile than modern nano detectors; effectively legacy. Apache licence does not offset being outclassed. |
| **NanoDet / NanoDet-Plus** | Tiny and Apache-licensed, but **effectively unmaintained since ~2021** — no living upstream to track for fixes, compat, or model improvements. Kept only as a last-resort fallback. |
| **On-device online / self-learning weight updates** | Unsupervised on-device weight updates on a *safety-critical* detector silently corrupt the model in the field. "Self-learning" = **server-side retrain flywheel**, not on-device learning. |
| **WorkManager for monitoring** | Built for deferrable periodic work (15-minute minimum) — the opposite of continuous real-time monitoring. Wrong tool; use a **foreground service**. |
| **Device-side SMS / dialing** | Google Play restricts SMS/Call-Log permissions to default-handler apps; a hazard app does not qualify. Delivery is **server-routed**. |
| **Auto-dial to emergency services (112/110)** | Legal liability on a false positive; impossible from an app on iOS. Alert targets are user-defined contacts only. |
| **Flutter / React Native** | This workload (continuous camera + on-device inference + foreground service) is where cross-platform layers lose control of camera, threading, and native ML runtimes. |
| **Cloud-only / server-only inference for the phone tier** | Breaks the offline, low-latency, privacy-preserving premise. Inference is on-device; the server is for delivery + retraining, not the hot path. (The CCTV tier may run heavier models server-side — a separate, deliberate choice.) |

---

## Section 8 — Unknowns

The honest ledger. **Separate measured facts from estimates.** The only things
this document treats as facts are external and verifiable; everything about how
Guardian behaves on a real phone is an estimate until benchmarked.

### Verified external facts (checked mid-2026)
- **Model licences:** all Ultralytics YOLO (v5/v8/v10/v11/v12/26) are AGPL-3.0;
  RF-DETR core (N–L) is Apache-2.0 (XL/2XL are PML-1.0); RT-DETR original is
  Apache-2.0; MoveNet / MediaPipe Pose / RTMPose / EfficientDet / NanoDet / OpenCV
  are Apache-2.0; YOLO-Pose is AGPL-3.0.
- **Dataset composition:** D-Fire = 21,527 images / 26,557 boxes, ~9,838
  negatives, YOLO format; FASDD ≈ 100k+ images with a camera sub-set (FASDD_CV).
- **NanoDet maintenance:** upstream effectively inactive since ~2021.
- **NNAPI:** deprecated; Android tooling steers to LiteRT delegates.

### Vendor-measured (reference hardware — NOT our target devices)
These are the model authors' numbers on datacentre/reference hardware; treat as
*indicative*, not as our results:
- YOLO26n ~40.9 mAP; ~38.9 ms CPU vs YOLO11n 56.1 ms (Ultralytics reference).
- RF-DETR-L 56.5 AP @ 6.8 ms (NVIDIA T4, TensorRT FP16); RT-DETR-L 53.1 AP @ 108
  FPS (T4); YOLOv10-B 52.7 AP.
- MoveNet Lightning <7 ms on "mobile devices" (Google reference).

### Our engineering estimates (UNMEASURED — must be benchmarked)
Everything in Section 2b, plus:
- **Expected Android FPS** on target phones — estimated, not measured.
- **Per-frame latency on-device** (incl. NV21→RGB conversion) — estimated.
- **RAM / peak memory** under continuous monitoring — estimated.
- **Battery consumption** (%/hour) — estimated; a headline product number that
  *must* be benchmarked.
- **Thermal behaviour** over sustained runtime — estimated; needs a ≥30-min run.
- **Quantized model accuracy delta** vs float — assumed acceptable, unverified.
- **RF-DETR-N on-device viability** on low-end phones — the single biggest
  open technical question behind the model choice.

### Not yet validated (experiments that gate the product)
- **False-positive rate on real target footage** — *the* viability question;
  answered by the Phase-0 spike, not by any public benchmark.
- **Confidence calibration** (ECE / reliability) on the quantized model — required
  before any number is shown or any auto-escalation is wired.
- **`ConfirmationPolicy` thresholds** — current values are placeholders; must be
  tuned against real footage for the 1–3 s latency vs FP/hour trade-off.
- **The build itself** — Milestone-1 sources are authored-correct but **not
  build-verified**; a Gradle sync + version bump is a prerequisite before any of
  the above.

---

## References

Consolidated from the per-topic research documents; primary sources checked
July 2026. Model versions, benchmarks, and EU AI Act enforcement dates are moving
targets — re-verify before relying on any figure in a commit.

- Licensing: Roboflow model-licence pages and Ultralytics licensing (AGPL-3.0
  across YOLO family; Enterprise licence); Roboflow RF-DETR repo (Apache-2.0 core
  / PML-1.0 Plus); Baidu RT-DETR (Apache-2.0).
- Models: Ultralytics YOLO26 (release 14 Jan 2026; NMS-free/DFL/MuSGD; nano
  figures); LearnOpenCV / arXiv 2509.25164; RF-DETR (ICLR 2026; DINOv2); pose —
  Google MediaPipe Pose Landmarker & MoveNet docs, OpenMMLab RTMPose; NanoDet
  (`RangiLyu/nanodet`).
- Runtime: Google Developers Blog "LiteRT" (28 Jan 2026); Android NNAPI
  deprecation guidance.
- Data: D-Fire (`gaiasd/DFireDataset`); FASDD (ESSD / Science Data Bank).
- Legal: EU AI Act (Reg. 2024/1689) Art. 5 / Annex III / Art. 50; GDPR Art. 9.
- Guardian docs: [models.md](models.md), [datasets.md](datasets.md),
  [benchmarking.md](benchmarking.md), [risks.md](risks.md),
  [roadmap.md](roadmap.md), [DECISION.md](DECISION.md).
