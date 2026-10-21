CREATE TABLE IF NOT EXISTS devices (
 id uuid PRIMARY KEY, name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100), location text NOT NULL DEFAULT '',
 capacity_mbps double precision NOT NULL DEFAULT 0 CHECK(capacity_mbps>=0), status text NOT NULL DEFAULT 'offline' CHECK(status IN ('online','degraded','offline')),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), last_seen_at timestamptz, latest jsonb,
 healthy_since timestamptz
);
CREATE TABLE IF NOT EXISTS telemetry_events (
 event_id uuid PRIMARY KEY, device_id uuid NOT NULL REFERENCES devices(id), timestamp timestamptz NOT NULL, received_at timestamptz NOT NULL DEFAULT now(), metrics jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS telemetry_device_time ON telemetry_events(device_id,timestamp DESC);
CREATE INDEX IF NOT EXISTS telemetry_received ON telemetry_events(received_at);
CREATE TABLE IF NOT EXISTS telemetry_aggregates (
 device_id uuid NOT NULL REFERENCES devices(id), bucket timestamptz NOT NULL, samples bigint NOT NULL,
 latency double precision NOT NULL, loss double precision NOT NULL, bandwidth double precision NOT NULL, cpu double precision NOT NULL, memory double precision NOT NULL, signal double precision NOT NULL,
 PRIMARY KEY(device_id,bucket)
);
CREATE TABLE IF NOT EXISTS incidents (
 id uuid PRIMARY KEY, device_id uuid NOT NULL REFERENCES devices(id), type text NOT NULL, severity text NOT NULL CHECK(severity IN ('low','medium','high','critical')),
 status text NOT NULL DEFAULT 'open' CHECK(status IN ('open','acknowledged','resolved')),title text NOT NULL, description text NOT NULL,
 first_seen_at timestamptz NOT NULL, last_seen_at timestamptz NOT NULL, occurrence_count integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),resolved_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS incident_active ON incidents(device_id,type) WHERE status <> 'resolved';
CREATE INDEX IF NOT EXISTS incident_filters ON incidents(status,severity,created_at DESC);
CREATE INDEX IF NOT EXISTS incident_device ON incidents(device_id,created_at DESC);
CREATE TABLE IF NOT EXISTS incident_analyses (
 incident_id uuid PRIMARY KEY REFERENCES incidents(id),status text NOT NULL CHECK(status IN ('pending','processing','completed','unavailable','failed')),
 result jsonb, attempts integer NOT NULL DEFAULT 0, updated_at timestamptz NOT NULL DEFAULT now(), lease_until timestamptz, generation integer NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS device_transitions(device_id uuid NOT NULL REFERENCES devices(id),timestamp timestamptz NOT NULL,status text NOT NULL);
CREATE INDEX IF NOT EXISTS transitions_device_time ON device_transitions(device_id,timestamp);
-- Transactional outbox closes the database-write / Kafka-publish failure gap.
CREATE TABLE IF NOT EXISTS outbox(id bigserial PRIMARY KEY,topic text NOT NULL,key text NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
