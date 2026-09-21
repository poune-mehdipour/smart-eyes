# Cloud Connector — Implementation Plan

Written after auditing the repository at commit `15b48e7` (the only commit on
`main`). This plan is the contract for what gets built; anything that ends up
differing is corrected here or explicitly labeled in the final docs.

## Audit findings (what exists today)

- Android app, 7 leaf Gradle modules; the pipeline is
  `FrameSource → DetectionEngine → EventConfirmationEngine → AlertController`.
- `:core:model` and `:core:detection` are **pure Kotlin JVM** modules (no
  Android dependency) — this is the seam the cloud work must respect.
- Confirmed hazards are `SecurityEvent(id, type, confidence, firstSeenMs,
  confirmedAtMs, boundingBox)`; live pipeline health is `InferenceMetrics`
  (latency, FPS, dropped frames, detections, confirmed events).
- The app declares **no INTERNET permission**; the manifest comments say the
  detection app must run in Airplane Mode. This constraint is preserved for
  the default build (see Edge integration below).
- Existing JVM unit tests live in `:core:detection` (49 tests). They are not
  touched.

## What gets added

### 1. Go Cloud Connector (`cloud-connector/`)

Standard-library-first Go 1.24 service:

```
cloud-connector/
  cmd/connector/       — main: HTTP + gRPC servers, migrations, wiring
  cmd/providersim/     — simulated external providers (OAuth2 IdP + REST + webhook pusher)
  internal/
    domain/            — Device, TelemetryEvent, HazardEvent, Command, CommandResult,
                         ProviderConnection; no provider JSON leaks in here
    config/            — env-only configuration; .env.example, no secrets committed
    storage/           — pgx-based Postgres repositories; embedded SQL migrations
    ingest/            — REST API for Guardian edge devices (telemetry, events,
                         command poll + ack)
    provider/          — Provider interface + normalization contract
    provider/alphasense/ — simulated Provider A: OAuth2 (refresh flow) + REST polling
    provider/betagrid/   — simulated Provider B: HMAC-signed webhooks, different schema
    oauth/             — token manager: expiry, single-flight refresh, failure states
    webhook/           — signature check, timestamp window, dedup, malformed handling
    reliability/       — retry with exponential backoff + jitter, error classification
    commands/          — command service + PENDING/DELIVERED/ACKNOWLEDGED/FAILED machine
    events/            — event pipeline: validate → normalize → persist (idempotent) → publish
    observability/     — slog JSON logging, request IDs, Prometheus metrics, health/ready
    grpcapi/           — internal gRPC API (v1)
  api/proto/guardian/v1/ — protobuf source (generated code committed under gen/)
  migrations/          — numbered SQL migrations
  deploy/k8s/          — Deployment/Service/ConfigMap/Secret-ref manifests
  Dockerfile, docker-compose.yml, .env.example
```

Dependency budget (deliberate): `pgx/v5`, `google/uuid`,
`prometheus/client_golang`, `grpc` + `protobuf`, `golang.org/x/crypto` only if
needed. Router, logging, config, retries, migrations: standard library.

### 2. Edge → cloud integration (Kotlin)

- New `:core:cloud` **pure JVM** module: telemetry/event wire contract
  (mirrors the Go ingest API), a `CloudReporter` interface, a queueing
  reporter with bounded buffer + retry, an `HttpURLConnection` gateway.
  Unit-tested on the JVM.
- `:app` gains two product flavors: `offline` (identical to today: no
  INTERNET permission, no-op reporter) and `connected` (adds INTERNET
  permission in a flavor manifest, binds the real reporter). Local detection
  never blocks on, or fails because of, cloud delivery.

### 3. Delivery model decisions (made once, here)

- **Device ↔ cloud transport:** HTTPS REST + polling for commands. A phone
  app that must keep working offline has no business holding a broker
  connection; polling is simple, resumable, and explainable. MQTT is future
  work, documented as such.
- **Idempotency:** edge events carry a UUID minted at confirmation time;
  Postgres unique constraints + `ON CONFLICT DO NOTHING` make redelivery
  harmless. Webhook deliveries carry the provider's delivery ID, same
  mechanism. Duplicates are counted in metrics, not errors.
- **Provider tokens at rest:** AES-256-GCM encrypted with a key from the
  environment; never logged.
- **Dead letters:** events that fail terminal validation land in a
  `dead_letter_events` table with the reason — lightweight, queryable, no
  extra infrastructure.

## Verification gates

Every phase ends with `gofmt -l`, `go vet ./...`, `go test ./...` green.
Final gate: Docker image build, `docker compose up` smoke test against
Postgres, DB integration tests, Android JVM tests if a Gradle/AGP toolchain
is available in the build environment (documented either way).

## Out of scope (documented, not pretended)

MQTT transport, OpenTelemetry tracing, per-device credentials/mTLS,
horizontal scaling concerns, real provider integrations, cloud deployment.
