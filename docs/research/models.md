# Models & Runtimes

This is the "which brain, on which runtime, under which licence" document. The
single most important point comes first because it constrains everything else.

## Resolve licensing *before* choosing a model

Guardian is a closed-source, sellable product. That makes model licence a
first-class selection criterion, not a footnote.

- **Ultralytics YOLO (v5, v8, v11, v12, YOLO26) is AGPL-3.0.** AGPL is a strong
  copyleft licence: if you distribute a product built on AGPL code — or even
  offer it as a network service — you can be obliged to release your own source
  under the same terms. For a proprietary product this is a live legal exposure,
  not a theoretical one. The escape hatch is a **paid Ultralytics Enterprise
  licence**. So "use YOLO26" is really "buy an Ultralytics commercial licence, or
  don't ship YOLO." Decide this before building an architecture on top of it.
- **RF-DETR (Roboflow) core models are Apache 2.0.** The `rfdetr` package and the
  Apache-designated weights (sizes Nano through Large) are permissively licensed —
  commercial use, modification, and distribution with no source-disclosure
  requirement. The "Plus" tier (`rfdetr_plus`, RF-DETR-XL / 2XL) is under a
  separate PML 1.0 licence, so stay within the Apache-designated sizes to keep
  things clean.

**Consequence:** the licence-safe default for a proprietary edge product is
either RF-DETR (Apache 2.0) or a non-Ultralytics architecture, *unless* the
Ultralytics Enterprise licence is purchased deliberately. This is a business
decision with a legal component; it should be made explicitly, on the record.

## The model landscape (mid-2026)

### YOLO26 — the edge-first default, if licensing is handled

Ultralytics released YOLO26 on 14 January 2026, positioned explicitly for
edge/CPU deployment. The two changes that matter for this project:

- **NMS-free, end-to-end inference.** The head is trained to emit one
  conflict-free box per object, removing Non-Maximum Suppression entirely. This
  kills the data-dependent latency spike that NMS causes on CPU (inference time
  becomes roughly constant regardless of how many objects are in frame) and
  removes the hand-tuned IoU/confidence post-processing thresholds from
  deployment. For a real-time monitoring loop, deterministic per-frame latency is
  worth a lot.
- **Distribution Focal Loss (DFL) removed.** DFL relied on softmax-heavy ops that
  low-power edge accelerators handle poorly; dropping it improves export and
  quantization compatibility.

Reported results for the nano model (`YOLO26n`): ~40.9 mAP with CPU inference of
~38.9 ms versus ~56.1 ms for YOLO11n — a headline of "up to ~43% faster CPU
inference" for the family. It is a multi-task family (detection, segmentation,
pose/keypoints, oriented boxes, classification), which matters later: the pose
head is a candidate for the deferred fall detector.

**Caveat:** these are the vendor's published benchmarks on reference hardware.
They are a reason to shortlist, not a number to ship. Re-measure on target phones
(see [benchmarking.md](benchmarking.md)). And YOLO26 is AGPL — see above.

### YOLO12 — attention-centric, same licence problem

YOLO12 (early 2025) is an attention-centred YOLO generation. It is a reasonable
model but carries the same Ultralytics AGPL-3.0 constraint, and for a CPU/edge
target YOLO26 supersedes it on the metrics that matter here (NMS-free determinism,
DFL removal, CPU speed). There is no licence-based or performance-based reason to
prefer YOLO12 over YOLO26 for this project; it is listed for completeness.

### RF-DETR — the data-efficiency and licence-safety play

RF-DETR (Roboflow, accepted at ICLR 2026) is a real-time detection **transformer**
built on a **DINOv2** self-supervised backbone. Two properties make it directly
relevant:

- **Data efficiency.** The pre-trained DINOv2 backbone transfers well to small,
  custom datasets and tends to converge in fewer epochs than YOLO on limited
  data, with stronger behaviour on domain-shift benchmarks (RF100-VL). For a
  project whose hardest problem is a small, self-collected dataset in a specific
  visual domain (see [datasets.md](datasets.md)), that is the single most useful
  property a model can have.
- **Licence.** Apache 2.0 for the core sizes. Commercially clean.

**Honest tradeoff:** RF-DETR's headline accuracy/latency numbers (e.g. RF-DETR-L
~56.5 AP at ~6.8 ms; 2XL ~60.1 AP, the first real-time model past 60 AP on COCO)
are measured on a datacentre GPU (NVIDIA T4, TensorRT FP16), *not* on a phone CPU.
DETR-family transformers are generally heavier and less mature for pure on-device
CPU inference than a YOLO-nano CNN. So the realistic split is:

- **Best pure on-device CPU real-time (licence handled):** YOLO26n.
- **Best data-efficiency + domain adaptation + licence-clean:** RF-DETR (train on
  small data, deploy the smallest size, accept a heavier runtime — or run it
  server-side for the CCTV tier where a GPU is available).

