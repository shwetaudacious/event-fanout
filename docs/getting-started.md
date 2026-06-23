# Getting Started

This guide covers three ways to run the Event Fanout Service locally and walks through an end-to-end API tutorial.

## Prerequisites

| Path | Requirements |
|------|-------------|
| Docker Compose (recommended) | Docker, Docker Compose, curl |
| Native Go | Go 1.26.4+, PostgreSQL 15, Redis 7, curl |
| Kubernetes | kubectl, Helm 3, a running cluster |

---

## Path A — Docker Compose (Recommended)

### 1. Clone and start

```bash
git clone https://github.com/event-fanout-service/event-fanout.git
cd event-fanout
make up
```

This starts four containers:

| Container | Role | Host port |
|-----------|------|-----------|
| `event_fanout_server` | HTTP API | `8080` |
| `event_fanout_worker` | Background processor (stub) | — |
| `event_fanout_db` | PostgreSQL 15 | `5432` |
| `event_fanout_redis` | Redis 7 | `6379` |

The database schema is applied automatically from `migrations/001_init_schema.sql` on first boot.

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
make logs-worker   # Worker only
make ps            # Container status
```

---

## Path B — Native Go Development

### 1. Install dependencies

Ensure PostgreSQL and Redis are running locally, then create the database:

```bash
createdb eventfanout
psql eventfanout < migrations/001_init_schema.sql
```

### 2. Set environment variables

```bash
export DATABASE_URL="postgres://postgres:postgres123@localhost:5432/eventfanout?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
export LOG_LEVEL="debug"
export ENVIRONMENT="development"
export SERVER_PORT="8080"
```

Adjust `DATABASE_URL` credentials to match your local Postgres setup.

### 3. Build and run

```bash
make build
./bin/server    # Terminal 1 — HTTP API on :8080
./bin/worker    # Terminal 2 — background processor (stub)
```

> **Note:** The worker currently connects to PostgreSQL and Redis, logs readiness, and waits for a shutdown signal. Event processing and webhook delivery are not yet implemented.

### 4. Run tests

```bash
make test
make test-coverage   # Generates coverage.html
```

---

## Path C — Kubernetes / Helm

### Prerequisites

- A Kubernetes cluster (e.g. DigitalOcean DOKS)
- `kubectl` configured for the cluster
- Helm 3 installed

### Deploy

```bash
# Create namespace
kubectl create namespace event-fanout

# Install chart
helm install event-fanout ./helm/eventfanout \
  -n event-fanout \
  -f ./helm/eventfanout/values.yaml

# Verify pods
kubectl get pods -n event-fanout
kubectl logs -n event-fanout -f deployment/event-fanout-server
```

### Upgrade

```bash
helm upgrade event-fanout ./helm/eventfanout \
  -n event-fanout \
  -f ./helm/eventfanout/values.yaml
```

### Access locally

```bash
# Port-forward
kubectl port-forward -n event-fanout svc/event-fanout 8080:80

# Or use LoadBalancer external IP
kubectl get svc -n event-fanout
```

Update `helm/eventfanout/values.yaml` to point `image.repository` at your container registry before deploying to production.

---

## End-to-End Walkthrough

This tutorial exercises the implemented API: create a subscription, ingest an event, and inspect the result.

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant API as Server
    participant DB as PostgreSQL

    Dev->>API: POST /api/v1/subscriptions
    API->>DB: Insert subscription
    API-->>Dev: 201 Created
    Dev->>API: POST /api/v1/events
    API->>DB: Persist event
    API-->>Dev: 201 Created
    Dev->>API: GET /api/v1/subscriptions
    API->>DB: Query subscriptions
    API-->>Dev: 200 OK
    Dev->>API: GET /health
    API-->>Dev: 200 OK
```

> Redis enqueue and webhook delivery are planned but not yet active in the ingestion path. See [Implementation Status](project-details.md#implementation-status).

### Step 1 — Start the stack

```bash
make up
curl http://localhost:8080/health
```

### Step 2 — Create a subscription

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

Save the returned `id` — you will use it in later steps.

Or use the Makefile shortcut:

```bash
make test-create-sub
```

### Step 3 — Ingest a matching event

```bash
curl -s -X POST http://localhost:8080/api/v1/events \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user.created",
    "source": "auth-service",
    "payload": {
      "user_id": "123",
      "email": "user@example.com"
    }
  }' | jq .
```

Or:

```bash
make test-ingest
```

The response includes the event `id` and `created_at` timestamp confirming durable storage.

### Step 4 — List subscriptions

```bash
curl -s http://localhost:8080/api/v1/subscriptions | jq .
# or
make test-list-subs
```

### Step 5 — Retrieve a single subscription

Replace `{subId}` with the ID from Step 2:

```bash
curl -s http://localhost:8080/api/v1/subscriptions/{subId} | jq .
```

### Step 6 — Update a subscription

```bash
curl -s -X PUT http://localhost:8080/api/v1/subscriptions/{subId} \
  -H "Content-Type: application/json" \
  -d '{
    "webhook_url": "https://webhook.site/new-endpoint",
    "rules": {"type": "user.created"}
  }' | jq .
```

### Step 7 — View server logs

```bash
make logs-server
```

Look for structured JSON log lines such as `"msg":"event ingested"` with the event ID.

### Step 8 — Run the test suite

```bash
make test
```

### Step 9 — Clean up

```bash
make down
```

---

## Troubleshooting

### Port already in use

If `5432`, `6379`, or `8080` is occupied:

```bash
# Find what's using the port
ss -tlnp | grep 8080

# Stop conflicting services or change docker-compose port mappings
```

### Database connection refused

Docker Compose waits for Postgres healthchecks before starting the server. If the server fails immediately:

```bash
docker-compose ps          # Check postgres is healthy
docker-compose logs postgres
```

For native Go development, confirm Postgres is running and `DATABASE_URL` is correct.

### Redis connection errors

The server and worker currently connect to Redis at hardcoded `localhost:6379` in [`cmd/server/main.go`](../cmd/server/main.go) and [`cmd/worker/main.go`](../cmd/worker/main.go), regardless of `REDIS_URL`. This works for native development and Docker Compose (each container has its own network namespace with Redis reachable at the service hostname in compose — note this as a known limitation when running outside compose).

### Schema errors on startup

Re-create the database volume:

```bash
make down
docker volume rm event-fanout_postgres_data 2>/dev/null || true
make up
```

### Worker exits immediately

Expected behavior today — the worker is a stub that waits for SIGTERM/SIGINT. Webhook processing will be added in a future release.

---

## Next Steps

- [Project Details](project-details.md) — configuration reference, data model, filter rules
- [Architecture](architecture.md) — system diagrams and planned delivery pipeline
