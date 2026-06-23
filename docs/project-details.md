# Project Details

Reference documentation for the Event Fanout Service repository: metadata, layout, configuration, data model, and API surface.

## Project Metadata

| Field | Value |
|-------|-------|
| **Name** | Event Fanout Service |
| **Module path** | `github.com/event-fanout-service/event-fanout` |
| **Version** | `0.1.0` |
| **License** | MIT |
| **Language** | Go 1.26.4 |

### Purpose

Accept structured events over HTTP, persist them durably in PostgreSQL, match them against subscriber filter rules, and deliver notifications to registered webhook endpoints with retry logic and an audit trail.

---

## Repository Layout

```
event-fanout/
├── cmd/
│   ├── server/              HTTP API entrypoint
│   └── worker/              Background processor (stub)
├── internal/
│   ├── config/              Environment-based configuration
│   ├── http/                REST handlers and route registration
│   ├── models/              Domain types and request/response structs
│   ├── queue/               Redis queue adapter
│   ├── repository/          PostgreSQL data access
│   └── service/             Business logic and rules matcher
├── migrations/              Database schema bootstrap SQL
├── helm/eventfanout/        Kubernetes Helm chart
├── .github/workflows/       CI (tests, lint) and container build
├── docker-compose.yml       Local development stack
├── Dockerfile               Multi-stage build (server + worker)
├── Makefile                 Build, test, and ops targets
└── docs/                    Project documentation
```

---

## Tech Stack

### Runtime

