#!/bin/sh
set -eu
docker compose up -d --wait postgres redis kafka
docker compose run --rm kafka-init
exists=$(docker compose exec -T postgres psql -U netpulse -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='netpulse_test'")
if [ "$exists" != 1 ]; then docker compose exec -T postgres createdb -U netpulse netpulse_test; fi
docker compose run --rm integration
