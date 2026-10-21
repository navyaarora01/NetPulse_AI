# REST API

Machine-readable contract: [openapi.json](openapi.json) (OpenAPI 3.1).

Base URL locally: `http://localhost:8080`. Dashboard proxies `/api/v1` on `http://localhost:5173`. API token authentication is optional locally; when `API_TOKEN` is set, **all** `/api` endpoints require `Authorization: Bearer …`. Health and metrics should stay private at the gateway.

| Method | Path | Behavior |
|---|---|---|
| GET | `/healthz` | Process liveness |
| GET | `/readyz` | Database and Kafka readiness |
| GET | `/metrics` | Prometheus text |
| GET | `/api/v1/summary` | Global device counts and active incidents by severity |
| GET/POST | `/api/v1/devices` | Paginated devices / register device |
| GET | `/api/v1/devices/{id}` | Current cached health, database fallback |
| GET | `/api/v1/devices/{id}/telemetry` | Minute-average telemetry |
| POST | `/api/v1/telemetry` | Publish one event or array of 1–100 events |
| GET | `/api/v1/incidents` | Filterable paginated incidents |
| GET | `/api/v1/incidents/{id}` | Incident and analysis |
| POST | `/api/v1/incidents/{id}/acknowledge` | Open → acknowledged |
| POST | `/api/v1/incidents/{id}/resolve` | Active → resolved |
| POST | `/api/v1/incidents/{id}/analyze` | Request failed/unavailable analysis; others deduplicate |

Lists return `{ "items": [], "limit": 50, "offset": 0 }`. Telemetry returns `{ "items": [] }`. Pagination defaults to limit 50, offset 0; maximum limit 200, offset 100000. Lists have deterministic ordering. Offset pagination can shift under concurrent inserts; use incident IDs when investigating.

Device list filter: `status=online|degraded|offline`. Incident filters: `status=open|acknowledged|resolved`, `severity=low|medium|high|critical`, `device_id=UUID`, `from`, `to`. Incident time filters use creation time. Telemetry and incident ranges default to the last 24 hours; positive ranges are capped at 24 hours. Use RFC3339 timestamps with URL encoding for offsets. Older records can be inspected by choosing an older range; the dashboard defaults to the last 24 hours.

```sh
curl -sS -X POST http://localhost:8080/api/v1/devices \
  -H 'Content-Type: application/json' \
  -d '{"name":"router-001","location":"Mumbai","capacity_mbps":1000}'
```

Copy the returned UUID into `device_id`. The simulator does this automatically. Each event ID must be a new UUID, but **retries must reuse the original event IDs**.

```json
{
  "event_id": "e5296d63-3f4a-4470-90c6-83ea8ca105f0",
  "schema_version": 1,
  "device_id": "7b9e9dce-36b4-441c-8463-76d137b7cb61",
  "timestamp": "2026-09-29T10:30:00Z",
  "metrics": {
    "latency_ms": 42.5,
    "packet_loss_pct": 0.4,
    "bandwidth_mbps": 125,
    "cpu_usage_pct": 54,
    "memory_usage_pct": 63,
    "signal_strength_dbm": -67
  }
}
```

Replace the example timestamp with current UTC. Events must be within the last 24 hours and no more than one minute in the future. Numeric bounds: latency 0–60000 ms; percentages 0–100; bandwidth 0–10000000 Mbps; signal -200–0 dBm. All measurements are required. `capacity_mbps=0` disables utilization detection.

Telemetry returns 202 only after all broker acknowledgements. This confirms ingestion, **not** completed processing. A publish interruption can partially accept a batch; retrying all original IDs is safe. Validation checks the whole batch before any publication. Invalid JSON/ranges produce 400, oversized bodies 413, unknown registered resources 404, authentication 401, invalid transitions 409, rate limit 429, dependency failure 503. No internal database or provider details are returned.

Errors have this shape, and every response has an `X-Request-ID` header:

```json
{"error":{"code":"invalid_event","message":"schema_version must be 1","request_id":"..."}}
```

Acknowledgement/resolution are idempotent for the same state. A resolved incident cannot be acknowledged. Analysis results contain `summary`, `facts`, `hypotheses`, `steps`, and `uncertainty`. Statuses are pending, processing, completed, unavailable, or failed. A disabled provider remains unavailable when requested; enable it and request again.
