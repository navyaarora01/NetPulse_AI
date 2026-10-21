#!/bin/sh
set -eu
curl --fail --silent http://localhost:8080/readyz
curl --fail --silent http://localhost:5173/ >/dev/null
curl --fail --silent http://localhost:8080/metrics | grep -q netpulse_telemetry_events_received_total
docker compose run --rm simulator -url http://api:8080 -seed-only