These are not mutually exclusive across tiers: a licence-clean RF-DETR on the
server-side CCTV path and a small quantized model on the phone are a coherent
combination.

### Camera-tampering needs no model at all

The tampering detector (cover, blur, sudden scene-shift) is **classical CV** —
frame differencing, blur/variance metrics, histogram/scene-change detection. No
model, no dataset, no licence question, negligible compute. It is the first
detector to build precisely because it proves the whole `Detector → @IntoSet →
pipeline` path on the cheapest possible target.

## On-device runtime

### LiteRT (formerly TensorFlow Lite)

The default on-device inference runtime for Android is **LiteRT** — the runtime
Google renamed from TensorFlow Lite in 2024 and has since broadened. As of the
28 January 2026 release, production NPU acceleration has graduated into the main
stack, with reported ~1.4× faster GPU inference than legacy TFLite and multi-
framework model import (TensorFlow, PyTorch, JAX). It handles hardware delegation
(NPU → GPU → CPU fallback) so the same model can target a range of devices.

**Delegates and hardware acceleration:**

- **GPU delegate** — good general-purpose acceleration; runs FP16/FP32 without
  requiring quantization, so it avoids quantization accuracy loss.
- **NPU delegate** — best-in-class where available, via the newer LiteRT stack.
- **NNAPI** — the old Android neural-networks bridge. It is being **deprecated**
  (Google's own guidance now steers new work toward LiteRT delegates instead), so
  do not build new dependencies on it. Treat CPU as the guaranteed floor and
  GPU/NPU as opportunistic acceleration behind capability checks.

### Alternatives worth knowing

- **ONNX Runtime Mobile** — sensible if the model pipeline standardizes on ONNX;
  RF-DETR and YOLO both export to ONNX.
- **NCNN** (Tencent) — a lean, mobile-first inference framework with a strong
  track record running YOLO-family models on phones; a fallback if LiteRT
  conversion of a specific model is troublesome.

Recommendation: standardize on **LiteRT**, keep the model in an interchange
format (ONNX) so the runtime is not a one-way door, and always ship a CPU path
that works before enabling any delegate.

## Quantization

To hit the battery/thermal/latency budget, the shipped model will almost
certainly be **INT8-quantized** (post-training quantization, or quantization-aware
training if PTQ costs too much accuracy). Quantization is not free: it trades a
few points of accuracy for large latency/size/power wins, and it *shifts the
score distribution*, which is why calibration (below) must be done on the
quantized model, not the float one. YOLO26's DFL removal was explicitly aimed at
making models quantization- and export-friendly, which helps here.

## Calibration — confidence is not a probability {#calibration}

A raw detector score (the number the model emits per box) is **not** a calibrated
probability. Displaying "Fire — 91% confidence" off a raw score is exactly the
kind of overclaiming this project refuses to do: the model may be badly
mis-calibrated, especially after quantization and especially on out-of-domain
footage. Two positions are acceptable and nothing in between:

1. **Don't show a number.** Treat the score as an internal health/gating signal
   only (this is the Milestone-1 stance), or
2. **Calibrate it first** — temperature scaling, Platt scaling, or isotonic
   regression fitted on held-out footage from the target domain — and only then
   surface a probability.

Calibration is also a hard prerequisite before any *automatic* escalation
(placing a call, notifying a contact), because an auto-action is only as
trustworthy as the probability behind it. See the confirmation thresholds in
`ConfirmationPolicy.defaults()` — those are placeholders for tuning against real
footage, not values to ship.

## References

- Ultralytics — YOLO26 launch and YOLO26-vs-YOLO11 comparison (release
  14 Jan 2026; NMS-free / DFL removal / MuSGD; nano ~40.9 mAP, ~38.9 ms vs
  56.1 ms CPU; "up to ~43%" CPU speedup). `docs.ultralytics.com`,
  `ultralytics.com/blog`.
- LearnOpenCV — YOLO26 NMS-free inference explainer (release date, CPU latency
  figures).
- arXiv 2509.25164 — YOLO26 architecture and benchmarking write-up.
- Ultralytics licensing — AGPL-3.0 and Enterprise licence terms.
- Roboflow — RF-DETR blog and repo (Apache 2.0 core / PML 1.0 Plus; DINOv2
  backbone; data efficiency; COCO/RF100-VL results; ICLR 2026). `blog.roboflow.com/rf-detr`,
  `github.com/roboflow/rf-detr`, `rfdetr.roboflow.com`.
- Google Developers Blog — "LiteRT: The Universal Framework for On-Device AI"
  (28 Jan 2026; NPU production, ~1.4× GPU vs TFLite, multi-framework).
- Android Developers — NNAPI migration guidance (deprecation, steer to LiteRT
  delegates); LiteRT GPU delegate documentation.
- Tencent NCNN; ONNX Runtime Mobile — alternative runtimes.

> Model versions and benchmark numbers move quarterly. Re-verify before relying
> on any specific figure in a build decision.
