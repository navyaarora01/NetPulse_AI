#!/bin/sh
set -eu
# Bounded load generator. Stop any existing simulator first.
docker compose run --rm simulator -url http://api:8080 -devices 100 -interval 200ms -scenario normal
