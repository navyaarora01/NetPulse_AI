import { cpSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const target = resolve(root, "sites/netpulse-snapshot");
const dataPath = resolve(root, process.argv[2] ||
  (existsSync(resolve(root, "backups/snapshot.json")) ? "backups/snapshot.json" : "web/public/snapshot.json"));
const data = JSON.parse(readFileSync(dataPath, "utf8"));
if (data.schema_version !== 1 || !Array.isArray(data.devices) || !data.telemetry)
  throw new Error("Expected an exported NetPulse snapshot.");
mkdirSync(target, { recursive: true });
mkdirSync(resolve(target, ".openai"), { recursive: true });
if (!existsSync(resolve(target, ".openai/hosting.json")))
  cpSync(resolve(root, "deploy/snapshot/hosting.json"), resolve(target, ".openai/hosting.json"));
for (const name of ["src", "public", "index.html", "vite.config.js", "eslint.config.js", ".npmrc", "package.json", "package-lock.json"]) {
  try { cpSync(resolve(root, "web", name), resolve(target, name), { recursive: true }); }
  catch (e) { if (e.code !== "ENOENT") throw e; }
}
const pkg = JSON.parse(readFileSync(resolve(target, "package.json"), "utf8"));
pkg.name = "netpulse-snapshot";
pkg.scripts.build = "VITE_SNAPSHOT_MODE=true vite build";
pkg.scripts.dev = "VITE_SNAPSHOT_MODE=true vite --host 127.0.0.1";
writeFileSync(resolve(target, "package.json"), JSON.stringify(pkg, null, 2) + "\n");
const lock = JSON.parse(readFileSync(resolve(target, "package-lock.json"), "utf8"));
lock.name = pkg.name;
lock.packages[""].name = pkg.name;
writeFileSync(resolve(target, "package-lock.json"), JSON.stringify(lock, null, 2) + "\n");
mkdirSync(resolve(target, "public"), { recursive: true });
cpSync(dataPath, resolve(target, "public/snapshot.json"));
writeFileSync(resolve(target, ".gitignore"), "node_modules/\ndist/\n.env*\n.sites*/\n");
writeFileSync(resolve(target, "README.md"), "# NetPulse AI snapshot\n\nRead-only captured telemetry from the local NetPulse demo. This static Site does not run Go, Kafka, Redis, or PostgreSQL.\n\nRun `npm ci` and `npm run build` to rebuild. The snapshot is public demonstration data; it contains no application credentials.\n");
console.log(JSON.stringify({ target, devices: data.devices.length, incidents: data.incidents.length }));
