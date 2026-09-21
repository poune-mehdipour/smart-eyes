# Interview Documentation — Guardian

Study notes for walking this project through a technical interview. Simple
English, technically accurate, and honest about scope: everything claimed
here exists in this repository, and simulated things are called simulated.

## 30-second explanation

> Guardian is a fire/smoke detector that runs entirely on an Android phone —
> camera in, TensorFlow Lite inference, multi-frame confirmation, local alarm
> out, no internet needed. I then extended it into an IoT platform: a Go
> cloud connector receives telemetry and confirmed hazard events from
> devices, integrates two (simulated) third-party providers with completely
> different APIs — one OAuth2 + REST polling, one HMAC-signed webhooks —
> normalizes everything into one domain model in PostgreSQL, and exposes a
> small gRPC API plus a safe command channel back to devices. The design
> rule that shapes everything: local detection must never depend on cloud
> availability.

## 2-minute explanation

Add to the above, in roughly this order:

1. **Edge:** the Android app was already layered (frame source → detection
   engine → event confirmation → alert) with the pure-Kotlin parts free of
   Android dependencies. I added `:core:cloud`, a pure-JVM module with the
   wire contract and a queueing reporter: bounded queue, background worker,
   exponential backoff with jitter, drop-oldest under overflow. Reporting is
   fire-and-forget — the local alarm has already fired before the cloud is
   even involved. Two product flavors make the guarantee structural: the
   `offline` flavor has no INTERNET permission in its manifest at all.
2. **Connector:** Go, standard library first. Devices self-register on first
   contact. Every hazard event — from the edge API, from polling, from
   webhooks — flows through one pipeline: validate → resolve device →
   persist idempotently → publish to stream subscribers → count in metrics.
3. **Providers:** the provider seam is small capability interfaces; each
   adapter keeps its provider's weirdness (score scales, vocabularies,
   timestamp formats, auth) entirely to itself.
4. **Reliability:** idempotency is enforced by Postgres unique constraints,
   so retries are always safe; retries themselves are centralized with
   permanent-vs-transient classification. The command channel is a strict
   state machine (PENDING → DELIVERED → ACKNOWLEDGED/FAILED) whose
   transitions are guarded in SQL.
5. **Operations:** structured JSON logs with request IDs, Prometheus
   metrics, health/readiness endpoints, Docker Compose for a full local
   environment (with a provider simulator so OAuth refresh and duplicate
   webhooks actually happen), Kubernetes manifests, and CI that runs
   format/vet/tests (including integration tests against a real Postgres)
   plus the Android JVM tests.

## Architecture walkthrough

Follow the data:

1. Camera frame → `DetectionEngine` (TFLite YOLOv8, decode, NMS) → raw
   `Detection`s → `SlidingWindowEventConfirmationEngine` debounces across
   frames → confirmed `SecurityEvent` → `AlertController` (vibrate, notify,
   tone). All on-device, all offline. *(pre-existing, untouched)*
