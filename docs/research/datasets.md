# Datasets

The blunt version: **data, not code, is the schedule risk for this product.** The
CameraX + inference loop is a weekend. A fire/smoke model that does not fire on
sunsets, red/orange objects, brake lights, reflections, and kettle steam in *your*
target environments is months. This document is about closing that gap.

## Why the negatives are the whole game

A fire/smoke detector's real enemy is the **false positive**. The failure mode
that kills the product is not "missed a fire" (rare, dramatic, forgivable in a v1
with human confirmation) — it is "screamed FIRE at a sunset for the tenth time
today," after which the user disables it and it protects nobody. That means the
value of a dataset is dominated by how many hard *negatives* (confusers) it
contains, not just how many fire images.

Keep that lens while reading the options below.

## Public datasets worth using

### D-Fire — the pragmatic starting point

- **Scale:** 21,527 RGB images, 26,557 bounding boxes (14,692 fire, 11,865
  smoke), at 416×416.
- **Composition:** only-fire 1,164 · only-smoke 5,867 · fire+smoke 4,658 · **none
  (negatives) 9,838**. Nearly half the dataset is deliberate confusers — objects
  and scenes that look like fire/smoke but are not. For a false-positive-bound
  problem, that negative half is the most valuable part.
- **Source mix:** includes surveillance-camera footage (state parks, university
  campuses) plus web-sourced imagery, giving intra-class variety in weather,
  lighting, and distance.
- **Format:** YOLO-format labels, so it drops straight into a YOLO/RF-DETR
  training pipeline.
- **Repo:** `gaiasd/DFireDataset` (GAIA, UFMG).

Why it is the starting point: it is right-sized to fine-tune on quickly, it is
surveillance-flavoured (close to the CCTV target), and its negative half directly
attacks the FP problem.

### FASDD — scale-up when D-Fire's ceiling is hit

- **Scale:** 100,000-level (120,000+) flame and smoke images — the largest
  open fire/smoke detection set.
- **Composition:** wide variation in resolution, day/night, indoor/outdoor,
  near/far, viewing angle, and platform (surveillance cameras, drones, satellites)
  — and notably many small-object instances, which stress small-target detection.
- **Format:** annotations in YOLO, VOC, COCO, and TDML; sub-datasets split by
  platform (FASDD_CV for camera/UAV, FASDD_RS for remote sensing). For this
  project, **FASDD_CV** is the relevant slice — ignore the satellite/RS portion.
- **Availability:** open-access via Science Data Bank.

Why later, not first: it is large enough that it is a "scale up accuracy and
generalization" move once a D-Fire-trained baseline exists and its limits are
understood. Use the camera slice; the remote-sensing imagery is off-domain for a
phone/CCTV product.

### Others in the space (context, not core)

Smaller or narrower sets exist — Domestic-Fire-and-Smoke (~5k, indoor-flavoured,
COCO/VOC/YOLO), the AI-For-Mankind / HPWREN wildfire-smoke sets (hundreds→~2k,
CC BY-NC-SA, wildfire-specific), FIgLib/SmokeyNet (wildland smoke sequences),
Kaggle fire/smoke collections of varying quality. Most are wildfire-oriented or
small; they are supplements for specific gaps (e.g. night smoke), not the base.

## The dataset you actually need: your own

Public datasets get a baseline off the ground and prove feasibility. They do **not**
match the visual domain your app will run in — the specific rooms, cameras,
lighting, and objects of your target users. The domain gap between "internet fire
images" and "this kitchen's phone camera at dusk" is exactly where false positives
live.

So the real dataset strategy is a **data flywheel**, and it is a product feature,
not an afterthought:

1. **Bootstrap** on D-Fire (then FASDD_CV) to get a working baseline.
2. **Collect from the field** — every confirmed event and, more importantly, every
   *false* alarm gets its frames captured (with consent, minimised — see
   [risks.md](risks.md)) and routed to the backend.
3. **Curate & re-label** the hard cases, weighting the confusers that caused false
   positives.
4. **Retrain / fine-tune** and push an updated model.

This loop is where the durable IP is. It also explains why the backend exists from
the first shipping alert (see [architecture.md](architecture.md) and
[roadmap.md](roadmap.md)): the false-alarm capture pipeline lives there. Note the
"self-learning A1 model" idea from the original brief should be read as *this
server-side retrain flywheel*, **not** on-device online learning — unsupervised
on-device weight updates on a safety-critical detector are a way to silently
destroy a model in the field, and are out of scope.

## Licensing caveat — check before shipping weights

Dataset licences govern what you may do with models trained on them, especially
for a commercial product and especially if you distribute weights. Terms vary:
some public sets are research-only, some are CC BY(-NC), some require attribution.
Two concrete cautions:

- **Non-commercial (NC) licences** (e.g. the HPWREN-derived wildfire sets are
  CC BY-NC-SA) can prohibit use in a sold product — including, arguably, the
  weights trained on them. Do not fold NC-licensed data into the commercial
  training set without checking.
- **Confirm D-Fire and FASDD terms** against their repositories/papers before
  commercial training and before any weight distribution. This document does not
  assert their exact licences because they must be verified from source, not
  remembered — treat that verification as a required task, not a formality.

## References

- D-Fire — `github.com/gaiasd/DFireDataset`; dataset description and counts
  (21,527 images; 26,557 boxes; 9,838 negatives; surveillance + web sources).
- FASDD — "An Open-access 100,000-level Flame and Smoke Detection Dataset"
  (ESSD / Science Data Bank; FASDD_CV vs FASDD_RS split; YOLO/VOC/COCO/TDML).
- `robmarkcole/fire-detection-from-images` — survey of public fire/smoke datasets
  (Domestic-Fire-and-Smoke, Kaggle sets, etc.).
- `aiformankind/wildfire-smoke-dataset` — HPWREN-derived smoke set (CC BY-NC-SA;
  the NC caveat above).

> Verify every dataset licence from its own repository before commercial training
> or weight distribution. Counts above are from the dataset papers/repos as of
> mid-2026.
