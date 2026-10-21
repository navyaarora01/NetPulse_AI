# NetPulse AI — End-to-End Project Specification

> **Purpose:** This document is the source of truth for an AI coding agent to build, test, document, and deploy NetPulse AI end to end.
>
> **Project:** Real-Time Network Observability Platform with AI-Assisted Incident Analysis  
> **Primary language:** Go  
> **Core technologies:** Go, Apache Kafka, Google Cloud Pub/Sub, PostgreSQL, Redis, Prometheus, Grafana, Docker, and an LLM API.

---

## 1. Instructions to the AI coding agent

Act as a senior backend/platform engineer. Build a complete, runnable, well-tested project—not just a prototype or a collection of snippets.

### Working rules

1. First inspect the current repository and its existing files. Preserve useful existing work and conventions.
2. If the repository is empty, initialize it with the structure in this specification.
3. Implement the project in small, coherent stages. Keep the application runnable after each stage.
4. Use **Go** for backend services and workers. Use idiomatic Go, clear package boundaries, context cancellation, structured logging, and graceful shutdown.
5. Do not invent credentials, deployment IDs, domains, or successful deployment results. Use environment variables and explain any manual steps.
6. Prefer a simple, maintainable MVP over unnecessary abstractions. Do not add Kubernetes unless it is genuinely needed; Docker Compose is sufficient locally.
7. Use stable, compatible dependency versions and pin them in the appropriate dependency files.
8. Add tests for business logic, API behavior, event processing, and failure paths. Do not claim tests pass unless they have actually been run.
9. Never commit secrets. Provide `.env.example`, and ensure `.env`, credentials, and generated files are ignored.
10. When finished, provide a concise summary of implemented features, commands to run, test results, deployment status, and any remaining manual steps.

---

## 2. Product overview

**NetPulse AI** is a real-time network observability platform that ingests simulated network telemetry, tracks device health, detects anomalous behavior, creates incidents, and uses an LLM to produce readable incident summaries and troubleshooting suggestions.

The system should demonstrate practical backend engineering concepts:

- Concurrent event ingestion and asynchronous processing
- Kafka topics, consumer groups, retries, and dead-letter handling
- Optional Google Cloud Pub/Sub ingestion through a provider adapter
- PostgreSQL persistence and Redis caching
- Rule-based and sliding-window anomaly detection
- REST APIs for devices, metrics, and incidents
- Prometheus metrics and Grafana dashboards
- AI-generated incident analysis with safe fallback behavior
- Dockerized local development and a documented cloud deployment path

### Intended demo

A user opens the dashboard, sees simulated routers sending telemetry, observes a latency or packet-loss spike, and sees an incident appear. The incident page displays metrics and an AI-generated explanation with possible investigation steps.

**Important:** AI analysis is advisory. Detection and incident severity must be determined by deterministic rules, not by the LLM.

---

## 3. Scope

### MVP — required

1. Generate telemetry for configurable simulated network devices.
2. Ingest telemetry through a Go HTTP endpoint and publish events to Kafka.
3. Consume events asynchronously, validate them, and calculate device health and rolling metrics.
4. Cache the latest device state in Redis.
5. Persist device metadata, telemetry aggregates, incidents, and AI analysis status in PostgreSQL.
6. Detect anomalies using configurable thresholds and rolling windows.
7. Create, deduplicate, list, inspect, acknowledge, and resolve incidents.
8. Generate an AI incident summary and troubleshooting suggestions through a provider interface.
9. Expose REST APIs and a basic web dashboard.
10. Expose Prometheus metrics and include a Grafana dashboard.
11. Provide Docker Compose, migrations, seed/demo data, tests, and deployment documentation.

### Optional enhancements — only after the MVP works

- Google Cloud Pub/Sub as an alternate telemetry transport or a Kafka-to-Pub/Sub bridge.
- Authentication and role-based access.
- WebSocket or Server-Sent Events for live dashboard updates.
- Alert delivery through email or a webhook.
- Kubernetes deployment manifests.
- More advanced statistical anomaly detection.

Do not allow optional features to delay the core MVP.

---

## 4. Technology choices

