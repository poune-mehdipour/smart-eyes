# Risks

Ordered roughly by how badly each can hurt the product. The ones marked
**🔴 KILL-RISK** can end the project or expose it to serious liability; treat them
as gating.

## 🔴 False-positive rate on real footage (product-killer)

The central technical risk. A hazard alerter that cries wolf gets disabled by its
users and then protects nobody — a false alarm is not a minor annoyance, it is the
failure that voids the product's entire value. Public benchmarks say nothing about
FP rate in *your* specific environments and lighting.

- **Mitigation:** the Phase-0 feasibility spike measures this before commitment
  (see [benchmarking.md](benchmarking.md)); FP/hour per environment and per
  confuser is the tracked metric, not mAP; the multi-frame `ConfirmationPolicy`
  debounce exists specifically to suppress single-frame noise; the retrain
  flywheel targets the confusers that actually fire.
- **Residual:** some environments may simply be too adversarial for an acceptable
  rate at MVP quality. Better to learn that on day 3 than month 3.

## 🔴 Model licence (commercial-killer)

Building on an AGPL-3.0 model (all Ultralytics YOLO, incl. YOLO26) in a
closed-source, distributed/SaaS product can obligate source disclosure under the
same licence. Discovering this after the architecture is built on it is expensive.

- **Mitigation:** decide licence *before* choosing a model (see
  [models.md](models.md)). Either buy an Ultralytics Enterprise licence
  deliberately, or default to Apache-2.0 RF-DETR / a permissively-licensed
  architecture. Also check **dataset** licences before training weights for a
  commercial product (non-commercial dataset licences can taint the weights).
- **Residual:** licence terms change; re-verify at ship time.

## 🔴 Legal — GDPR + EU AI Act on the CCTV / biometric roadmap (liability-killer)

The fire/smoke MVP is legally clean: no personal data, no biometrics. The later
roadmap is not, and the jump is a cliff, not a slope.

- **GDPR:** biometric data used to *uniquely identify* a person is **special-
  category** data (Art. 9) — face recognition for the apartment-access feature
  lands here, requiring explicit consent or a narrow legal basis, and GDPR is
  often *more* restrictive than the AI Act for private deployments. **Licence
  plates are personal data too** — automated plate recognition is squarely in
  GDPR scope even without faces. Baseline CCTV also triggers GDPR duties
  (lawful basis, signage/transparency, minimisation, retention limits).
- **EU AI Act (Reg. 2024/1689):**
  - **Prohibited practices are already in force (since 2 Feb 2025)** — including
    *untargeted scraping of facial images from the internet or CCTV to build/
    expand a face-recognition database*, and *biometric categorisation inferring
    sensitive attributes*. If the "learn faces from camera footage" idea is
    implemented naively, it can land on the **prohibited** list — penalties up to
    €35M or 7% of global turnover. This is the sharpest edge in the whole
    roadmap.
  - **Biometric identification/categorisation is high-risk (Annex III)** —
    conformity assessment, risk management, data governance, logging, human
    oversight, and a Fundamental Rights Impact Assessment. Most high-risk
    obligations apply from **2 Aug 2026** (some biometric-specific timings have
    been subject to adjustment — verify current status rather than trusting a
    remembered date).
  - **Transparency (Art. 50):** even permitted biometric-categorisation / emotion
    systems must inform the people subjected to them.
- **Mitigation:** assume this in the architecture *now*, even though the MVP does
  not touch it. Prefer verification over identification where a feature allows it;
  keep biometric/behavioural processing behind a clear module and data boundary;
  do a proper DPIA/FRIA before any face/plate/behaviour feature ships; get
  qualified legal review — this document is engineering guidance, not legal
  advice. **Do not** build a face-learning-from-footage feature without that
  review; it is the most likely single path to a prohibited-practice violation.
- **Residual:** regulatory timelines and guidance are still moving through
  2026–2027; re-check before shipping anything biometric.

## On-device performance: battery, thermal, latency

Continuous camera + inference is a sustained load that can drain battery, heat the
SoC into throttling (latency creeps up over minutes), and be unviable on low-end
devices.

- **Mitigation:** 2–5 fps source-side throttle; INT8-quantized model; drop-don't-
  queue frame handling; per-device performance profiling incl. a 30-min thermal
  run (see [benchmarking.md](benchmarking.md)); CPU as guaranteed floor with
  GPU/NPU as opportunistic acceleration behind capability checks.
- **Residual:** the low-end-device floor may force a resolution/fps compromise
  that trades some detection quality for viability.

## Calibration / overclaiming

Surfacing a raw model score as "91% confidence" is misleading — raw scores are not
calibrated probabilities, and quantization shifts them further. It also erodes the
user trust the product depends on.

- **Mitigation:** confidence is an internal health signal until calibrated (ECE /
  reliability measured on the quantized model); no auto-escalation until then (see
  [models.md](models.md#calibration)).

## Android background-execution reliability

Aggressive OEM battery managers kill background apps; a monitoring app that gets
silently killed protects nobody. `WorkManager` is the wrong tool (15-min minimum,
deferrable) and would be an active reliability bug here.

- **Mitigation:** a proper **foreground service** with a persistent notification
  (Milestone 4); guidance for users to exempt the app from OEM battery
  optimization; treat "did monitoring actually stay alive" as a tested property.

## Data flywheel privacy

The retrain loop captures field footage, including from people's homes/premises.
Done carelessly this is itself a GDPR problem and a trust breach.

- **Mitigation:** explicit consent, data minimisation (capture only what training
  needs), clear retention limits, and secure handling on the backend. The capture
  pipeline is designed with this constraint, not retrofitted.

## Scope creep across the 50-scenario list

The 50-item scenario list is a temptation to build breadth before depth. Ten
mediocre detectors are worth less than three reliable ones; breadth added before
the fire/smoke FP problem is solved dilutes the effort that matters.

- **Mitigation:** MVP is hard-scoped to fire + smoke + camera-tampering; the
  architecture makes each later scenario *additive* (one module) so there is no
  pressure to build them early to "keep the design open." Depth first.

## Fall detection is harder than it looks (a specific technical trap)

Fall detection is materially harder than fire — pose + temporal logic + a long
edge-case tail (fast sitting, lying down, bending to pick something up). Treating
it as a "week 3" object-detector drop-in underestimates it by an order of
magnitude and would sink an MVP timeline.

- **Mitigation:** deferred to Milestone 6 as its own difficulty class; approached
  as pose model + temporal state machine, not a single-frame detector.

## References

- EU AI Act (Regulation (EU) 2024/1689) — Article 5 prohibited practices (in
  force 2 Feb 2025); Annex III high-risk biometric ID/categorisation; Article 50
  transparency; high-risk obligations from 2 Aug 2026; penalty tiers. Sources:
  official AI Act text and summaries; EDPB/EDPS guidance on the AI Act × GDPR
  intersection; practitioner compliance guides (mid-2026).
- GDPR — Article 9 (special-category / biometric data); Article 5 principles;
  licence-plate and CCTV footage as personal data.
- Android Developers — foreground services; background execution limits;
  WorkManager periodic-work minimum interval.
- Cross-refs: [models.md](models.md), [datasets.md](datasets.md),
  [benchmarking.md](benchmarking.md), [roadmap.md](roadmap.md).

> Legal content here is engineering-oriented risk framing, **not** legal advice,
> and AI Act enforcement dates are still moving through 2026–2027. Obtain
> qualified legal review before shipping any feature that processes personal or
> biometric data.
