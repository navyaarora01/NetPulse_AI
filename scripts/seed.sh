#!/bin/sh
set -eu
docker compose run --rm simulator -url http://api:8080 -seed-only
