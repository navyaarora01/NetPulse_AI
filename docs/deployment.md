# Deployment guide

## Verification status

No cloud resources have been deployed. No cloud credentials, project ID or domain were supplied. Docker is not installed on the implementation machine, so local Compose execution, migrations against a running database and the full acceptance scenario remain unverified there. CI and `make integration` provide the verification path; do not treat configuration files as evidence of a successful deployment.

## Local deployment

Install Docker Desktop (or Docker Engine with Compose v2.24+). Allocate at least 4 CPU cores and 6 GB memory for Kafka, PostgreSQL, monitoring and app builds.

```sh
make setup
make up
make seed
make demo
```

Open the dashboard at http://localhost:5173, Grafana at http://localhost:3000, and Prometheus at http://localhost:9090. Grafana local-only login: `admin` / `local-demo-change-me`. API: http://localhost:8080.

`make up` builds images, waits for PostgreSQL and Kafka, runs topic initialization and migrations, then starts app services. Topics are never implicitly created. Redis is only a cache but Compose waits for its initial startup. A later Redis outage does not block the app. `.env.example` lists settings for host development; Compose overrides infrastructure hostnames to container names. Keep the default topic names for this Compose stack or update `kafka-init` in tandem.

Use `make logs`, `docker compose ps` and `/readyz` for diagnosis. Kafka can take a minute to initialize. `make down` preserves named volumes. **`docker compose down -v` permanently deletes local PostgreSQL, Kafka, Redis and monitoring data.** `make clean` prints this command instead of deleting data automatically.

To verify recovery, run only one simulator. The demo creates normal traffic for 15 seconds, spikes latency for 90 seconds on router-001, then continues normal telemetry. With defaults, an incident appears after five breached samples and resolves after two continuous healthy minutes. Look for a single active high-latency incident whose occurrence count increases. With AI disabled, `analysis_status=unavailable` is the expected fallback.

## Google Cloud target

The concrete target is a Compute Engine VM running separate API and worker containers, an nginx frontend behind Caddy HTTPS, Cloud SQL PostgreSQL over private networking, Memorystore Redis over private networking, and a managed Kafka service supporting **SASL/PLAIN over TLS**. This avoids placing a continuously polling Kafka consumer in a request-driven, scale-to-zero service. It is a single-VM reference deployment, not highly available production infrastructure.

The alternate cost-conscious development option is running the local Compose stack on a private VM and accessing it via an SSH tunnel. Keep APP_ENV=local and loopback bindings only in that private demonstration setup; do not expose the local stack's unauthenticated ports.

### 1. Prerequisites and permissions

Install `gcloud`, Docker/Buildx, Kafka CLI tools and an authenticated billing-enabled Google Cloud account. Choose your own values; none below represent deployed resources.

```sh
export PROJECT_ID='your-project-id'
export REGION='asia-south1'
export ZONE='asia-south1-a'
export DOMAIN='your-dashboard.example.com'
export RELEASE='release-001'
gcloud auth login
gcloud config set project "$PROJECT_ID"
gcloud services enable compute.googleapis.com artifactregistry.googleapis.com \
  sqladmin.googleapis.com redis.googleapis.com secretmanager.googleapis.com \
  servicenetworking.googleapis.com
```

Provisioning requires appropriate Compute/network admin, Cloud SQL admin, Redis admin, Artifact Registry admin, Secret Manager admin and service-account creation/act-as permissions. Grant deployment operators only those privileges required by your organization's policy. The **runtime** service account needs `roles/artifactregistry.reader` and secret accessor on only the runtime secret; it does not need project editor or owner.

### 2. Network, database, Redis and VM

Create a dedicated VPC/subnet, private services connection and managed database. Check organization policy and regional availability before running creation commands.

