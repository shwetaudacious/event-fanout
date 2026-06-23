# DOKS Deployment Guide

Deploy the Event Fanout Service to **DigitalOcean Kubernetes (DOKS)** using Helm and GitHub Actions. Production uses **DigitalOcean Managed PostgreSQL** and **DigitalOcean Managed Redis** with **Redis Streams** as the fanout backbone.

## Prerequisites

- DigitalOcean account with a DOKS cluster (`event-fanout-cluster` or update the workflow)
- Managed PostgreSQL 15 and Redis 7 in the same VPC/region as the cluster
- Container image published to GHCR (`ghcr.io/shwetaudacious/event-fanout`)
- `doctl`, `kubectl`, and `helm` installed locally for manual deploys

## Architecture on DOKS

```mermaid
flowchart TB
    subgraph doks [DOKSCluster]
        LB[LoadBalancerService]
        Server[ServerDeployment]
        Worker[WorkerDeployment]
    end
    PG[(ManagedPostgreSQL)]
    Redis[(ManagedRedisStreams)]
    GHCR[GHCRImageRegistry]

    Internet --> LB
    LB --> Server
    Server --> PG
    Server -->|XADD events:stream| Redis
    Worker --> PG
    Worker -->|XREADGROUP / XACK| Redis
    Worker --> Webhooks[ExternalWebhooks]
    GHCR --> Server
    GHCR --> Worker
```

The server persists events to Postgres, then enqueues to the Redis stream `events:stream`. Worker pods join consumer group `fanout-workers` (one consumer name per pod) for at-least-once fanout with pending-message reclaim on crash.

## 1. Provision Infrastructure

One-command setup (creates DOKS cluster, Managed PostgreSQL, Managed Redis, runs migrations, optionally syncs GitHub secrets):

```bash
export DIGITALOCEAN_ACCESS_TOKEN=dop_v1_...
SYNC_GITHUB_SECRETS=1 make provision-doks
```

Or run the script directly:

```bash
./scripts/provision-digitalocean.sh
```

Manual steps below if you prefer the control panel.

### DOKS cluster

```bash
doctl kubernetes cluster create event-fanout-cluster \
  --region nyc1 \
  --node-pool "name=workers;size=s-2vcpu-4gb;count=1"
```

### Managed PostgreSQL

1. In the DigitalOcean control panel: **Databases → Create → PostgreSQL 15**.
2. Choose the same region/VPC as your cluster.
3. Create database `eventfanout` (or use default and set `dbname` in the URL).
4. Copy the **connection string** with `sslmode=require` for private networking.

### Managed Redis (Streams backbone)

1. **Databases → Create → Redis 7**.
2. Same region/VPC as DOKS and Postgres.
3. Enable **TLS** and use the **`rediss://`** connection URL (note the double `s`).
4. Redis Streams (`XADD`, `XREADGROUP`, `XACK`, `XAUTOCLAIM`) are supported on DO Managed Redis.

Example URLs (replace with values from the control panel):

```text
DATABASE_URL=postgres://doadmin:PASSWORD@private-db-host:25060/eventfanout?sslmode=require
REDIS_URL=rediss://default:PASSWORD@private-redis-host:25061
```

## 2. Configure GitHub Secrets

In repository **Settings → Secrets and variables → Actions**:

| Secret | Description |
|--------|-------------|
| `DIGITALOCEAN_ACCESS_TOKEN` | DO API token with Kubernetes read/write |
| `DATABASE_URL` | Managed Postgres URL (`sslmode=require`) |
| `REDIS_URL` | Managed Redis URL (`rediss://` for TLS) |

Optional: create a GitHub **environment** named `production` with required reviewers before deploy.

## 3. Run Database Migrations

Apply schema before first deploy:

```bash
psql "$DATABASE_URL" -f migrations/001_init_schema.sql
```

## 4. Manual Helm Deploy

```bash
doctl kubernetes cluster kubeconfig save event-fanout-cluster

helm upgrade --install event-fanout ./helm/eventfanout \
  --namespace event-fanout \
  --create-namespace \
  -f ./helm/eventfanout/values-doks.yaml \
  --set image.repository=ghcr.io/shwetaudacious/event-fanout \
  --set image.tag=latest \
  --set secrets.databaseURL="$DATABASE_URL" \
  --set secrets.redisURL="$REDIS_URL"
```

`values-doks.yaml` disables in-cluster Postgres/Redis, enables HPA, and sets stream defaults:

| Setting | Default | Purpose |
|---------|---------|---------|
| `config.redisStreamKey` | `events:stream` | Stream key for fanout queue |
| `config.redisConsumerGroup` | `fanout-workers` | Consumer group for workers |
| `REDIS_CONSUMER_NAME` | Pod name (downward API) | Unique consumer per worker pod |

## 5. Verify

```bash
kubectl get pods -n event-fanout
kubectl get svc -n event-fanout
kubectl logs -n event-fanout deployment/event-fanout-server
kubectl logs -n event-fanout deployment/event-fanout-worker
```

Port-forward for local testing:

```bash
kubectl port-forward -n event-fanout svc/event-fanout 8080:80
curl http://localhost:8080/health
```

Confirm stream activity (requires `redis-cli` with TLS to managed Redis):

```bash
redis-cli -u "$REDIS_URL" XINFO GROUPS events:stream
```

## 6. Automated CI/CD Deploy

| Workflow | Trigger | Purpose |
|----------|---------|---------|
| `.github/workflows/test.yml` | Push | Unit + integration tests |
| `.github/workflows/build-push.yml` | Push to `main` | Build and push image to GHCR |
| `.github/workflows/deploy-doks.yml` | After successful build | Helm deploy with `values-doks.yaml` |

Update `CLUSTER_NAME` in `deploy-doks.yml` if your cluster has a different name.

## Helm Values Reference

| Value | Description |
|-------|-------------|
| `image.repository` | Container image |
| `image.tag` | Image tag |
| `secrets.databaseURL` | PostgreSQL connection string |
| `secrets.redisURL` | Redis connection string (`rediss://` in production) |
| `config.redisStreamKey` | Redis stream key |
| `config.redisConsumerGroup` | Consumer group name |
| `config.maxDeliveryRetries` | Max webhook retry attempts |
| `config.workerReplicas` | Worker pod count |
| `autoscaling.enabled` | HPA for server pods |

## Related

- [Architecture](architecture.md)
- [Delivery guarantees](delivery-guarantees.md)
- [Getting Started](getting-started.md)
