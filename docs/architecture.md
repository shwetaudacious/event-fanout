# Architecture

System design for the Event Fanout Service — full path from event ingest through subscriber matching, fanout, retry loop, and delivery audit store.

## Full Architecture Flow

The diagram below maps every stage in order. Numbers match the step descriptions.

```mermaid
flowchart TB
    subgraph ingest [1_EventIngestion]
        C[ClientApp] -->|POST /api/v1/events| API[HTTPServer]
        API -->|INSERT| EventsTable[(events table)]
        API -->|LPUSH events:queue| RedisQ[(Redis Queue)]
    end

    subgraph fanout [2_3_FanoutAndMatching]
        RedisQ -->|BRPOP| Worker[FanoutWorker]
        Worker -->|SELECT active| SubsTable[(subscriptions table)]
        Worker --> Matcher[RulesMatcher]
        Matcher -->|match| Worker
    end

    subgraph delivery [4_5_DeliveryAndRetry]
        Worker -->|INSERT pending| AttemptsTable[(delivery_attempts table)]
        Worker -->|POST JSON| Webhook[SubscriberWebhook]
        Webhook -->|2xx| Worker
        Webhook -->|5xx/timeout| RetryLoop[RetryLoop]
        RetryLoop -->|exponential backoff| Worker
        Webhook -->|4xx| FailedFinal[failed no retry]
        Worker -->|UPDATE status| AttemptsTable
    end

    subgraph audit [6_DeliveryAuditStore]
        AttemptsTable --> AuditAPI[AuditAPI]
        EventsTable --> AuditAPI
        SubsTable --> AuditAPI
        Client2[ClientApp] -->|GET .../audit| AuditAPI
    end
```

### Step-by-step

| Step | Stage | Component | Action | Store |
|------|-------|-----------|--------|-------|
| 1 | **Ingest** | HTTP Server | Validate `type`, `source`, `payload`; persist event | `events` (PostgreSQL) |
| 2 | **Enqueue** | HTTP Server | Push event JSON to Redis list | `events:queue` (Redis) |
| 3 | **Consume** | Worker | Block on `BRPOP`; deserialize event | — |
| 4 | **Match** | Rules Matcher | Load active subscriptions; filter by type, source, payload rules | `subscriptions` (read) |
| 5 | **Deliver** | Fanout Service | Create `delivery_attempts` row (`pending`); POST to webhook URL | `delivery_attempts` (write) |
| 6 | **Record** | Fanout Service | On 2xx → `success`; on 5xx/timeout → schedule retry; on 4xx → `failed` | `delivery_attempts` (update) |
| 7 | **Retry loop** | Worker | Poll `next_retry_at`; re-POST with backoff until success or max retries | `delivery_attempts` (update) |
| 8 | **Audit** | Audit API | Join event + attempts + subscription webhook URL; return history | `delivery_attempts` (read) |

---

## End-to-End Sequence

```mermaid
sequenceDiagram
    participant Client
    participant API as HTTPServer
    participant DB as PostgreSQL
    participant Redis as RedisQueue
    participant Worker
    participant Matcher as RulesMatcher
    participant Webhook
    participant Audit as AuditAPI

    Client->>API: POST /api/v1/events
    API->>DB: INSERT INTO events
    API->>Redis: LPUSH events:queue
    API-->>Client: 201 Created

    Worker->>Redis: BRPOP event
    Worker->>DB: SELECT subscriptions WHERE active
    Worker->>Matcher: Matches(event, subscription)
    Matcher-->>Worker: true/false per sub

    loop Each matching subscription
        Worker->>DB: INSERT delivery_attempt (pending)
        Worker->>Webhook: POST event payload
        alt 2xx
            Worker->>DB: UPDATE status=success, http_code
        else 5xx / timeout
            Worker->>DB: UPDATE status=failed, next_retry_at
            Note over Worker,Webhook: Retry loop with backoff
            Worker->>Webhook: POST (retry)
        else 4xx
            Worker->>DB: UPDATE status=failed (permanent)
        end
    end

    Client->>Audit: GET /api/v1/events/{id}/audit
    Audit->>DB: SELECT delivery_attempts + event
    Audit-->>Client: attempts with timestamps, http_code, status
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
    [*] --> pending: Worker creates attempt
    pending --> success: Webhook 2xx
    pending --> failed_retryable: 5xx / timeout
    failed_retryable --> pending: next_retry_at reached
    failed_retryable --> success: Retry returns 2xx
    failed_retryable --> failed_final: MAX_DELIVERY_RETRIES exceeded
    pending --> failed_final: Webhook 4xx
    success --> [*]
    failed_final --> [*]
```

Backoff: `BASE_RETRY_DELAY_SECONDS × 2^(attempt-1)`

---

## Data Stores

| Store | Table / Key | Written by | Read by |
|-------|-------------|------------|---------|
| PostgreSQL | `events` | Ingest API | Audit API, Worker |
| PostgreSQL | `subscriptions` | Subscription API | Worker (matcher) |
| PostgreSQL | `delivery_attempts` | Worker | Audit API, Retry poller |
| Redis | `events:queue` | Ingest API | Worker |

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
    Worker --> Webhooks[ExternalWebhooks]
```

### Production (DOKS)

See [DOKS Deployment](doks-deployment.md).

---

## Related

- [Delivery Guarantees](delivery-guarantees.md)
- [Project Details](project-details.md)
- [Getting Started](getting-started.md)
