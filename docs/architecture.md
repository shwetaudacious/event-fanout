# Architecture

System design for the Event Fanout Service — from event ingest through subscriber matching, fanout, retry loop, and delivery audit.

## System Context

```mermaid
flowchart TB
    Client[ClientApps] --> API[HTTPServer]
    API --> PG[(PostgreSQL)]
    API --> Redis[(RedisQueue)]
    Worker[FanoutWorker] --> Redis
    Worker --> PG
    Worker --> Webhooks[SubscriberWebhooks]
    Client --> Audit[AuditAPI]
    Audit --> PG
```

| Component | Role |
|-----------|------|
| **HTTP Server** | Ingest events, manage subscriptions, serve audit queries |
| **PostgreSQL** | Durable store: events, subscriptions, delivery_attempts |
| **Redis** | Async queue decoupling ingestion from fanout |
| **Fanout Worker** | Consumes queue, matches rules, delivers webhooks, retries |
| **Audit API** | Query delivery history per event or subscription |

---

## End-to-End Flow

```mermaid
sequenceDiagram
    participant Client
    participant API as HTTPServer
    participant DB as PostgreSQL
    participant Redis as RedisQueue
    participant Worker
    participant Webhook

    Client->>API: POST /api/v1/events
    API->>DB: INSERT event
    API->>Redis: LPUSH events:queue
    API-->>Client: 201 Created

    Worker->>Redis: BRPOP event
    Worker->>DB: SELECT active subscriptions
    Worker->>Worker: Evaluate filter rules
    Worker->>DB: INSERT delivery_attempt (pending)
    Worker->>Webhook: POST payload
    alt 2xx success
        Worker->>DB: UPDATE status=success
    else 5xx / timeout
        Worker->>DB: UPDATE status=failed, schedule next_retry_at
        Worker->>Webhook: Retry with exponential backoff
    else 4xx client error
        Worker->>DB: UPDATE status=failed (no retry)
    end

    Client->>API: GET /api/v1/events/{id}/audit
    API->>DB: SELECT delivery_attempts
    API-->>Client: Audit response
```

---

## Component Diagram

```mermaid
flowchart TB
    subgraph cmd [cmd]
        ServerMain[server]
        WorkerMain[worker]
    end

    subgraph internal [internal]
        HTTP[http/handler]
        EventSvc[service/event_service]
        FanoutSvc[service/fanout_service]
        Matcher[service/matcher]
        Queue[queue/redis]
        Delivery[delivery/client]
        Repos[repository/*]
    end

    ServerMain --> HTTP
    ServerMain --> EventSvc
    ServerMain --> Queue
    WorkerMain --> FanoutSvc
    WorkerMain --> Queue
    HTTP --> EventSvc
    EventSvc --> Repos
    EventSvc --> Queue
    FanoutSvc --> Matcher
    FanoutSvc --> Delivery
    FanoutSvc --> Repos
```

---

## Retry Loop

```mermaid
stateDiagram-v2
    [*] --> pending: Create attempt
    pending --> success: Webhook 2xx
    pending --> failed: Webhook 5xx/timeout
    failed --> pending: Retry scheduled (next_retry_at)
    failed --> failed: Max retries exceeded
    pending --> failed: Webhook 4xx (no retry)
    success --> [*]
```

Backoff formula: `BASE_RETRY_DELAY_SECONDS × 2^(attempt-1)`

---

## Deployment Topology

### Local (Docker Compose)

```mermaid
flowchart LR
    Dev[Developer] --> Server[:8080]
    Server --> PG[(Postgres)]
    Server --> Redis[(Redis)]
    Worker --> PG
    Worker --> Redis
```

### Production (DOKS)

See [DOKS Deployment](doks-deployment.md) for managed PostgreSQL, Redis, LoadBalancer, and GitHub Actions deploy pipeline.

---

## Delivery Guarantees

**At-least-once** per matching subscriber. See [README — Delivery Guarantees](../README.md#delivery-guarantees).

---

## Related

- [Project Details](project-details.md)
- [Getting Started](getting-started.md)
