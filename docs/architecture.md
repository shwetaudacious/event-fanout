# Architecture

System design documentation for the Event Fanout Service. Diagrams marked **current** reflect implemented behavior; diagrams marked **planned** describe the target design.

---

## System Context

**Status: mixed (server current, worker planned)**

Shows the major components and external dependencies at the highest level.

```mermaid
flowchart TB
    Client[ClientApps] --> API[HTTPServer]
    API --> PG[(PostgreSQL)]
    API --> Redis[(Redis)]
    Worker[FanoutWorker] --> PG
    Worker --> Redis
    Worker --> Webhooks[SubscriberWebhooks]
```

| Component | Role |
|-----------|------|
| **Client Apps** | Produce events and manage subscriptions via REST |
| **HTTP Server** | Validates requests, persists events, manages subscriptions |
| **PostgreSQL** | Durable store for events, subscriptions, delivery attempts |
| **Redis** | Async event queue between ingestion and processing |
| **Fanout Worker** | Consumes queue, matches rules, delivers webhooks |
| **Subscriber Webhooks** | Downstream HTTP endpoints receiving event notifications |

The HTTP server and PostgreSQL path is live today. The worker → webhook path is planned.

---

## Component Diagram

**Status: current structure, partial implementation**

Internal package layout and dependencies.

```mermaid
flowchart TB
    subgraph cmd [cmd]
        ServerMain[server/main.go]
        WorkerMain[worker/main.go]
    end

    subgraph internal [internal]
        HTTP[http/handler.go]
        EventSvc[service/event_service.go]
        SubSvc[service/subscription_service.go]
        Matcher[service/matcher.go]
        Queue[queue/redis.go]
        EventRepo[repository/event_repo.go]
        SubRepo[repository/subscription_repo.go]
        DeliveryRepo[repository/delivery_repo.go]
        Config[config/config.go]
        Models[models/models.go]
    end

    ServerMain --> Config
    ServerMain --> HTTP
    ServerMain --> EventSvc
    ServerMain --> SubSvc
    ServerMain --> Queue
    ServerMain --> EventRepo
    ServerMain --> SubRepo
    ServerMain --> DeliveryRepo

    WorkerMain --> Config
    WorkerMain -.-> EventSvc
    WorkerMain -.-> Queue

    HTTP --> EventSvc
    HTTP --> SubSvc
    EventSvc --> EventRepo
    EventSvc --> SubRepo
    EventSvc --> DeliveryRepo
    EventSvc --> Queue
    SubSvc --> SubRepo
    EventSvc -.-> Matcher

    EventRepo --> Models
    SubRepo --> Models
    DeliveryRepo --> Models
    Queue --> Models
```

Solid lines are wired today. Dashed lines (`-.->`) indicate planned connections (worker processing loop, matcher invocation during fanout).

### Package responsibilities

| Package | Responsibility |
|---------|---------------|
| `cmd/server` | Boot HTTP server, wire dependencies, graceful shutdown |
| `cmd/worker` | Boot background processor *(stub)* |
| `internal/http` | Route registration and request/response handling |
| `internal/service` | Business logic: ingestion, subscription management, rules matching |
| `internal/repository` | PostgreSQL CRUD for events, subscriptions, delivery attempts |
| `internal/queue` | Redis list adapter (`events:queue`) |
| `internal/config` | Environment variable configuration |
| `internal/models` | Domain types shared across layers |

---

## Event Ingestion Flow

**Status: current**

Sequence from client POST to durable storage.

```mermaid
sequenceDiagram
    participant Client
    participant Handler as HTTPHandler
    participant Svc as EventService
    participant Repo as EventRepository
    participant DB as PostgreSQL

    Client->>Handler: POST /api/v1/events
    Handler->>Handler: Validate type and source
    Handler->>Svc: IngestEvent(request)
    Svc->>Svc: Generate UUID and timestamp
    Svc->>Repo: Create(event)
    Repo->>DB: INSERT INTO events
    DB-->>Repo: OK
    Repo-->>Svc: event
    Svc-->>Handler: event
    Handler-->>Client: 201 Created + event JSON
```

After persistence the handler returns immediately. Enqueueing to Redis and triggering the worker is planned as a follow-on step in the ingestion path.

---

## Target Fanout Flow (Planned)

**Status: planned**

How events will be processed and delivered once the worker is implemented.

