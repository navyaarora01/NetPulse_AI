import { useCallback, useEffect, useState } from "react";
import {
  Activity,
  LayoutDashboard,
  Router,
  Siren,
  Terminal,
  ArrowUpRight,
  RefreshCw,
  ChevronRight,
  Radio,
  ShieldCheck,
  X,
} from "lucide-react";
import { request, setToken, stamp } from "./api.js";

function Badge({ value }) {
  return (
    <span className={`badge ${value}`}>{value?.replaceAll("_", " ")}</span>
  );
}
export function Chart({ data, field, unit, color = "#5bdfbc" }) {
  if (!data.length)
    return (
      <div className="empty chart-empty">
        Telemetry will appear after the simulator starts.
      </div>
    );
  const values = data.map((d) => d.metrics[field]);
  const max = Math.max(...values, 1) * 1.15;
  const points = values
    .map(
      (v, i) =>
        `${20 + (i * 660) / Math.max(values.length - 1, 1)},${150 - (v / max) * 125}`,
    )
    .join(" ");
  return (
    <div className="chart">
      <div className="chart-value">
        {values.at(-1).toFixed(1)} <small>{unit}</small>
        <span>Latest minute average</span>
      </div>
      <svg
        viewBox="0 0 700 170"
        role="img"
        aria-label={`${field} trend in ${unit}, latest ${values.at(-1).toFixed(1)}`}
      >
        <path
          d="M20 25H680 M20 85H680 M20 150H680"
          stroke="#263040"
          strokeDasharray="4 6"
        />
        <polyline
          points={points}
          stroke={color}
          strokeWidth="2.5"
          fill="none"
        />
        {values.length === 1 && (
          <circle
            cx="20"
            cy={150 - (values[0] / max) * 125}
            r="4"
            fill={color}
          />
        )}
      </svg>
      <div className="chart-axis">
        <span>{stamp(data[0].timestamp)}</span>
        <span>{stamp(data.at(-1).timestamp)}</span>
      </div>
    </div>
  );
}
function IncidentTable({ items, open }) {
  return items.length ? (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Incident</th>
            <th>Severity</th>
            <th>Status</th>
            <th>Last observed</th>
            <th>Events</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {items.map((i) => (
            <tr key={i.id}>
              <td>
                <button className="text-button" onClick={() => open(i)}>
                  {i.title}
                </button>
                <small>
                  {i.device_id.slice(0, 8)} · {i.type.replaceAll("_", " ")}
                </small>
              </td>
              <td>
                <Badge value={i.severity} />
              </td>
              <td>
                <Badge value={i.status} />
              </td>
              <td>{stamp(i.last_seen_at)}</td>
              <td>{i.occurrence_count}</td>
              <td>
                <button
                  aria-label={`Inspect ${i.title}`}
                  className="icon-button"
                  onClick={() => open(i)}
                >
                  <ArrowUpRight size={17} />
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  ) : (
    <div className="empty">
      <ShieldCheck size={28} />
      <h3>No incidents in this view</h3>
      <p>New detections will appear here automatically.</p>
    </div>
  );
}
export default function App() {
  const [view, setView] = useState("Overview"),
    [devices, setDevices] = useState([]),
    [incidents, setIncidents] = useState([]),
    [selected, setSelected] = useState(""),
    [telemetry, setTelemetry] = useState([]),
    [detail, setDetail] = useState(null),
    [detailTelemetry, setDetailTelemetry] = useState([]);
  const [summary, setSummary] = useState({
    devices: { total: 0, online: 0, degraded: 0, offline: 0 },
    incidents: { low: 0, medium: 0, high: 0, critical: 0 },
  });
  const [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [updated, setUpdated] = useState(null),
    [status, setStatus] = useState(""),
    [severity, setSeverity] = useState(""),
    [offset, setOffset] = useState(0),
    [deviceOffset, setDeviceOffset] = useState(0),
    [busy, setBusy] = useState(false),
    [auth, setAuth] = useState("");
  const refresh = useCallback(async () => {
    try {
      const [d, i, totals] = await Promise.all([
        request(`/devices?limit=200&offset=${deviceOffset}`),
        request(
          `/incidents?limit=25&offset=${offset}&status=${status}&severity=${severity}`,
        ),
        request(`/summary`),
      ]);
      if (totals.devices) setSummary(totals);
      setDevices(d.items);
      setIncidents(i.items);
      setUpdated(new Date());
      setError("");
    } catch (e) {
      setError(e.message);
    } finally {
      setLoading(false);
    }
  }, [deviceOffset, offset, status, severity]);
  useEffect(() => {
    refresh();
    const timer = setInterval(refresh, 5000);
    return () => clearInterval(timer);
  }, [refresh]);
  const activeID = selected || devices[0]?.id;
  useEffect(() => {
    if (!activeID) {
      setTelemetry([]);
      return;
    }
    let live = true;
    const controller = new AbortController();
    const load = async () => {
      try {
        const data = await request(`/devices/${activeID}/telemetry`, {
          signal: controller.signal,
        });
        if (live) setTelemetry(data.items);
      } catch (e) {
        if (live) setError(e.message);
      }
    };
    setTelemetry([]);
    load();
    const timer = setInterval(load, 5000);
    return () => {
      live = false;
      controller.abort();
      clearInterval(timer);
    };
  }, [activeID]);
  const inspect = async (i) => {
    setBusy(true);
    try {
      const [fresh, t] = await Promise.all([
        request(`/incidents/${i.id}`),
        request(`/devices/${i.device_id}/telemetry`),
      ]);
      setDetail(fresh);
      setDetailTelemetry(t.items);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  };
  const action = async (name) => {
    setBusy(true);
    try {
      setDetail(
        await request(`/incidents/${detail.id}/${name}`, { method: "POST" }),
      );
      await refresh();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  };
  const current = devices.find((d) => d.id === activeID);
  return (
    <div className="shell">
      <aside>
        <a
          className="brand"
          href="#"
          onClick={(e) => {
            e.preventDefault();
            setView("Overview");
          }}
        >
          <span className="brand-mark">
            <Activity size={23} />
          </span>
          netpulse<span className="ai-tag">AI</span>
        </a>
        <div className="workspace">
          <span className="workspace-icon">N</span>
          <div>
            Network operations<small>Observability workspace</small>
          </div>
        </div>
        <p className="nav-label">WORKSPACE</p>
        <nav>
          {[
            [LayoutDashboard, "Overview"],
            [Router, "Devices"],
            [Siren, "Incidents"],
            [Terminal, "Demo guide"],
          ].map(([Icon, label]) => (
            <button
              key={label}
              className={view === label ? "active" : ""}
              onClick={() => setView(label)}
            >
              <Icon size={18} />
              {label}
              {label === "Incidents" && (
                <span className="nav-count">
                  {incidents.filter((i) => i.status !== "resolved").length}
                </span>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span className={`dot ${error ? "red" : ""}`} />
          {error ? "Connection needs attention" : "Polling every 5 seconds"}
          <small>Kafka telemetry pipeline · v1</small>
        </div>
      </aside>
      <main>
        <header>
          <div className="breadcrumb">
            Workspace <ChevronRight size={14} /> <span>{view}</span>
          </div>
          <div className="header-right">
            <span className="live">
              <Radio size={13} /> {error ? "DISCONNECTED" : "API CONNECTED"}
            </span>
            <span className="avatar">NP</span>
          </div>
        </header>
        <div className="content">
          <div className="page-heading">
            <div>
              <p className="eyebrow">NETWORK INTELLIGENCE</p>
              <h1>{view === "Overview" ? "Your network, in focus." : view}</h1>
              <p className="subtitle">
                {view === "Overview"
                  ? "Real-time health, emerging incidents, and context to act."
                  : view === "Devices"
                    ? "Monitor your devices and explore their latest telemetry."
                    : view === "Incidents"
                      ? "Investigate deterministic detections with advisory AI context."
                      : "Run controlled scenarios through the real ingestion pipeline."}
              </p>
            </div>
            <button className="secondary" onClick={refresh} disabled={busy}>
              <RefreshCw size={15} /> Refresh
            </button>
          </div>
          {error && (
            <div role="alert" className="error">
              <strong>Unable to refresh:</strong> {error}
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  setToken(auth);
                  setAuth("");
                  refresh();
                }}
              >
                <label htmlFor="token">API token, if required</label>
                <input
                  id="token"
                  type="password"
                  value={auth}
                  onChange={(e) => setAuth(e.target.value)}
                  autoComplete="off"
                />
                <button>Connect</button>
              </form>
            </div>
          )}
          {loading ? (
            <div className="empty" role="status">
              Connecting to your network…
            </div>
          ) : (
            <>
              {view === "Overview" && (
                <>
                  <div className="stats">
                    {[
                      [
                        "Total devices",
                        summary.devices.total,
                        Router,
                        "neutral",
                      ],
                      ["Online", summary.devices.online, Activity, "green"],
                      ["Degraded", summary.devices.degraded, Activity, "amber"],
                      ["Offline", summary.devices.offline, Siren, "red"],
                    ].map(([title, count, Icon, color]) => (
                      <section className="stat" key={title}>
                        <div>
                          {title}
                          <Icon className={color} size={17} />
                        </div>
                        <strong>{count}</strong>
                        <small>
                          <span className={`dot ${color}`} />
                          {title === "Total devices"
                            ? "Across your entire network"
                            : "Current reported health"}
                        </small>
                      </section>
                    ))}
                  </div>
                  <div className="severity-strip">
                    <span>Active incidents</span>
                    {Object.entries(summary.incidents).map(([key, n]) => (
                      <span key={key}>
                        <Badge value={key} /> <strong>{n}</strong>
                      </span>
                    ))}
                  </div>
                  <div className="section-heading">
                    <h2>Telemetry overview</h2>
                    <label className="select-label">
                      Device{" "}
                      <select
                        value={activeID || ""}
                        onChange={(e) => setSelected(e.target.value)}
                      >
                        {devices.map((d) => (
                          <option value={d.id} key={d.id}>
                            {d.name}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                  <div className="chart-grid">
                    <section className="panel">
                      <div className="panel-heading">
                        <h3>Latency</h3>
                        <span>LAST 24 HOURS</span>
                      </div>
                      <Chart data={telemetry} field="latency_ms" unit="ms" />
                    </section>
                    <section className="panel">
                      <div className="panel-heading">
                        <h3>Packet loss</h3>
                        <span>LAST 24 HOURS</span>
                      </div>
                      <Chart
                        data={telemetry}
                        field="packet_loss_pct"
                        unit="%"
                        color="#a998ff"
                      />
                    </section>
                  </div>
                  <section className="panel">
                    <div className="panel-heading">
                      <h2>
                        Recent incidents{" "}
                        <span className="count">{incidents.length}</span>
                      </h2>
                      <button
                        className="text-button accent"
                        onClick={() => setView("Incidents")}
                      >
                        View all <ArrowUpRight size={15} />
                      </button>
                    </div>
                    <IncidentTable
                      items={incidents.slice(0, 6)}
                      open={inspect}
                    />
                  </section>
                  <div className="advisory">
                    <ShieldCheck size={18} />
                    <p>
                      Deterministic detection. AI-assisted investigation.
                      <span>
                        Incident severity comes from your configured rules. AI
                        suggestions are hypotheses.
                      </span>
                    </p>
                  </div>
                </>
              )}
              {view === "Devices" && (
                <>
                  <section className="panel">
                    <div className="panel-heading">
                      <h2>Device inventory</h2>
                      <span>{devices.length} IN THIS PAGE</span>
                    </div>
                    {devices.length ? (
                      <div className="table-wrap">
                        <table>
                          <thead>
                            <tr>
                              <th>Device</th>
                              <th>Status</th>
                              <th>Location</th>
                              <th>Latency</th>
                              <th>Last seen</th>
                            </tr>
                          </thead>
                          <tbody>
                            {devices.map((d) => (
                              <tr
                                key={d.id}
                                className={d.id === activeID ? "selected" : ""}
                              >
                                <td>
                                  <button
                                    className="text-button"
                                    onClick={() => setSelected(d.id)}
                                  >
                                    {d.name}
                                  </button>
                                  <small>{d.id.slice(0, 8)}</small>
                                </td>
                                <td>
                                  <Badge value={d.status} />
                                </td>
                                <td>{d.location || "—"}</td>
                                <td>
                                  {d.latest
                                    ? `${d.latest.latency_ms.toFixed(1)} ms`
                                    : "—"}
                                </td>
                                <td>{stamp(d.last_seen_at)}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    ) : (
                      <div className="empty">
                        No devices registered. Follow the Demo guide to seed
                        your network.
                      </div>
                    )}
                  </section>
                  <div className="pagination">
                    <button
                      className="secondary"
                      disabled={!deviceOffset}
                      onClick={() =>
                        setDeviceOffset(Math.max(0, deviceOffset - 200))
                      }
                    >
                      Previous
                    </button>
                    <span>Page {deviceOffset / 200 + 1}</span>
                    <button
                      className="secondary"
                      disabled={devices.length < 200}
                      onClick={() => setDeviceOffset(deviceOffset + 200)}
                    >
                      Next
                    </button>
                  </div>
                  {current && (
                    <>
                      <div className="section-heading">
                        <h2>
                          {current.name} <Badge value={current.status} />
                        </h2>
                        <span>
                          {current.location} · Capacity {current.capacity_mbps}{" "}
                          Mbps
                        </span>
                      </div>
                      <div className="chart-grid">
                        <section className="panel">
                          <div className="panel-heading">
                            <h3>Latency</h3>
                          </div>
                          <Chart
                            data={telemetry}
                            field="latency_ms"
                            unit="ms"
                          />
                        </section>
                        <section className="panel">
                          <div className="panel-heading">
                            <h3>Packet loss</h3>
                          </div>
                          <Chart
                            data={telemetry}
                            field="packet_loss_pct"
                            unit="%"
                            color="#a998ff"
                          />
                        </section>
                      </div>
                      <DeviceIncidents id={current.id} open={inspect} />
                    </>
                  )}
                </>
              )}
              {view === "Incidents" && (
                <>
                  <div className="filters">
                    <label>
                      Status
                      <select
                        value={status}
                        onChange={(e) => {
                          setStatus(e.target.value);
                          setOffset(0);
                        }}
                      >
                        <option value="">All statuses</option>
                        {["open", "acknowledged", "resolved"].map((s) => (
                          <option key={s}>{s}</option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Severity
                      <select
                        value={severity}
                        onChange={(e) => {
                          setSeverity(e.target.value);
                          setOffset(0);
                        }}
                      >
                        <option value="">All severities</option>
                        {["low", "medium", "high", "critical"].map((s) => (
                          <option key={s}>{s}</option>
                        ))}
                      </select>
                    </label>
                    <span>Created in the last 24 hours</span>
                  </div>
                  <section className="panel">
                    <IncidentTable items={incidents} open={inspect} />
                  </section>
                  <div className="pagination">
                    <button
                      className="secondary"
                      disabled={!offset}
                      onClick={() => setOffset(offset - 25)}
                    >
                      Previous
                    </button>
                    <span>Page {offset / 25 + 1}</span>
                    <button
                      className="secondary"
                      disabled={incidents.length < 25}
                      onClick={() => setOffset(offset + 25)}
                    >
                      Next
                    </button>
                  </div>
                </>
              )}
              {view === "Demo guide" && (
                <section className="panel demo">
                  <span className="badge online">LOCAL DEVELOPMENT</span>
                  <h2>See an incident from signal to recovery.</h2>
                  <p>
                    Run these commands from the project directory. The simulator
                    registers ten devices and sends real telemetry through the
                    API and Kafka.
                  </p>
                  <h3>1. Start the stack and seed devices</h3>
                  <pre>make up{"\n"}make seed</pre>
                  <h3>2. Run the latency scenario</h3>
                  <pre>make demo</pre>
                  <p>
                    The simulator sends 15 seconds of normal traffic, a
                    90-second latency spike on router-001, then restores normal
                    traffic. After the recovery window, the incident resolves
                    automatically.
                  </p>
                  <h3>3. Try another scenario</h3>
                  <pre>
                    docker compose run --rm simulator -url http://api:8080 \
                    {"\n"} -scenario packet-loss -target 2 -duration 90s
                  </pre>
                  <p>
                    Also available: normal, high-cpu, device-offline. Stop any
                    existing simulator before starting another so scenarios do
                    not interfere. Use Ctrl+C to stop.
                  </p>
                  <h3>Optional analysis</h3>
                  <p>
                    Set AI_ENABLED=true and AI_PROVIDER=mock in .env, then
                    recreate the API and worker. The mock is explicitly labeled
                    and needs no API key.
                  </p>
                </section>
              )}
            </>
          )}
          <footer>
            NETPULSE AI{" "}
            <span>
              {updated
                ? `Last refreshed ${updated.toLocaleTimeString()}`
                : "Awaiting first response"}{" "}
              · All data from your API
            </span>
          </footer>
        </div>
      </main>
      {detail && (
        <div className="modal-backdrop" onClick={() => setDetail(null)}>
          <section
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-labelledby="incident-title"
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              if (e.key === "Escape") setDetail(null);
              if (e.key === "Tab") {
                const nodes = [
                  ...e.currentTarget.querySelectorAll(
                    'button:not(:disabled),[tabindex="0"]',
                  ),
                ];
                const first = nodes[0],
                  last = nodes.at(-1);
                if (e.shiftKey && document.activeElement === first) {
                  e.preventDefault();
                  last?.focus();
                } else if (!e.shiftKey && document.activeElement === last) {
                  e.preventDefault();
                  first?.focus();
                }
              }
            }}
          >
            <div className="panel-heading">
              <span className="eyebrow">INCIDENT DETAIL</span>
              <button
                autoFocus
                className="icon-button"
                aria-label="Close incident"
                onClick={() => setDetail(null)}
              >
                <X />
              </button>
            </div>
            <h2 id="incident-title">{detail.title}</h2>
            <div className="badges">
              <Badge value={detail.severity} />
              <Badge value={detail.status} />
              <span>{detail.occurrence_count} observations</span>
            </div>
            <p>{detail.description}</p>
            <dl>
              <dt>First observed</dt>
              <dd>{stamp(detail.first_seen_at)}</dd>
              <dt>Last observed</dt>
              <dd>{stamp(detail.last_seen_at)}</dd>
              <dt>Resolved</dt>
              <dd>
                {detail.resolved_at ? stamp(detail.resolved_at) : "Active"}
              </dd>
            </dl>
            <Chart data={detailTelemetry} field="latency_ms" unit="ms" />
            <div className="actions">
              <button
                disabled={busy || detail.status !== "open"}
                onClick={() => action("acknowledge")}
              >
                Acknowledge
              </button>
              <button
                disabled={busy || detail.status === "resolved"}
                onClick={() => action("resolve")}
              >
                Resolve incident
              </button>
              <button
                className="secondary"
                disabled={busy}
                onClick={() => inspect(detail)}
              >
                Refresh detail
              </button>
            </div>
            <section className="analysis">
              <h3>
                <Activity size={18} /> AI investigation notes{" "}
                <Badge value={detail.analysis_status} />
              </h3>
              {detail.analysis ? (
                <>
                  <p>{detail.analysis.summary}</p>
                  {[
                    ["Observed facts", "facts"],
                    ["Possible causes · hypotheses", "hypotheses"],
                    ["Investigation suggestions", "steps"],
                  ].map(([title, key]) => (
                    <div key={key}>
                      <h4>{title}</h4>
                      <ul>
                        {detail.analysis[key]?.map((text, i) => (
                          <li key={i}>{text}</li>
                        ))}
                      </ul>
                    </div>
                  ))}
                  <p className="uncertainty">{detail.analysis.uncertainty}</p>
                </>
              ) : (
                <p>
                  Analysis is {detail.analysis_status}. Detection and incident
                  actions remain available.
                </p>
              )}
              <button
                className="secondary"
                disabled={
                  busy ||
                  ["pending", "processing", "completed"].includes(
                    detail.analysis_status,
                  )
                }
                onClick={() => action("analyze")}
              >
                Request analysis
              </button>
            </section>
          </section>
        </div>
      )}
    </div>
  );
}
function DeviceIncidents({ id, open }) {
  const [items, setItems] = useState([]);
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    const load = async () => {
      try {
        const data = await request(`/incidents?device_id=${id}&limit=200`);
        if (live) {
          setItems(data.items.filter((i) => i.status !== "resolved"));
          setError("");
        }
      } catch (e) {
        if (live) setError(e.message);
      }
    };
    load();
    const t = setInterval(load, 5000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [id]);
  return (
    <section className="panel">
      <div className="panel-heading">
        <h3>Active incidents · last 24 hours</h3>
      </div>
      {error ? (
        <p role="alert">{error}</p>
      ) : (
        <IncidentTable items={items} open={open} />
      )}
    </section>
  );
}