Use the following defaults unless the existing repository already has a sound equivalent:

| Area | Technology |
|---|---|
| Backend | Go (current stable release), standard `net/http` or Chi |
| API format | REST + JSON |
| Event streaming | Apache Kafka |
| Kafka client | A maintained Go Kafka client such as `franz-go` |
| Database | PostgreSQL |
| Database access | `pgx` / `pgxpool` with explicit SQL or `sqlc` |
| Cache | Redis |
| Redis client | `go-redis` |
| Metrics | Prometheus client for Go |
| Dashboards | Grafana |
| Local infrastructure | Docker Compose |
| Schema migrations | `golang-migrate` or an equivalent migration tool |
| AI | Provider interface with an OpenAI-compatible API implementation |
| Frontend | React + Vite + JavaScript, unless a frontend already exists |
| Frontend styling | Simple responsive CSS |
| API documentation | OpenAPI 3.1 |
| Testing | Go `testing`, HTTP tests, integration tests, and frontend tests as appropriate |
| CI | GitHub Actions |

### Transport design

Kafka is the default event bus for the MVP. Google Cloud Pub/Sub should be implemented behind a small transport interface only if it can be done without complicating the primary path.

Do not pretend Kafka and Pub/Sub are both active in the deployed system unless both are actually configured and tested. Clearly document which transport is used in each environment.

---

## 5. High-level architecture

```text
                  ┌─────────────────────────┐
                  │  Telemetry Simulator    │
                  │  configurable devices   │
                  └────────────┬────────────┘
                               │ HTTP / JSON
                               ▼
                  ┌─────────────────────────┐
                  │ Go Ingestion API        │
                  │ validate + publish      │
                  └────────────┬────────────┘
                               ▼
                  ┌─────────────────────────┐
                  │ Kafka: network-telemetry│
                  └────────────┬────────────┘
                               ▼
                  ┌─────────────────────────┐
                  │ Go Processing Worker    │
                  │ validate + aggregate    │
                  │ health + anomaly rules  │
                  └──────┬─────────┬────────┘
                         │         │
                         ▼         ▼
                    PostgreSQL    Redis
                         │         │
                         └────┬────┘
                              ▼
                  ┌─────────────────────────┐
                  │ Incident Service / API  │
                  │ lifecycle + dedup       │
                  └────────────┬────────────┘
                               ▼
                  ┌─────────────────────────┐
                  │ AI Analysis Worker      │
                  │ summary + suggestions   │
                  └────────────┬────────────┘
                               ▼
                  ┌─────────────────────────┐
                  │ React Dashboard         │
                  │ devices + incidents     │
                  └─────────────────────────┘

       Prometheus scrapes Go services; Grafana displays metrics.
       Optional Pub/Sub adapter can feed the same processing pipeline.
```

Keep the initial implementation modular, but avoid splitting every package into a separately deployed microservice. A reasonable MVP has an API process, a worker process, and a simulator process, with shared internal packages.

---

## 6. Repository structure

Use a structure similar to:

```text
netpulse-ai/
├── cmd/
│   ├── api/main.go
│   ├── worker/main.go
│   └── simulator/main.go
├── internal/
│   ├── config/
│   ├── domain/
│   ├── httpapi/
│   │   ├── handlers/
│   │   ├── middleware/
│   │   └── routes.go
│   ├── telemetry/
│   │   ├── validation.go
│   │   ├── service.go
│   │   └── repository.go
│   ├── device/
│   ├── incident/
│   ├── anomaly/
│   ├── ai/
│   │   ├── provider.go
│   │   ├── client.go
│   │   └── prompts.go
│   ├── messaging/
│   │   ├── producer.go
│   │   ├── consumer.go
│   │   └── pubsub/          # optional adapter
│   ├── cache/
│   ├── database/
│   ├── metrics/
│   └── logger/
├── migrations/
├── web/
│   ├── src/
│   ├── package.json
│   └── vite.config.js
├── deploy/
│   ├── grafana/
│   └── cloud/               # deployment configuration and scripts
├── docs/
│   ├── architecture.md
│   ├── api.md
│   └── deployment.md
├── scripts/
│   ├── seed.sh
│   └── load-test.sh
├── tests/
│   └── integration/
├── .github/workflows/ci.yml
├── .env.example
├── .gitignore
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
├── README.md
└── LICENSE
```