```sh
gcloud compute networks create netpulse --subnet-mode=custom
gcloud compute networks subnets create netpulse-app --network=netpulse \
  --region="$REGION" --range=10.42.0.0/24
gcloud compute addresses create netpulse-services --global --purpose=VPC_PEERING \
  --prefix-length=16 --network=netpulse
gcloud services vpc-peerings connect --service=servicenetworking.googleapis.com \
  --ranges=netpulse-services --network=netpulse
gcloud sql instances create netpulse-db --database-version=POSTGRES_17 \
  --tier=db-custom-1-3840 --region="$REGION" --network=netpulse --no-assign-ip \
  --storage-size=20 --storage-auto-increase --backup-start-time=02:00
gcloud sql databases create netpulse --instance=netpulse-db
# Generate a unique password in your secret manager; supply it interactively.
gcloud sql users create netpulse --instance=netpulse-db --prompt-for-password
gcloud redis instances create netpulse-cache --size=1 --region="$REGION" \
  --redis-version=redis_7_2 --network=netpulse --connect-mode=PRIVATE_SERVICE_ACCESS

gcloud iam service-accounts create netpulse-runtime
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:netpulse-runtime@$PROJECT_ID.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.reader
gcloud compute firewall-rules create netpulse-https --network=netpulse \
  --allow=tcp:80,tcp:443 --source-ranges=0.0.0.0/0 --target-tags=netpulse-web
gcloud compute firewall-rules create netpulse-iap-ssh --network=netpulse \
  --allow=tcp:22 --source-ranges=35.235.240.0/20 --target-tags=netpulse-web
gcloud compute instances create netpulse-app --zone="$ZONE" --machine-type=e2-standard-2 \
  --subnet=netpulse-app --image-family=debian-12 --image-project=debian-cloud \
  --boot-disk-size=30GB --tags=netpulse-web \
  --service-account="netpulse-runtime@$PROJECT_ID.iam.gserviceaccount.com" \
  --scopes=https://www.googleapis.com/auth/cloud-platform
```

Permit only the VM's private address/subnet to reach database and cache ports using your VPC/service controls. Do not create public PostgreSQL, Redis or Kafka firewall rules. Point the domain A record at the VM's reserved static external address (reserve/promote it before use). Install Docker Engine and Compose using Docker's Debian instructions, plus the Google Cloud CLI on the VM. Enable Docker at boot and grant your operator access to its socket. Docker socket access is root-equivalent; restrict it.

Cloud SQL: configure server CA validation and encrypted connections. Use a mounted CA file with `sslmode=verify-full` and the matching hostname, or run the Cloud SQL Auth Proxy as an additional private-network container and use its internal hostname. The reference Compose file includes an optional `sqlproxy` profile for the latter; grant the runtime service account `roles/cloudsql.client` and supply `SQL_CONNECTION_NAME` before using it. Never use an unencrypted public database connection. Redis in this reference is private-network-only; cache transport TLS is not implemented.

### 3. Kafka

Provision a managed Kafka cluster with SASL/PLAIN credentials and TLS (e.g. a provider offering this mechanism). Ensure outbound VM access to its advertised broker addresses. Provider-specific cluster creation and costs are intentionally manual. OAuth-only clusters need another SASL adapter, which is not implemented.

Create a mode-600 Kafka client properties file outside the repository containing `security.protocol=SASL_SSL`, `sasl.mechanism=PLAIN` and your provider's JAAS login string. Then:

```sh
for topic in network-telemetry network-telemetry-dlq incident-events incident-analysis-requests incident-analysis-dlq; do
  kafka-topics.sh --bootstrap-server "$KAFKA_BROKERS" --command-config "$KAFKA_CLIENT_CONFIG" \
    --create --if-not-exists --topic "$topic" --partitions 3 --replication-factor 3 \
    --config retention.ms=86400000 --config min.insync.replicas=2
done
```

Assign produce permissions to all five topics, consume permission to telemetry, and group access for `netpulse-worker`. Limit DLQ access and document your replay procedure. Use provider monitoring or `kafka-consumer-groups.sh --describe --group netpulse-worker` to inspect lag.

### 4. Secrets and runtime configuration

Prepare `runtime.env` **outside the repository**, mode 600. Copy applicable settings from `.env.example`, then replace:

```dotenv
APP_ENV=production
DATABASE_URL=postgres://netpulse:URL_ENCODED_PASSWORD@sqlproxy:5432/netpulse?sslmode=disable
REDIS_ADDR=PRIVATE_REDIS_IP:6379
KAFKA_BROKERS=YOUR_TLS_BROKERS
KAFKA_TLS=true
KAFKA_USERNAME=YOUR_SASL_USERNAME
KAFKA_PASSWORD=YOUR_SASL_PASSWORD
API_TOKEN=GENERATE_AT_LEAST_32_RANDOM_CHARACTERS
CORS_ORIGIN=https://YOUR_DOMAIN
AI_ENABLED=false
```

The `sslmode=disable` example applies **only to the internal Auth Proxy hop**; the proxy encrypts/authenticates the Cloud SQL connection. For direct Cloud SQL, configure verified TLS instead. URL-encode database credentials. Never place API or provider keys in `VITE_*` variables. Runtime environment secrets are visible to VM/root/container administrators.

