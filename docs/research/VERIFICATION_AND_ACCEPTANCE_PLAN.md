# Verification & Acceptance Plan

**Document type:** Engineering acceptance contract (pre-implementation).
**Audience:** CTO · Principal AI Engineer · Computer Vision Research Lead · QA /
Release Engineering · Data Protection Officer.
**Purpose:** define the objective, measurable, repeatable, auditable criteria that
any future implementation — decision, model, subsystem, or milestone — must
satisfy before it is *accepted*. This is not a test suite. It is the definition of
"acceptable," written so acceptance is a checkable fact rather than a matter of
opinion.

This plan governs the [engineering gates](#8--engineering-gates). It complements
and does not replace the existing research set: [architecture.md](architecture.md),
[models.md](models.md), [datasets.md](datasets.md),
[benchmarking.md](benchmarking.md), [risks.md](risks.md), [roadmap.md](roadmap.md),
[DECISION.md](DECISION.md),
[ENGINEERING_DECISION_MATRIX.md](ENGINEERING_DECISION_MATRIX.md).

---

## 0 — How thresholds are written (read this first)

**No performance number in this document is invented.** Every quantitative target
carries one of these tags:

- **`[MEASURE]`** — value unknown; must be obtained by measurement on the reference
  device set before it can be stated. No number is asserted.
- **`[RATIFY @ Gate N]`** — an absolute product budget/threshold to be decided and
  recorded at the named gate by the responsible role, using the first `[MEASURE]`
  baseline as input. Binding once ratified.
- **`[PROPOSED DEFAULT: …]`** — a conventional anchor offered *only* to start the
  ratification discussion. Not a measurement, not binding.
- **Binary requirement** — a yes/no structural property needing no measurement to
  define (e.g. "no network on the detection path"). Stated directly.

The working method is **baseline → budget → hold**: measure a baseline at a defined
milestone on defined hardware; ratify it into a budget; hold all subsequent work to
that budget. Acceptance then reduces to *"did metric X exceed its ratified budget —
yes or no?"* — objective, without pre-inventing absolute values.

Every criterion here is intended to be **objective** (a fact, not a judgement),
**measurable** (defined method + tool), **repeatable** (same procedure → same
result within stated variance), and **auditable** (produces a recorded artifact).
Where a value cannot yet be objective, it is tagged rather than guessed.

---

## 1 — Verification Philosophy

Six distinct activities are required. They are not substitutes for one another;
each answers a different question and catches a different failure class.

| Activity | Question | Checked against | Produces | Guards against |
|---|---|---|---|---|
| **Verification** | Did we build it *right*? | The specification | Pass/fail vs spec | Implementation that violates the design (e.g. an Android type leaking into the engine). |
| **Validation** | Did we build the *right thing*? | Real-world need | Fitness evidence | A system that meets spec but fails reality (fires on sunsets). |
| **Benchmarking** | How does it *perform*? | Controlled conditions | Numbers (latency, FPS, RAM, power, thermal) | Shipping blind on speed/resource cost. |
| **Regression testing** | Did a change make something *worse*? | The last ratified baseline | Delta + block-on-breach | Silent degradation over time. |
| **Acceptance testing** | Can we *accept* this deliverable? | This document | Formal accept/reject at a gate | Advancing on "looks done." |
| **Field testing** | Does it hold up in *the wild*? | Live pilot deployments | Real defect / FP-FN evidence | Lab-pass, field-fail. |

### Why an offline AI surveillance product needs all six

1. **No server-side hotfix.** On-device means a bad model or a leak ships and
   persists until an app update. Verification and regression discipline are the
   only safety net.
2. **AI is data-dependent and non-deterministic across domains.** Good benchmark
   numbers do not imply real-world correctness and vice versa — so benchmarking and
   validation are separate, both-mandatory guarantees.
3. **Safety and liability.** A false negative can mean a missed fire; a false
   positive erodes trust until the user disables the product (see
   [risks.md](risks.md)). This forces explicit, ratified FP/FN criteria plus field
   validation.
4. **Hardware heterogeneity.** Continuous camera + inference behaves differently by
   SoC, thermal envelope, and RAM. One device passing is not evidence; a device
   matrix is.
5. **Always-on operation.** Leaks, thermal throttling, battery drain, and
   background-kill only surface under long-duration and field testing, never in a
   five-minute run.

A build can be verified yet invalid, benchmarked well yet regressed next commit,
accepted in the lab yet failing in the field. The full set is the contract.

---

## 2 — Global Product Success Criteria

Product-wide metrics. **Tier:** **M** = mandatory (blocks release) · **N** =
nice-to-have (tracked, non-blocking) · **F** = future (post-MVP). Numeric targets
are `[MEASURE]`/`[RATIFY]` — none invented.

| # | Metric | Definition | Measurement (see §4) | Threshold | Tier |
|---|---|---|---|---|---|
| 1 | Offline operation | No network dependency on detection / alert-decision path | Static: no INTERNET on hot path. Dynamic: monitored zero network calls in airplane-mode session | **Binary — zero network I/O on the detection path; full function offline** | M |
| 2 | App stability / crash rate | Crash-free session rate | Crash reporting over defined sessions | `[MEASURE]`; accept `[RATIFY @ Gate 5]` `[PROPOSED DEFAULT: ≥99.5% crash-free sessions]` | M |
| 3 | Real-time inference | Sustains the design operating rate | Sustained processed-fps at pipeline output | Operating rate **2–5 fps by design** ([architecture.md](architecture.md)); sustained without drop-rate breach `[RATIFY @ Gate 4]` | M |
| 4 | Max end-to-end latency | Capture → confirmed-detection wall-clock | Timestamp delta; p50/p95/p99 | Budget `[MEASURE]` → `[RATIFY @ Gate 4]`; must fit the brief's 1–3 s confirmed-alert target | M |
| 5 | Min per-frame throughput (FPS headroom) | Model-capable fps above operating rate | Timed inference, percentiles | `[MEASURE]` → `[RATIFY @ Gate 4]` | M |
| 6 | Max RAM | Steady-state + peak process memory during monitoring | PSS/RSS over long run | `[MEASURE]` per tier → `[RATIFY @ Gate 4]` | M |
| 7 | Battery consumption | Drain during continuous monitoring | Battery Historian, controlled fixed-duration run | `[MEASURE]` (%/hr, normalized to capacity) → `[RATIFY @ Gate 4]` | M |
| 8 | Thermal stability | Sustained headroom; time-to-throttle | Android Thermal API over ≥30-min run | **No sustained throttling at operating rate on reference devices**; time-to-throttle `[MEASURE]` → `[RATIFY @ Gate 4]` | M |
| 9 | Startup time | Cold start → interactive monitoring | Trace markers, cold start | `[MEASURE]` → `[RATIFY @ Gate 5]` `[PROPOSED DEFAULT: ≤3 s, mid tier]` | N |
| 10 | Model loading time | Interpreter init + delegate warmup | Trace markers around init | `[MEASURE]` → `[RATIFY @ Gate 4]` | N |
| 11 | Storage footprint | Install size + model assets + event-store growth | Artifact size; event-store growth rate | Install `[MEASURE]`; event-store bounded by ratified retention `[RATIFY @ Gate 5]` | N |
| 12 | Detection reliability (recall) | True-positive rate on the sequestered field set | Field validation (§5) | Recall floor `[RATIFY @ Gate 2]`, per detector | M |
| 13 | False-positive tolerance | FP/hour on benign + confuser footage | Field validation | FP/hour ceiling `[RATIFY @ Gate 2]`, per detector — **product-killer metric** | M |
| 14 | False-negative tolerance | Miss rate on positive field set | Field validation | Miss ceiling `[RATIFY @ Gate 2]`, set against the FP trade-off | M |
| 15 | Recovery after camera interruption | Auto-resume after camera loss/reclaim, backgrounding, permission revoke/restore, rotation | Fault-injection | **Binary — resumes to healthy state within a bounded time** `[MEASURE]`, no crash, no silent stop | M |
| 16 | Long-running stability | No memory growth / throughput decay / thermal runaway over soak | ≥ N-hour soak; leak = memory-slope ≈ 0 | **Binary — no monotonic growth, no sustained decay** `[RATIFY @ Gate 4]` `[PROPOSED DEFAULT: ≥8 h soak]` | M |
| 17 | Calibration (if confidence surfaced) | Score → probability agreement | ECE / reliability on quantized model | If a probability is shown, ECE ≤ `[RATIFY @ Gate 2]`; **MVP default: confidence stays internal** ([models.md](models.md#calibration)) | M |
| 18 | Reproducible build | Same source → equivalent artifact | Repeat-build comparison | **Binary — reproducible and documented** | M |

**F-tier** items (multi-camera CCTV throughput, server-tier SLAs, biometric-feature
acceptance) are defined at the Enterprise-CCTV milestone, not here.

---

## 3 — Detector Acceptance Criteria

Criteria are **technology-agnostic** — they bind whatever model/approach is chosen.
Performance figures are `[MEASURE]`/`[RATIFY]`; environmental limits and required
scenarios are concrete (design decisions, not measurements). All FP/FN thresholds
are ratified at **Gate 2** against the **sequestered field set** (§5), never on
training data.

**Common Go/No-Go rule:** *Go* only if (a) all Pass/Fail rows pass across the full
reference device set, **and** (b) FP/hour and recall/miss on the sequestered field
set meet the Gate-2 thresholds, **and** (c) no open blocking issue. Else *No-Go*.
Owners: CV Research Lead (detection quality) + Principal AI Engineer (on-device
performance); recorded at the gate.

### 3.1 Camera Tampering — priority P0

| Field | Criterion |
|---|---|
| Required functionality | Detect lens cover/blocking, defocus/blur, abrupt scene-shift (repositioning) via classical CV — no model. |
| Expected behaviour | Raises tampering event after confirmation window; clears when tampering ends. |
| Latency / FPS / Memory / Battery / Thermal | **Binary — must run within the operating-rate budget with negligible added cost**; absolute figures `[MEASURE]`. Cheapest detector. |
| False-positive tolerance | Lights off/on and normal scene motion must not trigger. FP/hour ceiling `[RATIFY @ Gate 2]`. |
| False-negative tolerance | Must not miss sustained full-cover/defocus past the confirmation window. Miss ceiling `[RATIFY @ Gate 2]`. |
| Environmental limitations | Very low light weakens blur/scene-diff signal; document illumination floor `[MEASURE]`. |
| Required test scenarios | Hand/tape/cloth cover; spray/defocus; sudden repositioning; lights-off vs cover (must distinguish); gradual dusk (must **not** fire). |
| Minimum acceptable quality | Distinguishes tampering from ordinary lighting/scene change at ratified FP ceiling. |
| Failure conditions | Fires on normal light changes; misses sustained cover; degrades pipeline throughput. |
| Pass/Fail | Pass = all scenarios correct at ratified FP/FN on reference set. |
| Go/No-Go | Common rule. |

### 3.2 Fire — priority P1

| Field | Criterion |
|---|---|
| Required functionality | On-device flame detection; multi-frame confirmation before event. |
| Expected behaviour | Confirmed fire event within 1–3 s target; no alert on single-frame noise. |
| Latency / FPS / Memory / Battery / Thermal | Per global budgets (§2 #4–8); detector budget `[RATIFY @ Gate 4]`; baselines `[MEASURE]`. |
| False-positive tolerance | Must survive the confuser suite (sunset, red/orange objects, brake lights, screens, reflections). FP/hour ceiling `[RATIFY @ Gate 2]` — **gating metric**. |
| False-negative tolerance | Recall floor `[RATIFY @ Gate 2]`; an early flame missed then caught a few frames later is acceptable; a sustained missed fire is not. |
| Environmental limitations | Occluded / very distant / very small flames and heavy backlight are known-hard; document envelope `[MEASURE]`. |
| Required test scenarios | Real flame (candle → larger) at varied distance/angle; full confuser set as negatives; day/night; indoor/outdoor. |
| Minimum acceptable quality | Meets ratified recall floor **and** FP ceiling simultaneously on the sequestered set. |
| Failure conditions | Confuser FP above ceiling; recall below floor; latency outside target. |
| Pass/Fail | Pass = ratified FP **and** FN met on field set + performance within budget on reference set. |
| Go/No-Go | Common rule. |

### 3.3 Smoke — priority P1

| Field | Criterion |
|---|---|
| Required functionality | On-device smoke detection, multi-class head shared with fire. |
| Expected behaviour | Confirmed smoke event within target window; debounced against transient wisps. |
| Latency / FPS / Memory / Battery / Thermal | Shares fire model budget; incremental cost `[MEASURE]`. |
| False-positive tolerance | Expected **harder than fire** — steam, fog, haze, clouds, dust. FP/hour ceiling `[RATIFY @ Gate 2]`, tuned separately from fire. |
| False-negative tolerance | Recall floor `[RATIFY @ Gate 2]`; thin/dispersed smoke known-hard. |
| Environmental limitations | Kitchen steam and outdoor fog/haze are the dominant limitation; document envelope `[MEASURE]`. |
| Required test scenarios | Real smoke at varied density/distance; steam/fog/haze/dust as negatives; day/night. |
| Minimum acceptable quality | Ratified FP ceiling met **including** the steam/fog subset. |
| Failure conditions | Steam/fog FP above ceiling; thin-smoke recall below floor. |
| Pass/Fail | Pass = ratified thresholds met on field set incl. steam/fog subset. |
| Go/No-Go | Common rule. |

### 3.4 Intrusion — priority P2 (deferred)

| Field | Criterion |
|---|---|
| Required functionality | Person detection + user-defined polygon zone(s) + entry/exit logic. |
| Expected behaviour | Event when a person enters a defined zone; none outside zones. |
| Latency / FPS / Memory / Battery / Thermal | Per global budgets; `[MEASURE]` → `[RATIFY @ Gate 4]`. |
| False-positive tolerance | Pets, shadows, reflections, non-person motion must not trigger. FP/hour ceiling `[RATIFY @ Gate 2]`. |
| False-negative tolerance | Partial occlusion / edge crowding known-hard; miss ceiling `[RATIFY @ Gate 2]`. |
| Environmental limitations | Zone accuracy depends on camera placement; low light degrades person detection. Envelope `[MEASURE]`. |
| Required test scenarios | Person crossing boundary (in/out); pet in zone (no trigger); occluded entry; multiple people; night. |
| Minimum acceptable quality | Correct zone entry/exit at ratified FP/FN. |
| Failure conditions | Pet/shadow FP; missed entries; boundary instability. |
| Pass/Fail | Pass = ratified thresholds + correct geometry on reference set. |
| Go/No-Go | Common rule. |

### 3.5 Fall — priority P3 (deferred, hardest)

| Field | Criterion |
|---|---|
| Required functionality | Pose estimation + temporal state machine distinguishing a fall from normal posture transitions. |
| Expected behaviour | Confirmed fall event after temporal criteria; no event for sitting/lying/bending. |
| Latency / FPS / Memory / Battery / Thermal | Per global budgets; pose + temporal cost `[MEASURE]` → `[RATIFY @ Gate 4]`. |
| False-positive tolerance | Fast sitting, lying, bending, exercising are strong confusers. FP/hour ceiling `[RATIFY @ Gate 2]`. |
| False-negative tolerance | Partial-view/occluded falls known-hard; miss ceiling `[RATIFY @ Gate 2]`. |
| Environmental limitations | Highly camera-angle-dependent; single-person pose assumption; **dataset scarcity is the core limitation** ([datasets.md](datasets.md)). |
| Required test scenarios | Staged falls (multiple directions) with consented adults under safe conditions; sit/lie/bend as negatives; occluded/off-angle cases. |
| Minimum acceptable quality | Meets ratified FP/FN — **acceptance may prove infeasible at MVP quality**; that is itself a valid gate result. |
| Failure conditions | Posture-transition FP above ceiling; missed falls above ceiling. |
| Pass/Fail | Pass = ratified thresholds on field set; **explicit No-Go permitted** if the data problem cannot be closed. |
| Go/No-Go | Common rule, with explicit No-Go option. |

---

## 4 — Benchmark Plan

Benchmarking produces the `[MEASURE]` baselines that budgets are ratified from.
Every run is reproducible: fixed build, fixed input, fixed device state, recorded
procedure and raw trace.

### 4.1 Device matrix (by hardware class, not named unverified devices)

Final device list `[RATIFY @ Gate 3]`. Minimum three tiers:

| Tier | Class | RAM | Accelerator | Android |
|---|---|---|---|---|
| **A — floor** | Entry/older SoC | 3–4 GB | CPU only (no usable NPU) | Minimum supported (`minSdk` `[RATIFY @ Gate 3]`) |
| **B — mainstream** | Mid-range SoC | 6–8 GB | GPU delegate viable | Recent mainstream |
| **C — high-end** | Flagship SoC | ≥8 GB | Dedicated NPU | Latest |

**Budgets are ratified against Tier A**, not the flagship — a Tier-C-only pass is
not evidence for the devices most users carry. `minSdk`/`targetSdk` ratified at
Gate 3 with the coverage-vs-performance trade-off recorded.

### 4.2 Condition matrix

Each run is labelled with its conditions; results reported per cell, never averaged
across cells:

- **Lighting:** bright day · overcast · indoor artificial · dusk/low-light · night.
- **Location:** indoor · outdoor.
- **Camera motion:** static (mounted) · handheld/moving · shake.
- **Duration:** short (functional) · **soak** (`[PROPOSED DEFAULT: ≥8 h]`) ·
  **stress** (max fps, thermal-soak, low-memory).

### 4.3 Metrics — method and tool (one defined method each, for repeatability)

| Metric | Method | Tool |
|---|---|---|
| Inference latency | Wall-clock around model invoke; discard N warmup iters; time ≥M iters; p50/p95/p99/max; per delegate; report **cold** and **thermally-soaked** separately | Perfetto markers / in-app timing |
| End-to-end frame latency | Capture-ts → detection-result-ts (incl. NV21→RGB + inference + post); percentiles | Perfetto / `FrameMetrics` |
| Sustained FPS + drop rate | Processed frames/sec at pipeline output over fixed window; count frames dropped by `KEEP_ONLY_LATEST` | CameraX `ImageAnalysis` counters / Perfetto |
| RAM (steady + peak) | PSS/RSS sampled over run; **leak check = linear-fit slope over soak ≈ 0** | Android Studio Memory Profiler / `Debug.getMemoryInfo` / Perfetto |
| Battery | Fixed-duration controlled run (fixed brightness, controlled/airplane network, fixed scene); mAh + %/hr normalized to capacity | Battery Historian (bugreport) + `BatteryManager` proxy |
| Thermal | Sample `PowerManager.getThermalHeadroom()` at fixed interval + `THERMAL_STATUS_*` transitions over ≥30-min run; report **time-to-throttle** and steady-state headroom | Android Thermal API |
| UI jank | Frame render timing during active monitoring UI | `JankStats` / `FrameMetrics` / Perfetto |
| Startup / model load | Trace spans: process-start→interactive; interpreter-init→first-inference | Perfetto / Android Studio Profiler |
| Crash-free rate | Crashes per session over defined corpus/cohort | Crash reporting |
| Recovery time | Fault-injection (revoke camera, background, rotate, kill-and-restore); time to healthy resume | Instrumented test + logs |

### 4.4 Reporting

Each run yields a recorded artifact: build ID, device/tier, condition cell, raw
trace, derived metrics with percentiles and variance across repeats. A metric is
not "known" until measured on **Tier A** under the relevant condition cell, with
variance reported.

---

## 5 — Real-world Validation Plan

Benchmarks prove speed; validation proves the model is *right* on reality. The
central risk is the domain gap between public data and target environments
([datasets.md](datasets.md)).

### 5.1 Data hierarchy (increasing realism)

1. **Dataset validation** — held-out split of public data (D-Fire / FASDD_CV), plus
   **cross-dataset evaluation** (train on one, test on the other) to expose
   dataset-specific overfitting.
2. **Controlled recordings** — scripted scenarios on the target camera in
   target-like settings: real positives and the full confuser set as negatives, at
   varied distance/angle/lighting.
3. **Real environments (pilot)** — consented deployments in actual target sites,
   producing uncontrolled footage over time (the field set).

### 5.2 The sequestered field set (overfitting control)

- A **frozen, versioned field test set** from environments and time periods **not**
  in training/tuning data.
- **Never used for training, tuning, or threshold selection** — acceptance
  measurement only. Any leakage voids the result.
- Gate-2 FP/FN thresholds are ratified and measured **on this set only**; tuning
  uses a separate development set.
- Additional guards: cross-dataset evaluation; a dedicated **confuser-only FP
  suite** (sunset, red objects, steam, fog, reflections, screens, brake lights);
  and a fresh field slice re-collected **after each model freeze**, so acceptance is
  never measured on data the model has effectively seen.

### 5.3 Required edge-case coverage (FP/FN reported per case)

**Weather** (rain, fog, haze) · **lighting** (bright, backlit, dusk, near-dark) ·
**occlusions** (partial objects, obstructions) · **camera movement and shake** ·
**steam** (kitchen) vs real smoke · **reflections** (glass, mirrors, screens) ·
**crowds** · **pets** · **children** · **partially-visible objects** · enumerated
**rare situations** (fireworks, coloured stage/party lighting, welding, vehicle
lights).

### 5.4 People, children, and biometric data

- Field footage is **personal data**: consent, data minimisation, defined
  retention, secure handling ([risks.md](risks.md)).
- **Children:** minimise collection, obtain appropriate consent, and **build no
  biometric profiles of minors.** The fire/smoke/tampering MVP does not identify
  people, and validation must not introduce identification. Any move toward
  face/behaviour recognition triggers a DPIA/FRIA and legal review **before** data
  is collected — not after.

---

## 6 — Regression Strategy

Because the product ships offline, an escaped regression reaches devices and cannot
be hotfixed. Each regression class has a guard, a baseline, and a blocking gate,
all comparing to the **last ratified baseline** — never to "looks fine."

| Class | Guard | Baseline | Blocks merge/release? |
|---|---|---|---|
| Model quality | Fixed, versioned eval suite (sequestered field set + confuser suite); metrics recorded per model version | Ratified detection metrics (Gate 2) | Yes — if recall/FP breach tolerance `[RATIFY]` |
| False-positive | Confuser-only FP suite on every model/threshold change | Ratified FP/hour ceiling | Yes |
| False-negative | Positive field-set recall on every model change | Ratified recall floor | Yes |
| Performance (latency/FPS) | CI/nightly benchmark on Tier-A device | Ratified latency/FPS budget (Gate 4) | Yes — on breach |
| Memory | Soak memory-slope + peak PSS on reference device | Ratified memory budget | Yes |
| Battery | Controlled run at gate cadence (expensive; not per-commit) | Ratified %/hr budget | Yes at gate cadence |
| Latency | Included in the performance benchmark; percentiles tracked | Ratified latency budget | Yes |
| Architecture | Enforced module-dependency rule: `:core:model` / `:core:detection` stay Android-free; `FrameSource` seam not bypassed | Architecture invariants ([architecture.md](architecture.md)) | Yes — build-level check |
| Compatibility | Build + smoke test across `minSdk`→`targetSdk` and delegate-fallback (GPU/NPU→CPU) | Supported matrix | Yes |

**Golden artifacts** (field test set, confuser suite, benchmark harness) are
versioned and frozen; changing them is itself a reviewed event, because it moves the
baseline. A build cannot be accepted against a silently changed yardstick.

---

## 7 — Milestone Exit Criteria

Milestones are delivery phases (what is built); each has objective exit criteria and
is not "done" until all are satisfied and recorded. Milestones feed the
[gates](#8--engineering-gates) (decision checkpoints).

| Milestone | Deliverables | Documentation | Benchmark results | Test coverage | Demonstration | Approvals | Go/No-Go |
|---|---|---|---|---|---|---|---|
| **Research** | Complete research set | This plan + all research docs | — (defines how to benchmark) | — | Design-review walkthrough | CTO, PAIE, CV Lead | Go = Gate 0 exit met |
| **Prototype (feasibility spike)** | Phase-0 FP-rate spike on real footage | Spike result + go/adjust memo | FP-rate on target footage `[MEASURE]` | Spike harness | FP-rate demo on real scenes | CV Lead, PAIE | Go = viability evidence recorded |
| **Offline AI (fire/smoke model)** | Trained, quantized, LiteRT-exported detector(s) | Model card, dataset-licence audit, calibration report | Detection metrics on sequestered set; on-device latency/mem baseline (Tier A) | Model eval + confuser suite | Offline on-device inference demo | CV Lead + PAIE | Go = Gate 2 exit met |
| **Android MVP** | Fire + smoke + tampering integrated; foreground service; local alert engine; event history | Updated architecture notes, test report | Full global-metrics benchmark (§2) across device matrix | Detector acceptance (§3) + regression suite | End-to-end offline demo on Tier-A device | PAIE, QA | Go = Gates 3 & 4 exit met |
| **Public Beta** | Signed release build; crash reporting; opt-in field capture | Privacy/consent notice, release notes, runbook | Crash-free, soak, battery on real cohort | Full acceptance + field validation | Pilot deployment | CTO, QA, DPO | Go = Gate 5 exit met |
| **Enterprise CCTV** | RTSP frame source; server-tier inference; multi-camera | DPIA/FRIA, security review, SLA spec | Server-tier throughput/latency SLAs | Acceptance at CCTV scale + biometric-feature review (if any) | Enterprise pilot | CTO, DPO, Security | Go = Gate 6 exit met |

---

## 8 — Engineering Gates

Gates are formal decision checkpoints between milestones. Nothing proceeds past a
gate until its exit requirements are met, recorded, and signed off. A gate with an
open **blocking issue** is a hard stop.

| Gate | Name | Entry | Exit | Blocking issues (examples) | Owner |
|---|---|---|---|---|---|
| **0** | Research Complete | Full research set drafted | All research reviewed; open decisions listed; this plan accepted | Unresolved architecture ambiguity | CTO |
| **1** | Architecture Locked | Milestone-1 shell builds | Build verified; module invariants enforced (engine Android-free; seam intact); device matrix + `minSdk` ratified | Build unverified; seam violated | Principal AI Engineer |
| **2** | AI Model Approved | Trained/quantized detector + calibration report | Recall floor + FP ceiling met on **sequestered field set**; dataset licences verified; model licence confirmed | FP/hour above ceiling; AGPL model chosen without licence | CV Research Lead |
| **3** | Android Integration Approved | Detector(s) integrated behind the seam | End-to-end offline on Tier-A device; recovery-after-interruption passes; foreground service stable | Silent stop after camera reclaim; background-kill | Principal AI Engineer |
| **4** | Performance Approved | Full benchmark across device matrix | Latency/FPS/RAM/battery/thermal within **ratified budgets on Tier A**; soak shows no leak/decay/thermal runaway | Throttling at operating rate; memory growth | Principal AI Engineer + QA |
| **5** | Public Beta Ready | Signed build; crash reporting; consent flow | Crash-free rate ratified; regression suite green; privacy notice + retention in place; reproducible build | Crash rate above threshold; no consent flow | CTO + QA + DPO |
| **6** | Commercial Release Ready | Beta evidence; security + legal review | Security review passed; DPIA/FRIA complete for any personal-data/biometric feature; SLAs met; rollback plan | Unmitigated high risk; biometric feature without legal clearance | CTO + Security + DPO |

---

## 9 — Deployment Readiness Checklist

Binary and auditable. Each item has an **owner** and a required **evidence
artifact** (what an auditor inspects). Release is not ready until every mandatory
item is checked with evidence attached. *(Status reflects true current state:
pre-implementation — nothing complete.)*

| # | Item | Owner | Evidence artifact | Status |
|---|---|---|---|---|
| 1 | Architecture reviewed & invariants enforced | PAIE | Gate 1 record + module-dep check | ☐ |
| 2 | Model licence verified (per shipped model) | CV Lead | Licence confirmation note | ☐ |
| 3 | Dataset licences verified for training weights | CV Lead | Dataset licence audit | ☐ |
| 4 | Benchmark completed across device matrix | PAIE/QA | Gate 4 benchmark report | ☐ |
| 5 | Offline operation verified (airplane-mode session) | QA | Network trace: zero hot-path calls | ☐ |
| 6 | Performance within ratified budgets (Tier A) | PAIE | Gate 4 record | ☐ |
| 7 | Battery within ratified budget | PAIE/QA | Battery Historian report | ☐ |
| 8 | Thermal: no sustained throttling at operating rate | PAIE/QA | Thermal-run report | ☐ |
| 9 | Long-running soak passed (no leak/decay) | QA | Soak-test report | ☐ |
| 10 | Recovery-after-interruption passed | QA | Fault-injection report | ☐ |
| 11 | Crash-free rate meets ratified threshold | QA | Crash-reporting export | ☐ |
| 12 | Regression suite passed vs ratified baseline | QA | CI/regression report | ☐ |
| 13 | Detection FP/FN meet ratified thresholds (field set) | CV Lead | Gate 2 validation report | ☐ |
| 14 | Calibration acceptable (if confidence surfaced) | CV Lead | Reliability/ECE report | ☐ |
| 15 | Security review completed | Security | Security sign-off | ☐ |
| 16 | Privacy / consent / retention in place (if capturing footage) | DPO | Privacy notice + DPIA (as applicable) | ☐ |
| 17 | Documentation complete & current | PAIE | Doc index review | ☐ |
| 18 | APK/build reproducible & documented | Release Eng | Reproducible-build record | ☐ |
| 19 | Rollback / update plan defined | Release Eng | Runbook | ☐ |

---

## 10 — Engineering Dashboard

A single project-health view, **generated from the recorded artifacts** above (a
hand-kept dashboard is not auditable). Status tokens: `⬜ Not started` ·
`🟡 In progress` · `🟢 Met/passing` · `🔴 Failing/blocked` · `— N/A`.

**Current snapshot — pre-implementation (honest baseline):**

| KPI | Source | Status |
|---|---|---|
| Detector readiness — Fire | Gate 2/3/4 records | ⬜ Not started |
| Detector readiness — Smoke | Gate 2/3/4 records | ⬜ Not started |
| Detector readiness — Camera Tampering | Gate 3 record | ⬜ Not started |
| Detector readiness — Intrusion | Gate 2/3/4 records | ⬜ Not started (deferred) |
| Detector readiness — Fall | Gate 2/3/4 records | ⬜ Not started (deferred) |
| Benchmark status | §4 reports | ⬜ No baselines measured |
| Regression status | CI/regression report | ⬜ Suite not established |
| Performance status (vs budget) | Gate 4 record | ⬜ Budgets not yet ratified |
| Documentation status | Doc index | 🟡 Research complete; impl docs pending |
| Risk level | [risks.md](risks.md) | 🟡 Elevated — FP-rate, model-licence, biometric-legal open |
| Overall release readiness | §9 checklist | ⬜ 0/19 complete |

**Per-detector readiness state machine** (gate-controlled): `Not started → In
development → Benchmarked (perf baseline) → Validated (field FP/FN met) → Accepted
(gate signed)`.

**Honesty rule:** a KPI shows 🟢 only when its backing artifact exists and meets the
ratified threshold. "Looks done" is never 🟢.

---

## 11 — Open Questions

The verification ledger, strictly separated so no assumption is mistaken for a
result.

### Verified facts (recorded, external)
- Model/dataset **licences** (AGPL YOLO family; Apache RF-DETR core / RT-DETR / pose
  models / OpenCV; PML for RF-DETR Plus) — see the decision matrix.
- **Dataset composition** (D-Fire counts incl. ~9,838 negatives; FASDD scale + CV
  split).
- **Platform constraints** needing no measurement: device-side SMS blocked by Google
  Play; NNAPI deprecated; WorkManager 15-min floor unsuitable for continuous
  monitoring.

### Engineering assumptions (believed, unverified)
- The chosen model **will meet FP/FN targets after domain fine-tuning** — assumed;
  the Phase-0 spike is the first test.
- **Quantization accuracy loss is acceptable** — must be measured on the quantized
  model.
- **RF-DETR-N is viable on Tier-A devices** — the biggest performance unknown behind
  the model choice.
- The **2–5 fps operating rate is sufficient** for timely detection at target
  latency — validate against field footage.

### Unknowns (require measurement before any number is asserted)
- All on-device figures — **latency, FPS, RAM, battery, thermal time-to-throttle,
  startup, model-load** — each `[MEASURE]` on Tier A.
- **FP/hour and recall/miss on the sequestered field set** — the acceptance-gating
  numbers.
- **Crash-free rate, soak stability, recovery time.**
- **Calibration (ECE)** on the quantized model.

### Future experiments (to close the unknowns)
1. **Phase-0 feasibility spike** — FP-rate on real target footage (gates the product).
2. **Device-matrix benchmark** — establishes Tier-A baselines (Gate 4).
3. **Sequestered field validation** — FP/FN acceptance (Gate 2).
4. **Soak & thermal runs** — long-running stability (Gate 4).
5. **Calibration study** — only if a confidence number is ever surfaced.
6. **Cross-dataset & confuser evaluation** — overfitting control, ongoing.

---

## References

- Guardian research set: [architecture.md](architecture.md), [models.md](models.md),
  [datasets.md](datasets.md), [benchmarking.md](benchmarking.md),
  [risks.md](risks.md), [roadmap.md](roadmap.md), [DECISION.md](DECISION.md),
  [ENGINEERING_DECISION_MATRIX.md](ENGINEERING_DECISION_MATRIX.md).
- Measurement tooling (standard Android platform tooling): Perfetto system tracing;
  Android Studio CPU/Memory/Energy profilers; Battery Historian; Android Thermal API
  (`PowerManager` thermal headroom / status); `FrameMetrics` / `JankStats`; CameraX
  `ImageAnalysis` diagnostics.

> This is a verification **specification**, not a set of results. Every
> `[MEASURE]`/`[RATIFY]` tag marks a value that must be produced by measurement and
> recorded before it becomes binding. No performance number here is invented.