Adjust this structure if necessary, but keep API, domain logic, storage, messaging, and AI integration separated.

---

## 7. Domain model and telemetry schema

### Device

Fields:

- `id`: UUID
- `name`: string
- `location`: string
- `status`: `online`, `degraded`, or `offline`
- `created_at`, `updated_at`: timestamps
- `last_seen_at`: nullable timestamp

### Telemetry event

Use a versioned JSON event. Example:

```json
{
  "event_id": "uuid",
  "schema_version": 1,
  "device_id": "router-001",
  "timestamp": "2026-01-15T10:30:00Z",
  "metrics": {
    "latency_ms": 42.5,
    "packet_loss_pct": 0.4,
    "bandwidth_mbps": 125.0,
    "cpu_usage_pct": 54.0,
    "memory_usage_pct": 63.0,
    "signal_strength_dbm": -67.0
  }
}
```

Requirements:

- Validate required fields, timestamp, numeric ranges, and event ID.
- Reject malformed events with a useful error.
- Make processing idempotent using `event_id`.
- Use UTC timestamps internally.
- Avoid logging secrets or full sensitive payloads.

### Incident

Fields:

- `id`: UUID
- `device_id`: device reference
- `type`: e.g. `high_latency`, `packet_loss`, `device_offline`, `high_cpu`
- `severity`: `low`, `medium`, `high`, `critical`
- `status`: `open`, `acknowledged`, `resolved`
- `title`, `description`
- `first_seen_at`, `last_seen_at`
- `occurrence_count`
- `created_at`, `updated_at`, `resolved_at`
- AI analysis status and analysis text, stored separately or in a related table

### PostgreSQL tables

Create migrations for:

1. `devices`
2. `telemetry_events` or a deduplicated event ledger
3. `telemetry_aggregates`
4. `incidents`
5. `incident_analyses`

Add appropriate indexes for device/time queries, incident status/severity, and event ID uniqueness. Avoid storing every high-volume raw event indefinitely; make retention configurable.

---

## 8. Kafka topics and delivery behavior

Create topics with documented partitioning and retention settings:

| Topic | Purpose |
|---|---|
| `network-telemetry` | Valid telemetry events |
| `network-telemetry-dlq` | Invalid or repeatedly failing events |
| `incident-events` | Incident created/updated events |
| `incident-analysis-requests` | Requests for AI analysis |
| `incident-analysis-dlq` | Failed AI analysis requests |

Requirements:

- Use `device_id` as the message key for telemetry to preserve per-device ordering within a partition.
- Include event IDs and schema versions.
- Configure consumer groups explicitly.
- Implement bounded retries with backoff for transient failures.
- Route poison messages to a DLQ with failure reason and original event metadata.
- Make handlers idempotent because Kafka delivery and retries can result in duplicate processing.
- Commit offsets only after the corresponding processing has been safely persisted or otherwise handled.
- Document delivery semantics and the limitations of exactly-once processing.
- Handle graceful shutdown and in-flight messages.
- Expose consumer lag where available.

For the MVP, use at-least-once delivery plus idempotent database writes. Do not claim end-to-end exactly-once guarantees.

---

## 9. Anomaly detection and incident lifecycle

Implement deterministic, configurable rules first.

### Initial rules

1. **High latency:** latency exceeds a configurable threshold for a configurable number of observations.
2. **Packet loss:** packet loss exceeds a configurable percentage.
3. **High bandwidth utilization:** utilization exceeds a configurable threshold if device capacity is configured.
4. **High CPU or memory:** resource usage exceeds a configured threshold.
5. **Device offline:** no heartbeat received within a configurable timeout.
6. **Repeated instability:** a device repeatedly transitions between healthy and degraded states within a time window.

### Detection behavior