| Component | Library / Tool |
|-----------|----------------|
| HTTP router | [Gorilla Mux](https://github.com/gorilla/mux) |
| PostgreSQL driver | [pgx/v5](https://github.com/jackc/pgx) |
| Redis client | [go-redis/v9](https://github.com/redis/go-redis) |
| Logging | [zap](https://go.uber.org/zap) |
| UUIDs | [google/uuid](https://github.com/google/uuid) |

### Data Stores

- **PostgreSQL 15** — events, subscriptions, delivery_attempts
- **Redis 7** — event queue (currently a Redis list; Redis Streams planned)

### Operations

- **Docker Compose** — local development (postgres, redis, server, worker)
- **Dockerfile** — multi-stage build producing `/app/server` and `/app/worker`
- **Helm** — Kubernetes deployment chart under `helm/eventfanout/`
- **GitHub Actions** — test/lint workflow and container build/push workflow

---

## Configuration Reference

All configuration is loaded from environment variables in [`internal/config/config.go`](../internal/config/config.go).

| Variable | Default | Used by | Description |
|----------|---------|---------|-------------|
| `SERVER_PORT` | `8080` | server | HTTP listen port |
| `SERVER_HOST` | `0.0.0.0` | server | HTTP bind address |
| `DATABASE_URL` | `postgres://user:password@localhost:5432/eventfanout` | server, worker | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379` | server, worker | Redis connection URL *(parsed in config; server currently hardcodes `localhost:6379`)* |
| `MAX_WORKERS` | `5` | worker (planned) | General worker pool size |
| `EVENT_PROCESSOR_WORKERS` | `2` | worker (planned) | Event processor concurrency |
| `FANOUT_WORKER_POOL` | `10` | worker (planned) | Concurrent webhook delivery workers |
| `MAX_DELIVERY_RETRIES` | `5` | worker (planned) | Max retry attempts per delivery |
| `BASE_RETRY_DELAY_SECONDS` | `5` | worker (planned) | Initial retry delay (exponential backoff) |
| `WEBHOOK_TIMEOUT_SECONDS` | `30` | worker (planned) | HTTP timeout for webhook POST |
| `WEBHOOK_MAX_BODY_BYTES` | `1048576` | worker (planned) | Max webhook response body size |
| `LOG_LEVEL` | `info` | server, worker | Log level: `debug`, `info`, `warn`, `error` |
| `ENVIRONMENT` | `development` | server, worker | Runtime environment label |

### Docker Compose Defaults

When running `make up`, [`docker-compose.yml`](../docker-compose.yml) sets:

| Service | Host port | Notes |
|---------|-----------|-------|
| API server | `8080` | `LOG_LEVEL=debug`, `ENVIRONMENT=development` |
| PostgreSQL | `5432` | DB `eventfanout`, user `postgres`, password `postgres123` |
| Redis | `6379` | No auth |

Server `DATABASE_URL`:

```
postgres://postgres:postgres123@postgres:5432/eventfanout?sslmode=disable
```

Worker additionally sets `MAX_DELIVERY_RETRIES=5`, `BASE_RETRY_DELAY_SECONDS=5`, `WEBHOOK_TIMEOUT_SECONDS=30`, `FANOUT_WORKER_POOL=10`.

---

## Data Model

Schema defined in [`migrations/001_init_schema.sql`](../migrations/001_init_schema.sql).

### `events`

Stores ingested events.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `type` | VARCHAR(255) | Event type (e.g. `user.created`) |
| `source` | VARCHAR(255) | Originating service |
| `payload` | JSONB | Arbitrary event data |
| `created_at` | TIMESTAMP | Ingestion time |

### `subscriptions`

Webhook registrations with filter rules.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `webhook_url` | VARCHAR(2048) | Delivery target URL |
| `rules` | JSONB | Filter criteria (type, source, payload_rules) |
| `active` | BOOLEAN | Whether subscription is active |
| `created_at` | TIMESTAMP | Creation time |
| `updated_at` | TIMESTAMP | Last update time |

### `delivery_attempts`

Tracks webhook delivery state per event/subscription pair.

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key |
| `event_id` | UUID | FK → events |
| `subscription_id` | UUID | FK → subscriptions |
| `attempt_number` | INT | Attempt counter |
| `status` | VARCHAR(50) | `pending`, `success`, or `failed` |
| `http_code` | INT | HTTP response code from webhook |
| `error_message` | TEXT | Error detail on failure |
| `next_retry_at` | TIMESTAMP | Scheduled retry time |
| `created_at` | TIMESTAMP | Attempt timestamp |

Unique constraint on `(event_id, subscription_id)`.

---

## API Surface

Routes registered in [`internal/http/handler.go`](../internal/http/handler.go).

### Implemented

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Returns service health (database, redis status) |
| `POST` | `/api/v1/events` | Ingest an event (`type` and `source` required) |
| `POST` | `/api/v1/subscriptions` | Create a subscription |
| `GET` | `/api/v1/subscriptions` | List active subscriptions |
| `GET` | `/api/v1/subscriptions/{subId}` | Get a subscription by ID |
| `PUT` | `/api/v1/subscriptions/{subId}` | Update a subscription |
| `DELETE` | `/api/v1/subscriptions/{subId}` | Soft-delete (marks inactive) |

#### POST /api/v1/events

**Request:**

```json
{
  "type": "user.created",
  "source": "auth-service",
  "payload": {
    "user_id": "123",
    "email": "user@example.com"
  }
}
```

**Response:** `201 Created`

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "user.created",
  "source": "auth-service",
  "payload": { "user_id": "123", "email": "user@example.com" },
  "created_at": "2026-06-23T10:00:00Z"
}
```

#### POST /api/v1/subscriptions

**Request:**

```json
{
  "webhook_url": "http://webhook.example.com/events",
  "rules": {
    "type": "user.created",
    "source": "auth-service"
  }
}
```

**Response:** `201 Created`

```json
{
  "id": "660e8400-e29b-41d4-a716-446655440001",
  "webhook_url": "http://webhook.example.com/events",
  "rules": { "type": "user.created", "source": "auth-service" },
  "active": true,
  "created_at": "2026-06-23T09:00:00Z",
  "updated_at": "2026-06-23T09:00:00Z"
}
```

### Planned

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/events/{eventId}` | Retrieve event by ID |
| `GET` | `/api/v1/events/{eventId}/audit` | Delivery audit for an event |
| `GET` | `/api/v1/subscriptions/{subId}/audit` | Delivery audit for a subscription |

Service-layer methods for audit exist in [`internal/service/event_service.go`](../internal/service/event_service.go) but are not yet wired to HTTP routes.

---

## Filter Rule Syntax

Subscriptions match events using a JSON rules object.

### Basic Rules

```json
{
  "type": "user.created",
  "source": "auth-service",
  "payload_rules": []
}
```

| Field | Description | Matcher status |
|-------|-------------|----------------|
| `type` | Exact match on event type | Implemented |
| `source` | Exact match on event source | Implemented |
| `payload_rules` | JSON path conditions | Planned |

### Payload Rules (planned)

```json
{
  "path": "$.user.role",
  "op": "==",
  "value": "admin"
}
```

Supported operators (planned): `==`, `!=`, `>`, `<`, `>=`, `<=`, `in`, `regex`.

---

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make help` | List available targets |
| `make build` | Build `bin/server` and `bin/worker` |
| `make test` | Run unit tests with race detector |
| `make test-coverage` | Run tests and generate `coverage.html` |
| `make lint` | Run golangci-lint |
| `make fmt` | Format code and run `go mod tidy` |
| `make clean` | Remove `bin/` and coverage artifacts |
| `make docker-build` | Build Docker image locally |
| `make docker-push` | Tag and push to container registry |
| `make up` | Start all services via docker-compose |
| `make down` | Stop and remove containers |
| `make logs` | Tail all container logs |
| `make logs-server` | Tail server logs |
| `make logs-worker` | Tail worker logs |
| `make ps` | Show running containers |
| `make test-ingest` | POST a sample event to localhost:8080 |
| `make test-list-subs` | GET all subscriptions |
| `make test-create-sub` | POST a sample subscription |

---

## Implementation Status

| Component | Status | Notes |
|-----------|--------|-------|
| HTTP server | Done | Graceful shutdown, health check |
| Event ingestion | Done | Persists to PostgreSQL |
| Subscription CRUD | Done | Full create/read/update/delete |
| Rules matcher | Partial | Type and source only; no wildcards or payload rules yet |
| Redis enqueue on ingest | Planned | Queue adapter exists (`LPUSH events:queue`) but not called from ingestion |
| Background worker | Stub | Connects to DB/Redis; processing loop not implemented |
| Webhook delivery | Planned | Delivery repo and models exist |
| Retry with backoff | Planned | Config vars defined |
| Audit HTTP endpoints | Planned | Service methods exist; routes not registered |
| Redis Streams | Planned | Currently uses Redis list |
| Payload rule operators | Planned | Model defined; matcher not implemented |

---

## Delivery Guarantees (target)

The service is designed for **at-least-once** delivery:

- Events are persisted before processing
- Failed deliveries are retried with exponential backoff
- Subscribers must implement idempotency (deduplicate by event ID)

See [Architecture — Target Fanout Flow](architecture.md#target-fanout-flow-planned) for the planned delivery pipeline.

---

## Related Documentation

- [Getting Started](getting-started.md) — setup and walkthrough
- [Architecture](architecture.md) — diagrams and data flows
