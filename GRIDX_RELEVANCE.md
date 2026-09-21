# Skill Relevance Map

This project is not branded for, affiliated with, or derived from any
company's systems. It is a self-contained IoT/edge + cloud integration
project. This document maps the competencies a Cloud Connector / IoT
backend role typically asks for to the **specific code in this repository**
that demonstrates them — only things that actually exist here are listed,
and simulated components are labeled as such.

| Skill | Where it is demonstrated (files/packages) | Notes |
|---|---|---|
| Go (idiomatic, stdlib-first) | `cloud-connector/` — whole service; e.g. `internal/reliability/retry.go`, `internal/events/events.go`, `cmd/connector/main.go` | 5 direct dependencies; context-driven cancellation throughout; table-driven tests |
| REST API design | `internal/ingest/ingest.go` (edge API: telemetry, events, command poll/ack) | Bounded bodies, bearer auth, correct status codes (202/400/401/409/413), documented wire contract |
| OAuth2 (access/refresh lifecycle) | `internal/oauth/oauth.go` + `oauth_test.go` | Expiry skew, single-flight refresh, `invalid_grant` recovery, 401-revocation handling; fully tested against a fake IdP |
| Webhooks | `internal/webhook/webhook.go`, `internal/provider/betagrid/betagrid.go` + tests | HMAC-SHA256 verification (constant-time), timestamp window, dedup, dead-lettering of malformed-but-authentic payloads |
| Provider integrations / normalization | `internal/provider/provider.go` (capability interfaces), `provider/alphasense/`, `provider/betagrid/` | Two **simulated** providers with intentionally conflicting schemas normalized into one domain model (`internal/domain/domain.go`) |
| Device telemetry | Contract: `core/cloud/.../CloudContract.kt` ↔ `internal/ingest/ingest.go`; storage: `migrations/0001_init.sql` (`telemetry`) | Mirrors real measured on-device metrics (latency, FPS, dropped frames); no personal data |
| Command delivery (cloud→edge) | `internal/commands/commands.go` + tests; poll/ack endpoints in `internal/ingest` | PENDING→DELIVERED→ACKNOWLEDGED/FAILED state machine, idempotency keys, TTL reaper, push-vs-poll per provider capability |
| PostgreSQL | `internal/storage/storage.go`, `migrations/0001_init.sql`, `storage_integration_test.go` | Embedded migrations, unique-constraint idempotency, SQL-guarded state transitions, `FOR UPDATE SKIP LOCKED`, partial unique indexes — verified against real Postgres |
| gRPC + Protocol Buffers | `api/proto/guardian/v1/connector.proto`, `internal/grpcapi/grpcapi.go` | Versioned package, server-streaming event feed, status-code mapping; buf lint clean |
| Docker | `cloud-connector/Dockerfile`, `docker-compose.yml` | Multi-stage → `FROM scratch`, non-root; compose runs the full system incl. provider simulator |
| Kubernetes | `cloud-connector/deploy/k8s/connector.yaml` | Deployment/Service/ConfigMap/secretRef, both probes, resources, hardened pod security; honestly labeled as not cluster-applied |
| CI/CD | `.github/workflows/ci.yml` | gofmt gate, vet, `go test -race` with a Postgres service container, build, Docker build, Android JVM tests; deliberately no fake deploy stage |
| Distributed failure handling | `internal/reliability/retry.go`, adapter error classification in `provider/alphasense`, poller watermark logic in `internal/provider/poller.go` | Backoff+jitter, permanent/transient split, 429/500/timeout/expired-token/duplicate/replay scenarios all tested |
| Structured logging | `internal/observability/observability.go` | slog JSON, per-request correlation IDs propagated via context |
| Metrics | `internal/observability/observability.go` (Metrics struct) | Prometheus counters/histograms: events, providers, OAuth, webhooks, commands, latency; bounded cardinality |
| IoT/edge systems thinking | `core/cloud/` (Kotlin), `app` flavors (`app/build.gradle.kts`, `app/src/connected/AndroidManifest.xml`), `feature/monitoring/.../MonitoringViewModel.kt` | Offline-first: no INTERNET permission in the offline flavor (verified via `aapt` on the built APK); cloud reporting is fire-and-forget with bounded queue + backoff |
| End-to-end ownership | `docs/cloud/IMPLEMENTATION_PLAN.md` → implementation → tests → `README.md` (incl. Limitations) | Audit → plan → domain design → implementation → 93 Go unit + 8 Postgres integration + 61 Kotlin JVM tests → containerization → docs |

## What this map does NOT claim

- No production users, customers, or deployments; no load/scale numbers.
- AlphaSense and BetaGrid are simulations built for this project
  (`cmd/providersim`), not commercial integrations.
- Kubernetes manifests are written and reviewable, not applied to a cluster.
- There are no Android instrumentation (on-device) tests in this repository;
  the JVM test suites listed above are what actually ran.