- Use a rolling window or time-bucketed aggregates.
- Avoid firing an incident for one isolated noisy sample unless explicitly configured.
- Apply a cooldown and deduplication key such as `device_id + incident_type`.
- Update `last_seen_at` and `occurrence_count` for an existing active incident.
- Resolve an incident after the metric returns to normal for a configurable recovery window, or allow manual resolution.
- Keep severity thresholds configurable through environment variables or configuration files.
- Add unit tests for threshold boundaries, cooldown, recovery, and duplicate events.

Do not use the LLM to determine whether an incident exists or to assign authoritative severity.

---

## 10. Redis caching requirements

Use Redis for short-lived, frequently accessed state—not as the source of truth.

Suggested keys:

- `device:{device_id}:latest` — latest telemetry and computed status
- `device:{device_id}:health` — current health summary
- `incident:{incident_id}:analysis-status` — optional short-lived analysis status

Requirements:

- Use TTLs for cache entries.
- Update PostgreSQL and Redis with clear handling for partial failures.
- Treat cache misses as normal and fall back to PostgreSQL.
- Do not make correctness depend solely on Redis.
- Document key formats, TTLs, invalidation, and failure behavior.
- Add tests for cache miss, cache hit, expiration, and Redis unavailability.

---

## 11. AI incident analysis

### Purpose

Given an already-created incident and a bounded set of recent telemetry aggregates, produce:

- A concise incident summary
- Observed facts and relevant metric changes
- Plausible causes clearly labeled as hypotheses
- A short list of troubleshooting steps
- A confidence or uncertainty note

### Safety and correctness requirements

- The LLM must not create, resolve, or assign authoritative severity to incidents.
- Send only the minimum necessary telemetry context to the provider.
- Treat telemetry fields and device-provided strings as untrusted data, not instructions.
- Require structured JSON output and validate it before storing.
- Set request timeouts, token/output limits, and bounded retries.
- Handle provider timeouts, rate limits, invalid JSON, and unavailable credentials gracefully.
- If AI is unavailable, the incident and dashboard must continue to work, showing a clear `analysis unavailable` or `pending` state.
- Cache or deduplicate analysis requests to avoid repeated charges.
- Never expose the provider API key to the browser.
- Do not claim the AI has found a confirmed root cause. Use wording such as “possible cause” or “investigation suggestion.”

### Provider interface

Define a small interface so the application can use an OpenAI-compatible endpoint or another provider without changing incident logic. Read the API base URL, model, and API key from environment variables.

Provide a deterministic mock provider for local development and tests. The project must run without an AI API key.

---

## 12. REST API requirements

Prefix endpoints with `/api/v1`.

### Health and observability

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness, including required dependencies |
| `GET` | `/metrics` | Prometheus metrics |

### Devices and telemetry

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/api/v1/devices` | List devices; support pagination and status filter |
| `POST` | `/api/v1/devices` | Register a device |
| `GET` | `/api/v1/devices/{id}` | Device details and current health |
| `GET` | `/api/v1/devices/{id}/telemetry` | Query telemetry by time range |
| `POST` | `/api/v1/telemetry` | Validate and publish telemetry event(s) |

### Incidents

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/api/v1/incidents` | List incidents; filter by status, severity, device, and time |
| `GET` | `/api/v1/incidents/{id}` | Incident details and analysis |
| `POST` | `/api/v1/incidents/{id}/acknowledge` | Acknowledge an incident |
| `POST` | `/api/v1/incidents/{id}/resolve` | Resolve an incident |
| `POST` | `/api/v1/incidents/{id}/analyze` | Request or retry AI analysis |

API requirements:

- Validate input and return consistent JSON errors.
- Use appropriate HTTP status codes.
- Support pagination and bounded time ranges.
- Add request IDs and structured request logging.
- Add CORS configuration for the deployed frontend origin.
- Add rate limiting to public write endpoints if exposed publicly.
- Generate and maintain an OpenAPI specification.
- Document request/response examples.

---

## 13. Frontend dashboard

Build a small, clean, responsive dashboard. Avoid spending excessive time on visual polish.

### Required screens

1. **Overview**
   - Total devices, online/degraded/offline counts
   - Open incidents by severity
   - Recent incident feed
   - Basic latency and packet-loss trends

