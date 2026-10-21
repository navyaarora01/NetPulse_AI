#!/bin/sh
# Run on the provisioned VM after installing Docker Compose and gcloud.
set -eu
: "${PROJECT_ID:?Set PROJECT_ID}"
: "${REGION:?Set REGION}"
: "${SECRET_VERSION:?Pin SECRET_VERSION to a numeric version}"
: "${APP_IMAGE:?Set APP_IMAGE}"
: "${WEB_IMAGE:?Set WEB_IMAGE}"
: "${DOMAIN:?Set DOMAIN}"
case "$SECRET_VERSION" in *[!0-9]*|'') echo 'SECRET_VERSION must be numeric' >&2; exit 1;; esac
umask 077
sudo install -d -m 700 -o "$(id -u)" -g "$(id -g)" /opt/netpulse
secret_file=$(mktemp)
trap 'rm -f "$secret_file"' EXIT HUP INT TERM
gcloud secrets versions access "$SECRET_VERSION" --project "$PROJECT_ID" --secret netpulse-runtime --out-file "$secret_file"
sudo install -m 600 -o "$(id -u)" -g "$(id -g)" "$secret_file" /opt/netpulse/runtime.env
# Docker and gcloud must run under the same authenticated user.
gcloud auth configure-docker "$REGION-docker.pkg.dev" --quiet
docker compose -f deploy/cloud/compose.yml pull
case ",${COMPOSE_PROFILES:-}," in *,sqlproxy,*) docker compose -f deploy/cloud/compose.yml up -d sqlproxy;; esac
docker compose -f deploy/cloud/compose.yml run --rm migrate
docker compose -f deploy/cloud/compose.yml up -d --wait