```mermaid
sequenceDiagram
    participant Redis as RedisQueue
    participant Worker
    participant Matcher as RulesMatcher
    participant SubRepo as SubscriptionRepository
    participant DelRepo as DeliveryRepository
    participant DB as PostgreSQL
    participant Webhook as SubscriberWebhook

    Worker->>Redis: Consume event from queue
    Worker->>SubRepo: List active subscriptions
    SubRepo->>DB: SELECT subscriptions WHERE active
    DB-->>SubRepo: subscriptions

    loop For each subscription
        Worker->>Matcher: Matches(event, subscription)
        alt Rule matches
            Worker->>DelRepo: Create delivery_attempt (pending)
            DelRepo->>DB: INSERT delivery_attempts
            Worker->>Webhook: POST event payload
            alt Success (2xx)
                Worker->>DelRepo: Update status=success
            else Failure (5xx / timeout)
                Worker->>DelRepo: Update status=failed, schedule retry
            end
        end
    end
```

### Retry behavior (planned)

Failed deliveries will use exponential backoff:

```
delay = BASE_RETRY_DELAY_SECONDS * 2^(attempt - 1)
```

Capped by `MAX_DELIVERY_RETRIES`. HTTP 4xx responses will not be retried (client error).

---

## Subscription Management Flow

**Status: current**

```mermaid
sequenceDiagram
    participant Client
    participant Handler as HTTPHandler
    participant Svc as SubscriptionService
    participant Repo as SubscriptionRepository
    participant DB as PostgreSQL

    Client->>Handler: POST /api/v1/subscriptions
    Handler->>Svc: CreateSubscription(request)
    Svc->>Repo: Create(subscription)
    Repo->>DB: INSERT INTO subscriptions
    DB-->>Repo: OK
    Repo-->>Svc: subscription
    Svc-->>Handler: subscription
    Handler-->>Client: 201 Created
```

Delete operations soft-deactivate subscriptions (`active = false`) rather than removing rows.

---

## Deployment Topology

**Status: current (Docker Compose), planned (Kubernetes fanout)**

### Local — Docker Compose

```mermaid
flowchart LR
    subgraph host [DeveloperMachine]
        subgraph compose [DockerCompose]
            Server[event_fanout_server :8080]
            Worker[event_fanout_worker]
            PG[event_fanout_db :5432]
            Redis[event_fanout_redis :6379]
        end
        Dev[Developer curl/browser]
    end

    Dev --> Server
    Server --> PG
    Server --> Redis
    Worker --> PG
    Worker --> Redis
```

Both server and worker are built from the same multi-stage Dockerfile. Postgres runs the init migration on first start.

### Kubernetes — Helm (target)

```mermaid
flowchart TB
    subgraph k8s [KubernetesCluster]
        LB[LoadBalancer Service]
        ServerDep[Server Deployment]
        WorkerDep[Worker Deployment]
    end

    subgraph managed [ManagedServices]
        PG[(PostgreSQL)]
        Redis[(Redis)]
    end

    Internet[ClientTraffic] --> LB
    LB --> ServerDep
    ServerDep --> PG
    ServerDep --> Redis
    WorkerDep --> PG
    WorkerDep --> Redis
    WorkerDep --> ExtWebhooks[External Webhooks]
```

The Helm chart under `helm/eventfanout/` defines server deployment, service, HPA, and service account. Configure `values.yaml` for image registry, resource limits, and ingress.

---

## Data Flow Summary

| Stage | Current | Planned |
|-------|---------|---------|
| 1. Ingest | Client → Server → PostgreSQL | Same |
| 2. Enqueue | — | Server → Redis queue |
| 3. Process | — | Worker reads queue |
| 4. Match | Matcher exists, not invoked | Worker → RulesMatcher → subscriptions |
| 5. Deliver | — | Worker → HTTP POST to webhook |
| 6. Retry | — | Exponential backoff on failure |
| 7. Audit | Service methods exist | HTTP endpoints + DB query |

---

## Observability

### Logging (current)

Server and worker emit structured JSON logs via zap:

```json
{"level":"info","caller":"service/event_service.go:56","msg":"event ingested","event_id":"550e8400-e29b-41d4-a716-446655440000"}
```

Configure verbosity with `LOG_LEVEL` (`debug`, `info`, `warn`, `error`).

### Metrics (planned)

OpenTelemetry integration planned for:

- Event ingestion rate
- Delivery success/failure rate
- Retry attempt count
- Webhook latency (p50/p99)
- Queue depth

### Audit trail (planned)

Query delivery history via:

- `GET /api/v1/events/{eventId}/audit`
- `GET /api/v1/subscriptions/{subId}/audit`

---

## Design Trade-offs

| Decision | Rationale |
|----------|-----------|
| At-least-once delivery | Simpler than exactly-once; subscribers deduplicate by event ID |
| PostgreSQL as source of truth | Durable ingestion before async processing |
| Redis for queue | Low-latency decoupling between API and worker |
| Separate server/worker binaries | Independent scaling and deployment |
| Soft-delete subscriptions | Preserve audit history for inactive subscriptions |

---

## Related Documentation

- [Project Details](project-details.md) — configuration, data model, API reference
- [Getting Started](getting-started.md) — local setup and walkthrough