2. **Devices**
   - Device list with status, location, last seen, and health
   - Device detail page with recent telemetry charts and active incidents

3. **Incidents**
   - Filterable incident list
   - Severity, status, device, first seen, and last seen
   - Incident details with timeline, relevant metrics, and AI analysis
   - Acknowledge and resolve actions

4. **Demo controls**
   - Start/stop or configure the simulator through a safe development-only control, or document simulator commands separately.
   - Provide a clear way to trigger a demo anomaly.

### UI requirements

- Loading, empty, and error states.
- Human-readable timestamps and units.
- Accessible labels and keyboard-friendly controls.
- No API secrets in frontend environment variables.
- Do not hardcode fake live data in production views; demo data must come from the backend.

---

## 14. Telemetry simulator and demo scenario

Create a Go simulator that can generate telemetry for a configurable number of devices at a configurable interval.

### Simulator requirements

- Configurable device count, event interval, and random seed.
- Normal baseline metrics with controlled variation.
- Named scenarios:
  - `normal`
  - `latency-spike`
  - `packet-loss`
  - `device-offline`
  - `high-cpu`
- Ability to trigger a scenario for a chosen device and duration.
- Publish events through the same ingestion path used by the application.
- Print a concise summary of events sent and errors.
- Avoid unbounded goroutine creation and support graceful shutdown.

### Demo acceptance scenario

1. Start the full local stack.
2. Seed at least 10 devices.
3. Run the simulator in normal mode.
4. Trigger a latency spike for one device.
5. Confirm telemetry is ingested and persisted.
6. Confirm a single deduplicated incident is created and its occurrence count updates.
7. Confirm the device status and incident appear in the dashboard.
8. Confirm AI analysis appears when credentials are configured, or a clear fallback status appears without credentials.
9. Restore normal telemetry and confirm recovery behavior.
10. Confirm Prometheus metrics and Grafana panels show activity.

---

## 15. Observability

Instrument API and worker services with Prometheus metrics. Use bounded-cardinality labels; do not label metrics with arbitrary device IDs or raw error strings.

Suggested metrics:

- `netpulse_telemetry_events_received_total`
- `netpulse_telemetry_events_processed_total`
- `netpulse_telemetry_events_failed_total`
- `netpulse_telemetry_processing_duration_seconds`
- `netpulse_incidents_created_total`
- `netpulse_incidents_resolved_total`
- `netpulse_ai_analysis_requests_total`
- `netpulse_ai_analysis_failures_total`
- `netpulse_ai_analysis_duration_seconds`
- `netpulse_kafka_publish_failures_total`
- `netpulse_cache_operations_total`

Include a Grafana dashboard with panels for:

- Telemetry ingestion and processing rate
- Processing latency
- Failed events and DLQ activity
- Open incidents by severity
- AI analysis success/failure and duration
- Service health

Include useful structured logs with service name, request ID, event ID, and incident ID where applicable.

---

## 16. Configuration and environment variables

Provide a complete `.env.example` with safe placeholders. Validate required settings at startup and print actionable errors.

Suggested variables:

```dotenv
APP_ENV=local
API_ADDR=:8080
LOG_LEVEL=info

DATABASE_URL=postgres://netpulse:netpulse@postgres:5432/netpulse?sslmode=disable
REDIS_ADDR=redis:6379
REDIS_PASSWORD=
REDIS_DB=0

KAFKA_BROKERS=kafka:9092
KAFKA_TELEMETRY_TOPIC=network-telemetry
KAFKA_TELEMETRY_DLQ_TOPIC=network-telemetry-dlq
KAFKA_INCIDENT_TOPIC=incident-events
KAFKA_ANALYSIS_TOPIC=incident-analysis-requests
KAFKA_CONSUMER_GROUP=netpulse-worker

TELEMETRY_INTERVAL=2s
DEVICE_OFFLINE_TIMEOUT=60s
LATENCY_THRESHOLD_MS=150
PACKET_LOSS_THRESHOLD_PCT=5
CPU_THRESHOLD_PCT=90
MEMORY_THRESHOLD_PCT=90
ANOMALY_WINDOW=60s
ANOMALY_MIN_SAMPLES=5
INCIDENT_COOLDOWN=5m
RECOVERY_WINDOW=2m

AI_PROVIDER=openai-compatible
AI_BASE_URL=
AI_API_KEY=
AI_MODEL=
AI_TIMEOUT=20s
AI_ENABLED=false

PUBSUB_ENABLED=false
PUBSUB_PROJECT_ID=
PUBSUB_SUBSCRIPTION=
PUBSUB_TOPIC=
```

