# Getting Started

This guide covers three ways to run the Event Fanout Service and walks through a full ingest → fanout → audit flow.

## Prerequisites

| Path | Requirements |
|------|-------------|
| Docker Compose (recommended) | Docker, Docker Compose, curl, jq (optional) |
| Native Go | Go 1.22+, PostgreSQL 15, Redis 7, curl |
| DOKS / Kubernetes | kubectl, Helm 3, DOKS cluster — see [DOKS Deployment](doks-deployment.md) |

---

## Path A — Docker Compose (Recommended)

### 1. Clone and start

```bash
git clone https://github.com/shwetaudacious/event-fanout.git
cd event-fanout
make up
```

This starts four containers:

| Container | Role | Host port |
|-----------|------|-----------|
| `event_fanout_server` | HTTP API | `8080` |
| `event_fanout_worker` | Fanout + webhook delivery + retries | — |
| `event_fanout_db` | PostgreSQL 15 | `5432` |
| `event_fanout_redis` | Redis 7 | `6379` |

Schema is applied automatically from `migrations/001_init_schema.sql` on first boot.

### 2. Verify health

```bash
curl http://localhost:8080/health
```

Expected response:

```json
{
  "status": "healthy",
  "database": true,
  "redis": true,
  "message": "Event fanout service is running"
}
```

### 3. Stop services

```bash
make down
```

### Useful commands

```bash
make logs          # All container logs
make logs-server   # Server only
make logs-worker   # Worker only (webhook delivery logs)
make ps            # Container status
```

---

## Path B — Native Go Development

### 1. Install dependencies

```bash
createdb eventfanout
psql eventfanout < migrations/001_init_schema.sql
```

Ensure Redis is running on `localhost:6379`.

### 2. Set environment variables

```bash
export DATABASE_URL="postgres://postgres:postgres123@localhost:5432/eventfanout?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
export LOG_LEVEL="debug"
export ENVIRONMENT="development"
export SERVER_PORT="8080"
export MAX_DELIVERY_RETRIES="5"
export BASE_RETRY_DELAY_SECONDS="5"
export WEBHOOK_TIMEOUT_SECONDS="30"
```

### 3. Build and run

Both processes are required for end-to-end fanout:

```bash
make build
./bin/server    # Terminal 1 — HTTP API on :8080
./bin/worker    # Terminal 2 — consumes queue, delivers webhooks
```

### 4. Run tests

```bash
make test                                              # Unit tests
go test -tags=integration ./tests/integration/...      # Integration (needs Postgres + Redis)
make test-coverage                                     # Coverage report
```

---

## Path C — DOKS / Kubernetes

For production deployment to DigitalOcean Kubernetes, see **[DOKS Deployment](doks-deployment.md)**.

Quick manual deploy:

```bash
helm upgrade --install event-fanout ./helm/eventfanout \
  -n event-fanout --create-namespace \
  --set image.repository=ghcr.io/shwetaudacious/event-fanout \
  --set secrets.databaseURL="$DATABASE_URL" \
  --set secrets.redisURL="$REDIS_URL"
```

---

## End-to-End Walkthrough

Full flow: subscription → ingest → worker fanout → webhook delivery → audit query.

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant API as Server
    participant DB as PostgreSQL
    participant Redis as RedisQueue
    participant Worker
    participant Webhook

    Dev->>API: POST /api/v1/subscriptions
    API->>DB: Insert subscription
    Dev->>API: POST /api/v1/events
    API->>DB: Persist event
    API->>Redis: Enqueue event
    Worker->>Redis: Dequeue event
    Worker->>Webhook: POST payload
    Dev->>API: GET /api/v1/events/{id}/audit
    API->>DB: Query delivery_attempts
    API-->>Dev: Audit with status + HTTP code
```

### Step 1 — Start the stack

```bash
make up
curl http://localhost:8080/health
```

### Step 2 — Create a subscription

Use [webhook.site](https://webhook.site) to get a test URL, then:

```bash
curl -s -X POST http://localhost:8080/api/v1/subscriptions \
  -H "Content-Type: application/json" \
  -d '{
    "webhook_url": "https://webhook.site/your-unique-id",
    "rules": {
      "type": "user.created",
      "source": "auth-service"
    }
  }' | jq .
```

Save the returned `id` as `SUB_ID`.

### Step 3 — Ingest a matching event

```bash
curl -s -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user.created",
    "source": "auth-service",
    "payload": {"user_id": "123", "email": "user@example.com"}
  }' | jq .
```

Save the returned `id` as `EVENT_ID`. Within a few seconds the worker delivers to your webhook URL.

### Step 4 — Verify webhook delivery

Check webhook.site for the POST payload, or inspect worker logs:

```bash
make logs-worker
```

### Step 5 — Query delivery audit

```bash
curl -s http://localhost:8080/api/v1/events/$EVENT_ID/audit | jq .
```

Expected: `attempts[0].status` is `"success"` with `http_code: 200`.

Subscription-level audit:

```bash
curl -s http://localhost:8080/api/v1/subscriptions/$SUB_ID/audit | jq .
```

### Step 6 — Run tests

```bash
make test
```

### Step 7 — Clean up

```bash
make down
```

---

## Filter Rules Example

Match premium users with a payload rule:

```bash
curl -s -X POST http://localhost:8080/api/v1/subscriptions \
  -H "Content-Type: application/json" \
  -d '{
    "webhook_url": "https://webhook.site/your-id",
    "rules": {
      "type": "user.created",
      "payload_rules": [
        {"path": "$.tier", "op": "==", "value": "premium"}
      ]
    }
  }'
```

Supported operators: `==`, `!=`, `>`, `<`, `>=`, `<=`, `in`, `regex`. Type and source support `*` wildcards.

---

## Troubleshooting

### Port already in use

```bash
ss -tlnp | grep -E '8080|5432|6379'
```

Stop conflicting services or adjust ports in `docker-compose.yml`.

### Database connection refused

```bash
docker compose ps
docker compose logs postgres
```

Wait for the Postgres healthcheck before the server starts.

### Events ingested but not delivered

1. Confirm the worker container is running: `make ps`
2. Check worker logs: `make logs-worker`
3. Verify subscription rules match the event type/source
4. Confirm the webhook URL is reachable from the worker container

### Schema errors on startup

Reset the database volume:

```bash
make down
docker volume rm event-fanout_postgres_data 2>/dev/null || true
make up
```

---

## Next Steps

- [Architecture](architecture.md) — ingest → fanout → retry → audit diagrams
- [Delivery Guarantees](delivery-guarantees.md) — at-least-once semantics and failure conditions
- [Project Details](project-details.md) — config, API reference, data model
- [DOKS Deployment](doks-deployment.md) — production deploy to DigitalOcean Kubernetes
