# Verification Report — Cloud Connector milestone

What was actually run in the development environment for this milestone, with
exact results. Anything not listed here was not verified.

Environment: Linux x86_64, Go 1.24.7 (module toolchain go1.25.0), JDK 17
(Temurin/OpenJDK 17.0.20) + JDK 21, Android SDK platform 35 / build-tools
35.0.0, Docker 29.4, PostgreSQL 16 (container).

## Go — cloud-connector

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l .` | clean (no files) |
| Static analysis | `go vet ./...` | clean |
| Unit tests | `go test ./...` | **93 tests, all pass** (8 packages); Postgres integration tests skipped without env |
| Unit tests, race detector | `go test -race -count=1 ./...` | all pass |
| Postgres integration tests | `TEST_DATABASE_URL=… go test ./internal/storage/ -run TestIntegration` against PostgreSQL 16 | **8/8 pass** (migrations, upsert stability, event idempotency, webhook dedup, command lifecycle + SQL transition guards + `SKIP LOCKED`, idempotency key, expiry, token encryption-at-rest) |
| Build | `go build ./...` | success |
| Docker image | `docker build` (multi-stage, `FROM scratch`) | success¹ |
| Proto lint | `buf lint` | clean |

¹ In this sandboxed build environment the module download step needed the
sandbox's proxy CA injected as an extra build layer; the committed
`Dockerfile` is the clean version and is what CI builds.

## Live end-to-end (connector + PostgreSQL + providersim, via Docker)

Verified by hand against the running system:

- `/healthz` → ok, `/readyz` → ready (and readiness fails while DB is down —
  startup waited for Postgres).
- AlphaSense polling with real OAuth2 against the simulator (90 s / 8 s token
  TTLs): initial grant + refresh path exercised; polled alerts normalized and
  deduplicated across overlapping poll windows (`events_duplicate_total`
  increasing, each alert stored once).
- BetaGrid signed webhooks accepted; deliberate duplicate deliveries answered
  200 and counted (`webhook_duplicates_total` = 2) while processed once.
- Edge API: telemetry 202; event 202 `duplicate:false` then 202
  `duplicate:true` on redelivery; unauthenticated request → 401.
- gRPC (`guardian.v1`): ListDevices (5 devices across 3 sources with
  normalized status), ListRecentEvents (normalized types/confidences),
  SendCommand idempotency (same key → same command id), command to a
  BetaGrid device rejected with InvalidArgument.
- Command lifecycle: PENDING → poll (DELIVERED) → ack (ACKNOWLEDGED) →
  second ack → 409.
- `/metrics`: events/provider/webhook/telemetry/command series present with
  correct values.

## Android / Kotlin

| Check | Command | Result |
|---|---|---|
| JVM unit tests | `./gradlew :core:model:test :core:detection:test :core:cloud:test` | **61 tests, 0 failures** (49 pre-existing `:core:detection` — untouched and still green — plus 12 new `:core:cloud`) |
| Offline APK | `./gradlew :app:assembleOfflineDebug` | BUILD SUCCESSFUL → `app-offline-debug.apk` (≈34.3 MB) |
| Connected APK | `./gradlew :app:assembleConnectedDebug -Pguardian.cloud.baseUrl=… -Pguardian.cloud.apiToken=…` | BUILD SUCCESSFUL → `app-connected-debug.apk` |
| Offline guarantee | `aapt d permissions` on both APKs | offline: **no INTERNET permission**; connected: INTERNET present |

Three pre-existing compile errors in the baseline commit (`15b48e7`) had to
be fixed before any APK could build at all — they are baseline bugs, not
side effects of this milestone: missing `tensorflow-lite-gpu-api` on the
firesmoke compile classpath; a `drawText(color=…)` argument that doesn't
exist on that overload; `Icons.Filled.Stop` absent from
`material-icons-core`. Fixes are minimal and commented at the call sites.

## Not verified here (and therefore not claimed)

- Android instrumentation / on-device behavior (no emulator/KVM in this
  environment; no instrumentation tests exist in this repository).
- Kubernetes manifests applied to a live cluster (validated for structure
  only).
- `docker compose up` as a single command was not run verbatim; the
  equivalent three-container topology it describes (same images, env, and
  network) is what the live end-to-end section above verified.
- Load/performance characteristics.
- The GitHub Actions workflow has not executed on GitHub yet; its steps are
  the same commands run here.