The application should work locally with the AI provider disabled. Use separate environment files or deployment secret configuration for cloud environments.

---

## 17. Local development and Docker Compose

Provide a Docker Compose stack for:

- API
- Worker
- Simulator (optional profile or separate command)
- PostgreSQL
- Redis
- Kafka in a local single-node development configuration
- Prometheus
- Grafana
- Frontend

Requirements:

- Add health checks and dependency readiness handling.
- Use named volumes for local persistence.
- Make startup deterministic and document any Kafka initialization steps.
- Expose only necessary ports.
- Provide a one-command startup path and a one-command shutdown path.
- Include database migration and seed commands.
- Keep development credentials clearly labeled as local-only.
- Ensure `docker compose down -v` is documented as destructive to local persisted data.

Provide a Makefile with commands such as:

```bash
make setup
make up
make down
make logs
make migrate
make seed
make test
make lint
make demo
make clean
```

Commands must match the actual implementation.

---

## 18. Testing and quality requirements

### Unit tests

Cover:

- Telemetry validation and schema handling
- Anomaly thresholds and rolling windows
- Incident creation, deduplication, acknowledgement, and resolution
- Retry/backoff decisions
- AI response parsing and invalid output
- Cache behavior and fallback logic

### Integration tests

Cover:

- API + PostgreSQL persistence
- Kafka publish/consume flow
- Duplicate event handling
- DLQ behavior
- Redis unavailable/cache miss behavior
- Incident lifecycle
- AI provider mock behavior

Use test containers or a documented local dependency stack where appropriate. Tests must not require paid external APIs.

### Quality gates

- `gofmt` and `go vet`
- Go unit and integration tests
- Frontend lint/build/tests
- Dependency and secret hygiene
- CI workflow that runs checks on pull requests

Document any tests that require Docker or other local services.

---

## 19. Deployment plan

Provide a practical, reproducible deployment path and document the exact services, settings, and costs/limitations.

### Local deployment — mandatory

The project must run locally using Docker Compose with no cloud account and no AI API key.

### Cloud deployment — required deliverable

Use **Google Cloud** as the documented target, while keeping the application portable.

Recommended deployment approach:

- Deploy the Go API and worker as separate containerized services where the chosen hosting platform supports long-running workers.
- Use managed PostgreSQL and Redis, or clearly document a suitable hosted alternative.
- Use a managed Kafka provider for the Kafka path, with TLS/SASL credentials stored in a secret manager.
- If implementing Pub/Sub, document the subscription and IAM configuration separately.
- Deploy the frontend to a static hosting service or a suitable managed frontend platform.
- Configure HTTPS, CORS, health checks, resource limits, and logging.
- Keep secrets out of source control and container images.
- Provide deployment scripts or infrastructure configuration where practical.

**Important deployment considerations:**

- A continuously running Kafka consumer may not fit a scale-to-zero request-driven runtime. Select a runtime that supports background workers or explain and implement a suitable alternative.
- Do not assume a managed Kafka service is free. Include a cost-conscious development option and explain which resources may incur charges.
- Do not expose PostgreSQL, Redis, or Kafka publicly without appropriate network and authentication controls.
- Document teardown steps to avoid ongoing cloud charges.
- If cloud credentials are unavailable, finish the deployment configuration and provide exact manual deployment steps. Never claim a cloud deployment succeeded without verification.

### Deployment documentation must include

1. Prerequisites and account setup.
2. Required APIs/services and permissions.
3. Environment variables and secret setup.
4. Database creation and migrations.
5. Kafka topic creation and authentication.
6. Build and deploy commands.
7. Frontend configuration and deployment.
8. Smoke tests and troubleshooting.
9. Rollback procedure.
10. Resource cleanup and cost warnings.

