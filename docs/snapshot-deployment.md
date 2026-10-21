# Public snapshot deployment

The public demo is a hosted, read-only snapshot. Viewing it needs no hosting
account or card. The full Go/Kafka/PostgreSQL/Redis application remains available locally;
it is not running on the static host.

Public URL: https://netpulse-ai-network-demo.swift-emu-6996.chatgpt.site

Published with Sites on 21 October 2026. Access is public: anyone with the URL can
view it without an operator token. The captured data contains ten simulated
routers, three incidents and 180 minute-level telemetry records. The latest
telemetry was recorded on 21 October 2026 at 13:39:42 IST.

## What visitors can do

Visitors can select devices, inspect telemetry charts, filter incident history
and open incident details. All data is frozen at the recorded session. The Site
labels this explicitly, disables polling, and omits mutation controls. It does
not generate new telemetry, accept ingestion, acknowledge or resolve incidents,
or call AI providers.

## Preserved local data

The local simulator and application containers are stopped. Docker named volumes
for PostgreSQL, Kafka, Redis, Prometheus and Grafana remain intact. A separate
PostgreSQL custom-format backup is saved under the ignored `backups/` directory:

`backups/netpulse-2026-10-21T08-14-33.981Z.dump`

The archive's table-of-contents was checked with `pg_restore --list`. This checks
readability, not a complete restoration rehearsal. The static deployment does
not include the database backup, raw event ledger, credentials or environment
files; it publishes only the selected dashboard data in `snapshot.json`.

## Refresh the captured data

The repository includes the published demonstration data in
`web/public/snapshot.json` and the Site identity in `deploy/snapshot/hosting.json`.
The generated `sites/` checkout, private backups and local environment files are
excluded from Git. From a fresh clone, build the existing snapshot without Docker:

```sh
cd web
npm ci
npm run build:snapshot
```

The output is `web/dist/`; serve it with any static host. The standard
`npm run build` still builds the dashboard for the live backend.

From the repository root, first stop the simulator and app stack so the export
represents a fixed session. Do not remove volumes.

```sh
docker stop netpulse-local-demo
docker compose down
node scripts/export-snapshot.mjs
node scripts/prepare-snapshot-site.mjs
```

The exporter briefly starts only PostgreSQL when needed, writes a new private
backup plus `backups/snapshot.json`, then stops PostgreSQL again. The preparation
script copies the frontend source and public snapshot into
`sites/netpulse-snapshot/`, preserving its Sites identity. Without a local export,
it uses the checked-in public snapshot. Rebuild that Site with
`npm ci` and `npm run build` from its directory. Publishing an update uses the
Sites workflow for the project ID in its `.openai/hosting.json`; do not create a
second Site or upload the database backup.

## Verification

- Frontend lint and all eight tests pass, including snapshot identity, filtering,
  pagination and mutation rejection checks.
- Both the normal frontend build and the snapshot build pass.
- Chromium verified ten devices, three incidents, chart rendering, incident
  details, absence of write controls and a 390 px mobile overview without page
  overflow. No JavaScript errors or backend API requests occurred.
- Sites deployment status returned `succeeded`, with public access confirmed.

This is a demonstration export of simulated network data. A future independent
live deployment still requires hosting the backend services and persistent data.
