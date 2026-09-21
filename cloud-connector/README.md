# Guardian Cloud Connector

The Go backend of the Guardian project. Full documentation — architecture,
decisions, failure handling, run instructions, honest limitations — lives in
the [repository README](../README.md); this file is a map of the tree.

```
cmd/connector/        the service: HTTP ingest + webhooks + gRPC + pollers
cmd/providersim/      simulated external providers (fake OAuth2 IdP + REST
                      API for "AlphaSense"; signed-webhook pusher for
                      "BetaGrid") — a test fixture, not a product component
internal/
  domain/             the internal model; no provider wire formats allowed in
  config/             env-only configuration (every knob documented in .env.example)
  ingest/             REST API Guardian edge devices call (contract mirrored
                      by the Android :core:cloud module)
  provider/           capability interfaces + poll loop
  provider/alphasense simulated OAuth2 + REST-polling adapter
  provider/betagrid   simulated HMAC-webhook adapter
  oauth/              OAuth2 token lifecycle (refresh, single-flight, recovery)
  webhook/            signature → validate → window → dedup → process
  reliability/        the one retry implementation (backoff + full jitter)
  events/             the one event pipeline (validate → persist → publish)
  commands/           command state machine + dispatch + reaper
  storage/            pgx Postgres access; embedded migrations; token crypto
  observability/      slog JSON logs, request IDs, Prometheus, health/ready
  grpcapi/            guardian.v1.ConnectorService implementation
api/proto/            protobuf source (buf-linted); generated code in api/gen
migrations/           SQL schema, applied automatically at startup
deploy/k8s/           Kubernetes manifests
```

Quick start: `docker compose up --build` (see the root README for what to
poke at afterwards). Tests: `go test ./...` — the Postgres integration tests
in `internal/storage` run only when `TEST_DATABASE_URL` is set.
