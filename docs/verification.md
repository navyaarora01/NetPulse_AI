# Verification record

Implementation environment: macOS arm64. Project-local Go 1.27.1 and Node.js 22.23.3 were downloaded from their official distributions and SHA-256 hashes were checked against vendor release metadata. They live under ignored `.tools/`; they are not project dependencies or deployment artifacts.

## Checks executed

- Go backend unit tests: pass, including telemetry validation, anomaly boundaries/windows, config validation, API behavior/auth/CORS, retries, provider JSON validation/timeout/failure and cache hit/miss/TTL/stale-write/outage behavior.
- All four Go executables: build successfully with `CGO_ENABLED=0`.
- `go vet`: passes.
- `gofmt`: clean.
- Frontend ESLint: passes.
- Frontend Vitest: 5 tests pass.
- Frontend Vite production build: passes.
- npm install audit: zero reported vulnerabilities after updating Vite and Vitest.
- Integration suite: compiles under `-tags=integration`; **not executed against infrastructure**.
- YAML files: parsed successfully for syntax (not a substitute for `docker compose config`).
- OpenAPI document: JSON structure parsed; 13 endpoint paths present.
- Shell scripts: syntax checked.

Commands used for backend checks include `CGO_ENABLED=0` because Apple's C developer tools are not installed. Tests using mock HTTP/Redis servers needed permission to bind local ports outside the execution sandbox. No paid provider calls were made.

## Remaining verification

Docker is not installed in this environment. Therefore Compose startup, image builds, topic initialization, real PostgreSQL migrations, Kafka/DLQ integration, Redis integration, Grafana live queries and the full simulator acceptance walkthrough have **not** been run here. Use:

```sh
make up
make integration
sh scripts/smoke.sh
make demo
```

The CI workflow runs these infrastructure checks on Ubuntu with Docker. It has been authored but not pushed or executed in a hosted repository. No race-detector result is claimed; the race job is configured in CI and requires a C toolchain.

Browser automation reported no available browser connection. The frontend server started, but no rendered screenshot/visual inspection was performed. DOM tests cover empty/error states and chart accessibility, not complete visual or browser interaction coverage.

Cloud configuration has not been deployed; it requires your project, account, credentials, domain and a Kafka provider. No benchmark, production readiness certification, cloud availability, screenshot or successful deployment is claimed.

## Dependency review

The Go vulnerability scan initially found reachable advisories in pgx and `golang.org/x/text`. These were patched to pgx v5.9.2 and x/text v0.39.0. Non-reachable advisories were also patched with compress v1.18.7 and x/sys v0.44.0. The final `govulncheck` scan reported **No vulnerabilities found**. Regression tests, vet and executable builds passed with this final dependency set. This is a point-in-time source scan, not a container-image or infrastructure security audit.
