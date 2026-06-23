# Event Fanout Service

[![Tests](https://github.com/shwetaudacious/event-fanout/actions/workflows/test.yml/badge.svg)](https://github.com/shwetaudacious/event-fanout/actions/workflows/test.yml)

A production-ready event ingestion and webhook fanout service. Clients POST structured events; the service persists them durably, matches subscriptions by filter rules, and delivers notifications to registered webhook endpoints with retry, audit, and observability.

**Stack:** Go · PostgreSQL 15 · Redis 7 · Docker · Helm · DOKS · GitHub Actions

## Overview

| Capability | Status |
|------------|--------|
| Durable event ingestion (`POST /api/v1/events`) | Implemented |
| Subscription CRUD with filter rules | Implemented |
| Async fanout + webhook delivery | Implemented |
| Retry with exponential backoff | Implemented |
| Delivery audit endpoints | Implemented |
| DOKS deployment (Helm + GitHub Actions) | Implemented |

## Setup

### Prerequisites

- **Docker path:** Docker, Docker Compose, curl
- **Native Go path:** Go 1.25+, PostgreSQL 15, Redis 7
- **Production:** DOKS cluster, managed Postgres + Redis — see [DOKS Deployment](docs/doks-deployment.md)

### Docker Compose (recommended)

```bash
git clone https://github.com/shwetaudacious/event-fanout.git
cd event-fanout
make up
curl http://localhost:8080/health
```

This starts the API server (`:8080`), fanout worker, PostgreSQL (`:5432`), and Redis (`:6379`). Schema is applied automatically on first boot.

### Native Go

```bash
createdb eventfanout
psql eventfanout < migrations/001_init_schema.sql

export DATABASE_URL="postgres://postgres:postgres123@localhost:5432/eventfanout?sslmode=disable"
export REDIS_URL="redis://localhost:6379"

make build
./bin/server    # terminal 1
./bin/worker    # terminal 2 — required for webhook delivery
```

### Verify the stack

```bash
# Create a subscription (use a webhook.site URL for real delivery)
curl -X POST http://localhost:8080/api/v1/subscriptions \
  -H "Content-Type: application/json" \
  -d '{"webhook_url":"https://webhook.site/your-id","rules":{"type":"user.created"}}'

# Ingest an event
curl -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{"type":"user.created","source":"auth-service","payload":{"user_id":"123"}}'

# Check delivery audit (replace EVENT_ID)
curl http://localhost:8080/api/v1/events/EVENT_ID/audit
```

Full walkthrough: [Getting Started](docs/getting-started.md)

## Documentation

| Document | Description |
|----------|-------------|
| [Getting Started](docs/getting-started.md) | Local setup and end-to-end walkthrough |
| [Architecture](docs/architecture.md) | Full flow: ingest → match → fanout → retry → audit |
| [Project Details](docs/project-details.md) | Config, data model, API reference |
| [Delivery Guarantees](docs/delivery-guarantees.md) | At-least-once semantics and failure conditions |
| [DOKS Deployment](docs/doks-deployment.md) | Deploy to DigitalOcean Kubernetes |

## Architecture Flow

Full path from ingest through audit store (see [Architecture](docs/architecture.md) for details):

```mermaid
flowchart LR
    A[1_Ingest] --> B[2_Enqueue]
    B --> C[3_Consume]
    C --> D[4_MatchRules]
    D --> E[5_DeliverWebhook]
    E --> F[6_RetryLoop]
    F --> G[7_AuditStore]
    G --> H[8_QueryAudit]
```

| Step | Stage | Store updated |
|------|-------|---------------|
| 1 | REST ingest → validate → persist | `events` (PostgreSQL) |
| 2 | Enqueue for async processing | `events:queue` (Redis) |
| 3 | Worker dequeues event | — |
| 4 | Match against subscription rules | `subscriptions` (read) |
| 5 | POST to webhook; record attempt | `delivery_attempts` |
| 6 | Retry with backoff on 5xx/timeout | `delivery_attempts` |
| 7 | Persist final status + HTTP code | `delivery_attempts` |
| 8 | `GET .../audit` returns history | `delivery_attempts` (read) |

## API Summary

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check (DB + Redis) |
| `POST` | `/api/v1/events` | Ingest event |
| `GET` | `/api/v1/events/{eventId}` | Get event |
| `GET` | `/api/v1/events/{eventId}/audit` | Delivery audit for event |
| `POST` | `/api/v1/subscriptions` | Create subscription |
| `GET` | `/api/v1/subscriptions` | List subscriptions |
| `GET/PUT/DELETE` | `/api/v1/subscriptions/{subId}` | Manage subscription |
| `GET` | `/api/v1/subscriptions/{subId}/audit` | Delivery audit for subscription |

## Filter Rule Syntax

Subscriptions filter events with a JSON rules object:

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

| Field | Description | Example |
|-------|-------------|---------|
| `type` | Event type; supports `*` wildcard | `"user.*"`, `"order.created"` |
| `source` | Event source; supports `*` wildcard | `"auth-*"`, `"billing-service"` |
| `payload_rules` | JSON path conditions (all must match) | See operators below |

**Operators:** `==`, `!=`, `>`, `<`, `>=`, `<=`, `in`, `regex`

**Examples:**

| Use case | Rule |
|----------|------|
| All user events | `{"type": "user.*"}` |
| Premium signups only | `{"type": "user.created", "payload_rules": [{"path": "$.tier", "op": "==", "value": "premium"}]}` |
| High-value orders | `{"type": "order.completed", "payload_rules": [{"path": "$.amount", "op": ">", "value": 1000}]}` |

## Delivery Guarantees

**Per-subscriber webhook delivery: at-least-once.** See **[Delivery Guarantees](docs/delivery-guarantees.md)** for the full failure matrix.

| Semantic | Supported? |
|----------|------------|
| At-least-once | Yes |
| At-most-once | No |
| Exactly-once | No — subscribers deduplicate by event `id` |

## Testing

```bash
make test                  # Unit tests (matcher, delivery, queue, service)
make test-integration      # Integration tests (Postgres + Redis required)
make test-all              # Both
```

**Unit tests** cover: rules matcher, webhook client, Redis queue, audit view builder.

**Integration tests** cover: end-to-end fanout, retry on 5xx, no retry on 4xx, non-matching rules, payload filtering, subscription CRUD, event + subscription audit.

CI runs both jobs on every push — see [`.github/workflows/test.yml`](.github/workflows/test.yml).

## DOKS Deployment

```bash
helm upgrade --install event-fanout ./helm/eventfanout \
  -n event-fanout --create-namespace \
  --set secrets.databaseURL="$DATABASE_URL" \
  --set secrets.redisURL="$REDIS_URL"
```

See [DOKS Deployment](docs/doks-deployment.md). Required GitHub secrets: `DIGITALOCEAN_ACCESS_TOKEN`, `DATABASE_URL`, `REDIS_URL`.

## What We Sacrifice for Simplicity vs. What We'd Harden Next

| What we sacrifice now | Why it's acceptable for MVP | What we'd harden next |
|-----------------------|------------------------------|----------------------|
| **Redis list queue** (not Streams) | Simple LPUSH/BRPOP works for single-worker and low volume | Redis Streams + consumer groups for horizontal scaling and at-least-once dequeue |
| **No transactional outbox** | Faster to ship; ingest is durable to Postgres | Outbox table + relay for atomic DB+queue writes |
| **At-least-once delivery** | Industry-standard trade-off; simpler than exactly-once | Idempotency keys + dedup store for effectively-once |
| **No webhook HMAC signing** | Subscribers trust network path | HMAC-SHA256 signature headers on every POST |
| **Basic health checks** | Sufficient for DOKS liveness/readiness | Deep probes, per-webhook circuit breakers |
| **No metrics/tracing** | Logs + audit API cover debugging | OpenTelemetry metrics, Grafana dashboards, distributed tracing |
| **Single-region** | One DOKS cluster is enough to demo | Multi-region active-active with event replay |
| **No dead-letter queue** | Failed deliveries visible in audit API | DLQ with manual replay tooling for ops |
| **Non-atomic DB→Redis enqueue** | Rare edge case; returns 500 to client | Saga or outbox to eliminate under-delivery gap |

## Development

```bash
make build          # Build server + worker binaries
make up             # Start full stack
make test           # Unit tests
make test-integration  # Integration tests
make logs-worker    # Watch webhook delivery logs
```

## License

MIT — see [LICENSE](LICENSE).

## Support

[GitHub Issues](https://github.com/shwetaudacious/event-fanout/issues)