2. The confirmed event is also handed to `CloudReporter.reportEvent()` —
   non-blocking enqueue. The worker serializes it (`CloudContract`) and
   POSTs `/v1/edge/events` with a bearer token; transient failures back off
   and retry, 4xx drops (it can't get better), overflow drops oldest.
3. The connector authenticates the request, upserts the device, and runs the
   event pipeline. The dedup key is the UUID minted on the phone at
   confirmation time, so a retried POST after a lost response inserts
   nothing the second time — the response says `"duplicate": true`.
4. Meanwhile the AlphaSense poller fetches `/api/v2/units` and
   `/api/v2/alerts?since=…` with an OAuth2 bearer token (the token manager
   refreshes early and single-flights), and BetaGrid webhooks arrive signed
   with HMAC-SHA256 — verify → parse → timestamp window → dedup → same
   pipeline. Different score systems and vocabularies are normalized in the
   adapters (0–100 → 0–1; low/medium/high → 0.25/0.5/0.9, a documented
   convention; FLAME → FIRE).
5. Downstream services read via gRPC (`ListRecentEvents`, `StreamEvents` —
   a live feed fanned out from the pipeline) and act via `SendCommand`.
   Guardian devices poll for commands and ack; the AlphaSense adapter pushes
   commands, translating vocabulary (`START_MONITORING` → `"arm"`);
   BetaGrid deliberately has no command capability, and the service rejects
   commands to its devices at creation time with a clear error.

## Why Go?

- The connector's job is concurrent I/O: HTTP servers, pollers, a gRPC
  stream, background reapers. Goroutines + `context` fit exactly, and
  cancellation propagates through every layer (tested).
- Static single binary → a `FROM scratch` container (~15 MB) with no shell.
- The standard library covers routing (1.22+ method patterns), JSON,
  crypto/HMAC/AES-GCM, and structured logging (`slog`), so the dependency
  list stays five entries long — each one defensible: pgx, uuid, prometheus,
  grpc, protobuf.
- Explicit error values make the transient/permanent classification a type
  (`reliability.Permanent`), not a comment.

## Why provider abstraction?

Because "integrate many vendors' clouds" is the actual product problem, and
without a seam every vendor quirk metastasizes through the codebase. The
interface is deliberately split by capability (`DevicePoller`,
`EventPoller`, `CommandSender`): a webhook provider has nothing to poll, and
pretending otherwise means stub methods that lie. Type-asserting for
capabilities keeps the polling loop honest, and the command service can
refuse impossible requests up front. Adding a third provider touches: one
new package + registration in `main.go`. Nothing else.

## How OAuth works here

`internal/oauth.Manager`, hand-rolled deliberately so the lifecycle is
visible and testable:

- First call does a `client_credentials` grant; the simulated IdP returns
  access + refresh tokens and `expires_in`.
- Tokens are cached; a 30-second skew means we refresh *before* expiry, so
  no request is sent with a token about to die mid-flight.
- The mutex is held across the refresh network call on purpose: concurrent
  callers share one refresh (single-flight) instead of stampeding the IdP.
- Refresh uses `grant_type=refresh_token`; if the IdP answers
  `invalid_grant` (dead refresh token), the manager falls back to a full
  re-auth instead of failing forever.
- If the provider 401s a request despite a "valid" token (server-side
  revocation), the adapter invalidates the cache and retries once with a
  fresh token; a second 401 is permanent.
- 4xx at the token endpoint is permanent (no retry); 5xx/transport errors
  retry with backoff. Secrets never appear in logs — only the OAuth error
  code does.

## How webhooks work here

Order matters and is security-motivated (`internal/webhook`):

1. Bounded read (1 MiB default) — oversize rejected before allocation grows.
2. HMAC-SHA256 signature over the raw body, constant-time compare —
   unauthenticated bytes never reach a parser.
3. Parse + strict validation — unknown kind/hazard/certainty, missing IDs or
   timestamps are rejected; because they were *authentic*, they're stored in
   a dead-letter table with the reason. Never silently accepted.
4. Timestamp window (±5 min) bounds replay of captured signed payloads.
5. Dedup: `INSERT … ON CONFLICT DO NOTHING` on (provider, delivery_id);
   a duplicate answers **200**, because answering an error to a retrying
   sender means retrying forever.
6. Only then: hazard → event pipeline; heartbeat → device status.

## How idempotency works

Three layers, all backed by the database rather than memory (so restarts and
replicas don't break it):

- **Edge events:** dedup key = event UUID minted on the phone at
  confirmation; unique `(source, dedup_key)`.
- **Webhooks:** dedup key = provider's delivery ID; a deliveries table is
  the gate for all kinds (hazard + heartbeat), plus the same event
  constraint behind it.
- **Commands:** callers pass an idempotency key; a partial unique index
  `(device_id, idempotency_key) WHERE key <> ''` makes a retried
  `SendCommand` return the original command instead of enqueueing twice.

Duplicates are metrics (`events_duplicate_total`, `webhook_duplicates_total`),
not errors.

## Retry strategy

One implementation (`internal/reliability.Do`): exponential backoff with
**full jitter** — the delay is uniform in [0, min(base·2ⁿ, max)] — so a herd
of failing callers doesn't resynchronize into waves. Errors are classified:
`Permanent` wraps things retrying can't fix (4xx, validation, bad refresh
token) and stops immediately; everything else retries up to the policy
limit; `context` cancellation interrupts even mid-sleep. The edge (Kotlin)
side implements the same shape independently for its queue.

## What happens when a provider is down?

The poll cycle fails after its in-adapter retries, increments
`provider_request_errors_total`, logs with detail, **keeps its watermark**,
and waits for the next tick. Because the next cycle re-covers the window
(watermark + 1-minute overlap) and events are idempotent, an outage delays
that provider's events and loses nothing else. Other providers, edge
ingest, gRPC — all unaffected. Webhook providers retry on their side; dedup
makes their redelivery safe.

## What happens when the cloud connector is down?

The safety-relevant answer: **nothing changes on the device.** Detection,
confirmation, and the local alarm are structurally independent of the
network (the offline flavor cannot even open a socket). The connected
flavor's reporter queues events (64 slots), retries with backoff, drops
oldest under overflow, and loses the queue if the process dies —
documented; the connector's idempotency is what makes the retries safe.
On restart the connector recovers all state from Postgres and applies any
pending migrations before serving; readiness gates traffic until the DB is
reachable.

## Why PostgreSQL?

The hard problems here are relational-integrity problems: "exactly once"
expressed as unique constraints, a command state machine whose illegal
transitions must be impossible even under concurrency (`UPDATE … WHERE
state = ANY($legal)` — zero rows affected *is* the error), and exactly-once
claiming for concurrent device polls (`FOR UPDATE SKIP LOCKED`). One
Postgres does all of that with ACID and no extra infrastructure. A queue or
a document store would each add operational surface while solving a problem
this scale doesn't have.

## Why gRPC?

The connector-to-platform boundary is service-to-service: typed contracts,
codegen for any consumer language, streaming for the live event feed, and a
schema (`guardian.v1`) with explicit versioning discipline — additive
changes in v1, breaking changes mean v2. JSON/REST stays where it belongs:
at the device and webhook edge, where debuggability and proxy-friendliness
win.

## How Docker is used

A multi-stage build: `golang:alpine` compiles static binaries (CGO off),
the runtime image is `FROM scratch` + CA certs — no shell, no package
manager, runs as a non-root numeric user. `docker compose up` gives the
whole development environment: Postgres (with healthcheck), connector, and
the provider simulator wired to exercise OAuth refresh (90-second tokens)
and duplicate webhooks continuously.

## How Kubernetes is used

`deploy/k8s/connector.yaml`: Deployment + Service + ConfigMap, secrets only
as `secretRef` (created out-of-band, never committed), `readinessProbe` on
`/readyz` (DB reachability — keeps traffic away, doesn't restart),
`livenessProbe` on `/healthz` (process health — restarts), resource
requests/limits, non-root + read-only root filesystem + dropped
capabilities, Prometheus scrape annotations. Honest caveats I say out loud:
single replica (the poller isn't leader-elected), and the manifests are
provided and reviewable but weren't applied to a live cluster as part of
this repo.

## Observability strategy

- **Logs:** `slog` JSON to stdout. Every HTTP request gets a correlation ID
  (accepted from `X-Request-ID` or minted), which flows through context into
  every log line of that request — grep one ID, see one story. No secrets
  ever logged.
- **Metrics:** Prometheus counters/histograms with bounded label
  cardinality (static route labels, never raw URLs): events
  received/processed/failed/duplicate by source, provider requests/errors,
  OAuth refresh outcomes, webhook rejects/duplicates, command transitions,
  request latency histogram.
- **Health:** liveness (`/healthz`) is "the process serves"; readiness
  (`/readyz`) is "the database answers" — different questions, different
  probes.
- Tracing: not implemented; correlation IDs cover single-service debugging,
  and OpenTelemetry is listed as follow-up rather than pretended.

## Testing strategy

Test the behavior, fake the boundary, and verify the database bits against
the real database:

- Pure logic (normalization, retry, state machine, crypto, JSON contract)
  → table-driven unit tests.
- Protocol behavior (OAuth lifecycle, provider HTTP handling, webhook
  ordering, edge API) → `httptest` servers and scriptable fakes; assertions
  on *observable* behavior (how many grants, which grant types, exact
  states) rather than internals.
- Storage semantics that mocks can't prove (constraints, SQL guards,
  `SKIP LOCKED`, encryption at rest) → integration tests against real
  Postgres, skipped without `TEST_DATABASE_URL`, provided in CI as a
  service container.
- Kotlin mirrors the same philosophy: contract tests pin the exact wire
  fields the Go side parses; reporter tests script a fake gateway.
- Deliberately not: mocking the database in unit tests and calling that
  integration coverage.

## Security decisions

- No committed credentials anywhere; `.env.example` documents every knob;
  compose defaults are dev-only and only address the compose network.
- Webhook HMAC verification before parsing, constant-time comparison,
  timestamp window against replay.
- Edge API bearer auth with constant-time compare; empty token = loud
  startup warning, dev only.
- Provider OAuth tokens AES-256-GCM encrypted at rest; key from env
  (KMS-shaped split: ciphertext in DB, key elsewhere).
- All SQL parameterized via pgx; no string-built statements.
- Request body size caps, header/read/write timeouts, per-request timeout
  middleware.
- Trust boundaries documented: devices are semi-trusted (shared token —
  per-device credentials are listed future work), providers are untrusted
  until verified (signature/OAuth), downstream gRPC is internal-network
  only (no auth on it yet — stated, not hidden).
- Commands are a fixed safe vocabulary; nothing physical is remotely
  actuatable, and the device remains the authority on execution.

## Biggest technical tradeoffs

1. **Polling (not push) for device commands** — a phone that must survive
   offline can't hold a broker connection; polling is trivially resumable
   and idempotent. Cost: command latency = poll interval. MQTT is the
   documented upgrade path.
2. **In-memory edge outbox** — no persistence dependency in the reporting
   path; cost: events lost if the process dies mid-outage. Chosen because
   the safety function (local alarm) is unaffected; a Room outbox is next.
3. **Hand-rolled OAuth manager and migrations runner** vs libraries — more
   code I own, but the whole point of the exercise is demonstrating the
   lifecycle; both are small and fully tested. In a team product I'd weigh
   `golang.org/x/oauth2` and `golang-migrate` seriously.
4. **One Postgres for everything** (events, commands, dedup, dead letters)
   — no broker to operate; cost: at high scale the events table becomes the
   pressure point (see below).
5. **Two intentionally different simulated providers** instead of one real
   one — real vendor APIs need accounts/credentials a portfolio can't ship;
   simulation lets failure modes (token expiry, duplicates, malformed
   payloads) be *provoked on demand* and tested. The tradeoff is honesty,
   handled by labeling.

## What I would change at 20,000 devices

Rough shape: 20 k devices at 30-second telemetry ≈ ~700 writes/s telemetry
plus event traffic — a single Postgres with the current row-per-snapshot
telemetry table starts to hurt first.

- Batch or sample telemetry writes; partition `telemetry` and
  `hazard_events` by time; aggressive retention policy.
- Multiple connector replicas behind the LB (ingest is already safe:
  idempotency in DB) + leader election (e.g. Postgres advisory locks) for
  the pollers and reaper.
- Per-device credentials (issued at registration) instead of the shared
  token; rate limiting per device.
- Connection pooling discipline (pgbouncer) and read replicas for the gRPC
  read paths.
- Command polling gets jitter and adaptive intervals to spread load.

## What I would change at 100,000 devices

- Ingest and processing split: an actual queue (e.g. Kafka/NATS) between
  the HTTP edge and the pipeline; the dedup/idempotency design already maps
  onto consumer-side dedup keyed the same way.
- Telemetry to a time-series store (or Postgres+Timescale); Postgres keeps
  devices, commands, provider connections — the transactional core.
- Device transport moves to MQTT/WebSocket with a broker tier for commands;
  the poll API remains as fallback.
- Horizontal sharding of pollers by provider connection; StreamEvents moves
  to the queue's fan-out instead of in-process pub/sub.
- An actual gateway with per-device authn/z (mTLS or token service).

## What I intentionally did NOT build

- A message broker, service mesh, or microservice fleet — one service, one
  database is the right size for the problem and for explaining every line.
- OpenTelemetry, admin UI, multi-tenancy, per-device credential issuance.
- A fake "deploy to AWS" pipeline requiring credentials that don't exist.
- Mock detections on the phone, or a bundled model whose license forbids it.
- Android-side command execution (the cloud half exists and is verified;
  the app half is future work, stated in the README).

## 20 likely interview questions, with concise answers

1. **Why doesn't cloud failure break detection?** Reporting is downstream
   of the local alarm and fire-and-forget; the offline flavor has no
   INTERNET permission at all, so independence is structural, not a promise.
2. **Walk me through a webhook delivery.** Bounded read → HMAC verify
   (constant-time) → parse/validate (fail → dead-letter + 400) → timestamp
   window → DB dedup gate (duplicate → 200 + metric) → normalize → insert
   (unique constraint) → publish → 200.
3. **Why 200 for duplicates?** The sender retries until success; an error
   response to a duplicate means infinite redelivery. The duplicate *is*
   successfully processed — by having been processed before.
4. **How do you know an event isn't processed twice?** The dedup key is
   part of a unique constraint; `ON CONFLICT DO NOTHING` + rows-affected
   tells me it was a duplicate. Database-level, so it survives restarts and
   replicas.
5. **What if the OAuth refresh token is revoked?** `invalid_grant` is
   permanent → the manager falls back to a fresh `client_credentials`
   grant. If even that fails permanently, polling errors are logged/counted
   and the next cycle retries — no crash, no wedge.
6. **Why hold the mutex across the refresh HTTP call?** That's the
   single-flight: 20 concurrent callers produce one grant request (tested).
   The cost — brief serialization on refresh — is the point.
7. **Race: two devices poll commands at once?** `UPDATE … WHERE id IN
   (SELECT … FOR UPDATE SKIP LOCKED) RETURNING` — each command claimed
   exactly once; verified against real Postgres.
8. **Why is the command state machine in SQL and not Go?** It's in both
   (`domain.CanTransition` documents it; the SQL `WHERE state = ANY(…)`
   enforces it) — but enforcement has to live where concurrency is
   resolved, and that's the database.
9. **Why polling for commands instead of push?** Offline-first devices;
   resumable; no broker to operate; idempotent. Latency tradeoff accepted
   and documented; MQTT is the upgrade path.
10. **What's in your retry policy and why jitter?** Exponential backoff,
    full jitter (uniform in [0, ceiling]): after a shared outage, plain
    exponential backoff makes all clients return in synchronized waves;
    jitter spreads them.
11. **How do you avoid retrying forever?** Error classification: 4xx and
    validation failures wrap as `Permanent` and stop immediately; transient
    errors have an attempt budget; context cancellation aborts even
    mid-backoff.
12. **Where could you still lose data?** The edge in-memory queue (process
    death during an outage) and events dropped after the retry budget —
    both documented; neither affects local alerting. Everything cloud-side
    is durable in Postgres.
13. **Why not Kafka?** At this scale a queue adds an operational system to
    run, monitor, and explain, for reliability Postgres constraints already
    give me. At 100 k devices the answer changes, and the dedup design
    ports over.
14. **How are provider tokens protected?** AES-256-GCM at rest, random
    nonce per encryption, key from the environment (KMS-shaped split); a DB
    dump alone yields nothing. Verified by an integration test that reads
    the raw column.
15. **What does /readyz check that /healthz doesn't?** Readiness = "can I
    do useful work" (DB ping) and gates traffic; liveness = "is the process
    alive" and gates restarts. Coupling them makes K8s restart you for your
    database's problems.
16. **How would a third provider be added?** New package implementing the
    capability interfaces it genuinely has, its own wire types and
    normalization + tests, registration in `main.go`. The domain model,
    pipeline, storage, gRPC are untouched — same additive property as the
    Android detector seam.
17. **Why both REST and gRPC?** Different audiences: devices/webhooks need
    simple debuggable HTTP; internal services get typed, versioned,
    streaming contracts. One boundary each.
18. **What breaks first under load?** Telemetry writes (row per snapshot
    per device). Mitigations in order: batching/sampling, time partitioning,
    retention, then a time-series store.
19. **Is the confidence score a probability?** No — deliberately not. Edge
    scores are uncalibrated model scores; BetaGrid's categorical certainty
    maps to documented convention values (0.25/0.5/0.9). Normalized range,
    not calibrated meaning; calibration is a prerequisite for any
    auto-escalation.
20. **What are you least happy with?** The shared edge token (per-device
    credentials are the real answer), the in-memory outbox, and the fact
    that the poller assumes one replica. All three are stated limitations
    with designed upgrade paths — I'd rather own them than hide them.
