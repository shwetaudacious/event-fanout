# Project Details

Reference documentation for the Event Fanout Service.

## Project Metadata

| Field | Value |
|-------|-------|
| **Name** | Event Fanout Service |
| **Module path** | `github.com/event-fanout-service/event-fanout` |
| **GitHub** | [github.com/shwetaudacious/event-fanout](https://github.com/shwetaudacious/event-fanout) |
| **Version** | `0.1.0` |
| **License** | MIT |
| **Language** | Go 1.22 |

## Repository Layout

```
cmd/server/              HTTP API entrypoint
cmd/worker/              Queue consumer + webhook delivery
internal/config/         Environment configuration
internal/http/           REST handlers and routes
internal/delivery/       Webhook HTTP client
internal/worker/         Background processor loop
internal/service/        Event, subscription, fanout, matcher logic
internal/repository/     PostgreSQL data access
internal/queue/          Redis Streams queue (events:stream, consumer group fanout-workers)
internal/redisutil/      REDIS_URL parsing
migrations/              PostgreSQL schema
helm/eventfanout/        DOKS Helm chart
tests/integration/       End-to-end integration tests
.github/workflows/       CI test, image build, DOKS deploy
docs/                    Documentation
```

## Tech Stack

| Layer | Technology |
|-------|------------|
| HTTP | Gorilla Mux |
| Database | PostgreSQL 15 (pgx/v5) |
| Queue | Redis 7 (go-redis, list-based) |
| Logging | zap (structured JSON) |
| Containers | Multi-stage Docker build |
| Orchestration | Helm on DOKS |
| CI/CD | GitHub Actions |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | local postgres | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection URL |
| `SERVER_PORT` | `8080` | HTTP listen port |
| `SERVER_HOST` | `0.0.0.0` | HTTP bind address |
| `MAX_DELIVERY_RETRIES` | `5` | Max webhook retry attempts |
| `BASE_RETRY_DELAY_SECONDS` | `5` | Initial exponential backoff delay |
| `WEBHOOK_TIMEOUT_SECONDS` | `30` | Webhook HTTP timeout |
| `WEBHOOK_MAX_BODY_BYTES` | `1048576` | Max webhook response body read |
| `FANOUT_WORKER_POOL` | `10` | Retry batch size per poll |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `ENVIRONMENT` | `development` | Runtime environment label |

Docker Compose defaults: Postgres `postgres/postgres123`, DB `eventfanout`, ports `8080/5432/6379`.

## Data Model

### events

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `type` | VARCHAR | Event type (e.g. `user.created`) |
| `source` | VARCHAR | Originating service |
| `payload` | JSONB | Arbitrary event data |
| `created_at` | TIMESTAMPTZ | Ingestion time |

### subscriptions

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `webhook_url` | VARCHAR | Delivery target |
| `rules` | JSONB | Filter criteria |
| `active` | BOOLEAN | Soft-delete flag |
| `created_at` / `updated_at` | TIMESTAMPTZ | Timestamps |

### delivery_attempts

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `event_id` | UUID | FK → events |
| `subscription_id` | UUID | FK → subscriptions |
| `attempt_number` | INT | Attempt counter |
| `status` | VARCHAR | `pending`, `success`, `failed` |
| `http_code` | INT | Webhook HTTP response |
| `error_message` | TEXT | Failure detail |
| `next_retry_at` | TIMESTAMPTZ | Scheduled retry time |
| `created_at` / `updated_at` | TIMESTAMPTZ | Timestamps |

Unique constraint on `(event_id, subscription_id)`.

## API Surface

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | DB + Redis connectivity check |
| `POST` | `/api/v1/events` | Ingest event (durable + enqueue) |
| `GET` | `/api/v1/events/{eventId}` | Retrieve event |
| `GET` | `/api/v1/events/{eventId}/audit` | Delivery audit for event |
| `POST` | `/api/v1/subscriptions` | Create subscription |
| `GET` | `/api/v1/subscriptions` | List active subscriptions |
| `GET` | `/api/v1/subscriptions/{subId}` | Get subscription |
| `PUT` | `/api/v1/subscriptions/{subId}` | Update subscription |
| `DELETE` | `/api/v1/subscriptions/{subId}` | Soft-delete subscription |
| `GET` | `/api/v1/subscriptions/{subId}/audit` | Delivery audit for subscription |

Audit endpoints support `?limit=50&offset=0` pagination.

### Example: Event audit response

```json
{
  "event_id": "550e8400-e29b-41d4-a716-446655440000",
  "event": { "type": "user.created", "source": "auth-service", "...": "..." },
  "total_subscriptions": 1,
  "attempts": [
    {
      "subscription_id": "660e8400-e29b-41d4-a716-446655440001",
      "webhook_url": "https://webhook.site/abc",
      "status": "success",
      "attempt_number": 1,
      "http_code": 200,
      "timestamp": "2026-06-23T10:00:05Z"
    }
  ]
}
```

## Filter Rule Syntax

```json
{
  "type": "user.*",
  "source": "auth-service",
  "payload_rules": [
    {"path": "$.role", "op": "==", "value": "admin"},
    {"path": "$.amount", "op": ">", "value": 1000},
    {"path": "$.region", "op": "in", "value": ["us-east", "us-west"]},
    {"path": "$.email", "op": "regex", "value": ".*@example\\.com$"}
  ]
}
```

| Field | Description |
|-------|-------------|
| `type` | Event type match; supports `*` wildcard |
| `source` | Source match; supports `*` wildcard |
| `payload_rules` | JSON path conditions (all must match) |

Operators: `==`, `!=`, `>`, `<`, `>=`, `<=`, `in`, `regex`.

## Delivery Guarantees

**Per-subscriber: at-least-once.** See the full specification in **[Delivery Guarantees](delivery-guarantees.md)**.

| Semantic | This service |
|----------|--------------|
| At-least-once | Yes — retries until 2xx or max retries; duplicates possible |
| At-most-once | No — retries re-POST on transient failure |
| Exactly-once | No — subscribers must deduplicate by event `id` |

Quick reference:

1. Event persisted to PostgreSQL **before** Redis enqueue (non-atomic across both)
2. One `delivery_attempts` row per `(event_id, subscription_id)`
3. Backoff: `BASE_RETRY_DELAY × 2^(attempt-1)`; 4xx = no retry; 5xx/timeout = retry
4. Audit: `GET /api/v1/events/{id}/audit` and `GET /api/v1/subscriptions/{id}/audit`

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make up` / `make down` | Start/stop Docker Compose stack |
| `make build` | Build server and worker binaries |
| `make test` | Run unit tests |
| `make test-coverage` | Generate coverage report |
| `make test-ingest` | POST sample event |
| `make test-create-sub` | POST sample subscription |
| `make logs-worker` | Tail worker delivery logs |

## Testing

| Suite | Command | Scenarios covered |
|-------|---------|-------------------|
| Unit | `make test` | Rules matcher (type, source, wildcards, payload ops), webhook client, Redis queue, audit views |
| Integration | `make test-integration` | E2E fanout, 5xx retry, 4xx no-retry, non-match skip, payload filter, subscription CRUD, event + sub audit |
| CI | GitHub Actions on push | Both jobs in [`.github/workflows/test.yml`](../.github/workflows/test.yml) |

## CI/CD Pipelines

| Workflow | Trigger | Action |
|----------|---------|--------|
| `test.yml` | Push / PR | Unit tests + integration tests + lint |
| `build-push.yml` | Push to `main` | Build and push image to GHCR |
| `deploy-doks.yml` | After successful build | Helm deploy to DOKS |

## Related

- [Getting Started](getting-started.md)
- [Architecture](architecture.md)
- [DOKS Deployment](doks-deployment.md)