---

## 20. Security and reliability

- Validate all inbound data.
- Apply request size limits and timeouts.
- Use parameterized SQL.
- Keep credentials in environment variables or a secret manager.
- Configure CORS narrowly.
- Avoid exposing internal error details to clients.
- Add graceful shutdown and context propagation.
- Use bounded retries with exponential backoff and jitter.
- Avoid retrying permanent validation failures indefinitely.
- Ensure duplicate events do not create duplicate incidents.
- Add retention policies for telemetry and logs.
- Ensure AI output is treated as untrusted content and safely rendered in the UI.
- Do not include secrets, private IP inventories, or sensitive payloads in AI prompts.
- Clearly distinguish observed telemetry from AI-generated hypotheses.

Authentication may be deferred for a local-only demo, but any publicly exposed write or administrative endpoint must be protected or restricted.

---

## 21. README and project documentation

Write a polished README containing:

- Project overview and problem statement
- Feature list
- Architecture diagram (Mermaid)
- Tech stack
- Prerequisites
- Local setup and quick start
- Environment variable reference
- API documentation link
- Example telemetry event
- Demo scenario and screenshots placeholders
- Testing commands
- Observability and dashboard access
- Deployment instructions
- Known limitations
- Security notes
- Future improvements

Also create:

- `docs/architecture.md`
- `docs/api.md`
- `docs/deployment.md`
- OpenAPI specification
- Mermaid sequence diagram for telemetry ingestion and incident creation

Do not include fabricated screenshots, benchmark numbers, or deployment claims.

---

## 22. Definition of done

The project is complete when all required items below are satisfied:

- [ ] A fresh checkout can be configured using documented instructions.
- [ ] `docker compose up` (or the documented equivalent) starts the local stack.
- [ ] Database migrations run successfully.
- [ ] The simulator generates valid telemetry.
- [ ] Telemetry flows through Kafka and is processed by the Go worker.
- [ ] PostgreSQL stores devices, aggregates, and incidents.
- [ ] Redis caches latest device health with a documented TTL.
- [ ] Anomaly rules create and deduplicate incidents correctly.
- [ ] Incident acknowledgement, resolution, and recovery work.
- [ ] AI analysis works with a configured provider and degrades gracefully without one.
- [ ] Dashboard displays real API data and handles loading/error/empty states.
- [ ] Prometheus metrics and Grafana dashboard are usable.
- [ ] Unit tests and applicable integration tests pass.
- [ ] CI workflow is configured.
- [ ] README, API docs, architecture docs, and deployment guide are complete.
- [ ] Cloud deployment configuration is provided and verified if credentials are available.
- [ ] No secrets or fabricated metrics are committed.

---

## 23. Implementation order

Build in this order and keep the repository runnable after each phase:

1. Repository setup, configuration, logging, health endpoints, and Docker Compose.
2. PostgreSQL schema, migrations, device model, and seed data.
3. Telemetry schema, ingestion API, Kafka producer, and simulator.
4. Kafka consumer, validation, idempotent persistence, and Redis cache.
5. Aggregation, anomaly detection, and incident lifecycle.
6. Incident and device REST APIs with OpenAPI documentation.
7. AI provider interface, mock provider, and incident analysis worker.
8. Prometheus metrics and Grafana dashboard.
9. React dashboard and demo controls.
10. Unit/integration tests, CI, performance/load-test script, and README.
11. Cloud deployment configuration, smoke tests, and teardown documentation.

At the end of each phase, run relevant formatting, tests, and build commands. Fix failures before moving forward.

---

## 24. Final response expected from the coding agent

When implementation is finished, report:

1. What was implemented.
2. Repository structure and key architectural decisions.
3. Exact local setup and demo commands.
4. Test commands and actual results.
5. How to configure AI analysis.
6. Deployment target, what was deployed, and what remains manual.
7. Any known limitations or unimplemented requirements.
8. A short list of suggested next improvements.

**Start by inspecting the repository, then implement the MVP in the order above. Do not stop after writing a plan; build the working application.**
