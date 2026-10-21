import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync, renameSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

// Export persisted state without restarting the API, worker, or simulator.
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const output = resolve(root, process.argv[2] || "backups/snapshot.json");
function compose(args, options = {}) {
  return execFileSync("docker", ["compose", ...args], {
    cwd: root, encoding: "utf8", maxBuffer: 256 * 1024 * 1024, ...options,
  });
}
function query(sql) {
  return JSON.parse(compose(["exec", "-T", "postgres", "psql", "-U", "netpulse",
    "-d", "netpulse", "-tAX", "-v", "ON_ERROR_STOP=1", "-c", sql]));
}
const wasRunning = compose(["ps", "--status", "running", "-q", "postgres"]).trim() !== "";
try {
  if (!wasRunning) compose(["up", "-d", "--wait", "postgres"], { stdio: "inherit" });
  mkdirSync(resolve(root, "backups"), { recursive: true });
  const stamp = new Date().toISOString().replaceAll(":", "-");
  const backup = resolve(root, "backups", `netpulse-${stamp}.dump`);
  const archive = compose(["exec", "-T", "postgres", "pg_dump", "-U", "netpulse",
    "-d", "netpulse", "--format=custom", "--no-owner", "--no-acl"], { encoding: "buffer" });
  writeFileSync(backup, archive, { mode: 0o600, flag: "wx" });

  const devices = query(`SELECT COALESCE(jsonb_agg(jsonb_build_object(
    'id',id,'name',name,'location',location,'status',status,'capacity_mbps',capacity_mbps,
    'created_at',created_at,'updated_at',updated_at,'last_seen_at',last_seen_at,'latest',latest)
    ORDER BY name),'[]') FROM devices`);
  const incidents = query(`SELECT COALESCE(jsonb_agg(jsonb_build_object(
    'id',i.id,'device_id',i.device_id,'type',i.type,'severity',i.severity,'status',i.status,
    'title',i.title,'description',i.description,'first_seen_at',i.first_seen_at,
    'last_seen_at',i.last_seen_at,'occurrence_count',i.occurrence_count,
    'created_at',i.created_at,'updated_at',i.updated_at,'resolved_at',i.resolved_at,
    'analysis_status',a.status,'analysis',a.result) ORDER BY i.created_at DESC,i.id),'[]')
    FROM incidents i LEFT JOIN incident_analyses a ON a.incident_id=i.id`);
  const rows = query(`SELECT COALESCE(jsonb_agg(jsonb_build_object(
    'device_id',device_id,'timestamp',bucket,'samples',samples,'metrics',jsonb_build_object(
    'latency_ms',latency/samples,'packet_loss_pct',loss/samples,
    'bandwidth_mbps',bandwidth/samples,'cpu_usage_pct',cpu/samples,
    'memory_usage_pct',memory/samples,'signal_strength_dbm',signal/samples))
    ORDER BY bucket),'[]') FROM telemetry_aggregates`);
  const telemetry = Object.fromEntries(devices.map(d => [d.id, []]));
  for (const {device_id, ...row} of rows) telemetry[device_id].push(row);
  const summary = {
    devices: { total: devices.length, online: 0, degraded: 0, offline: 0 },
    incidents: { low: 0, medium: 0, high: 0, critical: 0 },
  };
  for (const d of devices) summary.devices[d.status]++;
  for (const i of incidents) if (i.status !== "resolved") summary.incidents[i.severity]++;
  const snapshot = { schema_version: 1, captured_at: new Date().toISOString(),
    latest_telemetry_at: devices.map(d => d.last_seen_at).filter(Boolean).sort().at(-1) || null,
    devices, incidents, telemetry, summary };
  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(output + ".tmp", JSON.stringify(snapshot), { mode: 0o600 });
  renameSync(output + ".tmp", output);
  console.log(JSON.stringify({ output, backup, devices: devices.length,
    incidents: incidents.length, telemetryBuckets: rows.length }));
} finally {
  if (!wasRunning) compose(["stop", "postgres"], { stdio: "inherit" });
}