```sh
gcloud secrets create netpulse-runtime --replication-policy=automatic \
  --data-file=/secure/path/runtime.env
gcloud secrets add-iam-policy-binding netpulse-runtime \
  --member="serviceAccount:netpulse-runtime@$PROJECT_ID.iam.gserviceaccount.com" \
  --role=roles/secretmanager.secretAccessor
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:netpulse-runtime@$PROJECT_ID.iam.gserviceaccount.com" \
  --role=roles/cloudsql.client
```

### 5. Build, deploy and migrate

Build amd64 images for the documented VM, even when building on Apple Silicon:

```sh
gcloud artifacts repositories create netpulse --repository-format=docker --location="$REGION"
gcloud auth configure-docker "$REGION-docker.pkg.dev"
export APP_IMAGE="$REGION-docker.pkg.dev/$PROJECT_ID/netpulse/app:$RELEASE"
export WEB_IMAGE="$REGION-docker.pkg.dev/$PROJECT_ID/netpulse/web:$RELEASE"
docker buildx build --platform linux/amd64 -t "$APP_IMAGE" --push .
docker buildx build --platform linux/amd64 -t "$WEB_IMAGE" --push web
```

Copy `deploy/cloud` to the VM in a checkout-like directory structure, then export the same variables there plus `SECRET_VERSION=1`, `COMPOSE_PROFILES=sqlproxy` and `SQL_CONNECTION_NAME=PROJECT:REGION:INSTANCE`. Use immutable image digests for production releases. Run `sh deploy/cloud/deploy.sh`; it retrieves the pinned secret, pulls images, runs migrations and waits for app readiness. The `sqlproxy` service must be running before migration (the deployment script starts it when the profile is enabled). Runtime secrets are materialized at `/opt/netpulse/runtime.env` with mode 600.

The frontend has no deployment-time secret or API URL: nginx proxies to the private API container. Caddy obtains HTTPS for your domain after DNS and ports 80/443 are ready. No database, cache, broker or worker ports are exposed by the cloud Compose configuration. API `/healthz` and `/metrics` are internal; scrape through an internal monitoring agent.

### 6. Verification and troubleshooting

On the VM:

```sh
docker compose -f deploy/cloud/compose.yml ps
docker compose -f deploy/cloud/compose.yml exec api wget -qO- http://127.0.0.1:8080/readyz
curl --fail "https://$DOMAIN/"
curl --fail -H "Authorization: Bearer $API_TOKEN" "https://$DOMAIN/api/v1/summary"
```

Run the simulator locally against the HTTPS domain with `API_TOKEN` in its environment, or from the app image on the private Compose network. Use the acceptance scenario in the README. Check database rows, one deduplicated incident, recovery, cache TTL, and metrics; merely loading HTML is not an end-to-end test.

Readiness failure: inspect SQL connectivity, migration logs, Kafka DNS/advertised listeners and SASL credentials. AI failure: verify model structured-output support, base URL including `/v1`, provider limits and timeout; no provider response bodies or keys are logged. Clear failures remain visible in `analysis_status` and DLQ.

### 7. Rollback and cleanup

Record deployed image digests and secret version. Roll back by exporting the previous `APP_IMAGE`, `WEB_IMAGE`, and secret version, then rerun the deployment script. The initial migration is additive and versioned; there is no automatic down migration. For future schema changes, use expand/contract migrations and verified backups. Take a Cloud SQL backup before schema upgrades; a destructive schema rollback requires a maintenance window and restore plan.

To tear down, first stop cloud Compose. Delete the VM, reserved external IP, Redis instance, managed Kafka cluster, Cloud SQL instance (after required backup/export), unused Artifact Registry images/repository and secrets. Then remove firewall rules, service-account bindings, private-service connections/ranges, subnet and VPC when no longer used. Stopping the VM alone does **not** stop database, Redis, Kafka, disk, address or registry billing.

Cloud SQL, Memorystore, the VM/disk/IP, managed Kafka, registry storage, egress, secrets and any paid LLM API can incur charges. No free-tier or cost estimate is claimed. Set a billing budget and check the current calculator/provider quote for your region and retention requirements before provisioning.

References: [Compute Engine container deployment](https://docs.cloud.google.com/compute/docs/containers/deploying-containers), [Cloud SQL private networking](https://docs.cloud.google.com/sql/docs/postgres/configure-private-services-access), [Secret Manager access](https://docs.cloud.google.com/secret-manager/docs/access-secret-version). The target uses ordinary Docker containers on a VM rather than the deprecated container declaration deployment flow.
