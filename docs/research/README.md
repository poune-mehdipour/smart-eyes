# Guardian — Research

Technical research backing the Guardian build. These documents exist to make the
engineering decisions *auditable*: every major choice in the codebase should trace
back to a justification here, and every justification should be honest about what
is proven versus what is still a bet.

Read in this order:

1. **[architecture.md](architecture.md)** — why the frame-source seam and the
   pure-Kotlin engine are the load-bearing decisions, and where the pipeline
   deliberately stops in Milestone 1.
2. **[models.md](models.md)** — the detector-model landscape (YOLO26, YOLO12,
   RF-DETR), on-device runtimes, and the licensing trap that has to be resolved
   *before* a model is picked.
3. **[datasets.md](datasets.md)** — the fire/smoke datasets that exist, what
   their negatives buy you, and why data — not code — is the schedule risk.
4. **[benchmarking.md](benchmarking.md)** — how success is measured. The metric
   that decides whether this product is viable is false-positive rate on *your*
   footage, not mAP on someone else's.
5. **[roadmap.md](roadmap.md)** — the milestone sequence, the spike that comes
   before any of it, and what is deliberately deferred.
6. **[risks.md](risks.md)** — technical, product, legal (GDPR / EU AI Act), and
   commercial risks, with the ones that can kill the product flagged.
7. **[DECISION.md](DECISION.md)** — the recommended stack in one place, with a
   one-line justification for every major choice and the open questions that
   remain.

## Ground rules these documents follow

- **No prediction.** Guardian reports hazards that are happening now. Multi-frame
  confirmation is a debounce, not a forecast.
- **Confidence is a health signal, not a probability.** A raw model score is not
  a calibrated likelihood; nothing surfaces a "%" until it has been calibrated.
- **The MVP is fire + smoke + camera-tampering.** Fall and zone-intrusion are a
  different, harder difficulty class and are deferred on purpose.
- **Alert delivery is a server concern, not a device concern.** Google Play
  policy blocks device-side SMS for an app like this; the backend is core IP from
  day one, not a "later" afterthought.

## A note on sources

Model and regulatory facts move fast. Figures were checked against primary
sources in **July 2026** (release blogs, dataset papers, the EU AI Act text and
official guidance). Each document ends with its references. Re-verify model
versions and AI Act enforcement dates before they are relied on in a commit —
they are moving targets, and this document says so wherever that is true.
