let pending;

export function snapshotRequest(data, path, options = {}) {
  if ((options.method || "GET").toUpperCase() !== "GET")
    throw new Error("This captured demo is read-only.");
  const url = new URL(path, "https://snapshot.invalid");
  const parts = url.pathname.split("/").filter(Boolean);
  const page = (items) => {
    const limit = Math.max(1, Number(url.searchParams.get("limit")) || 50);
    const offset = Math.max(0, Number(url.searchParams.get("offset")) || 0);
    return { items: items.slice(offset, offset + limit), limit, offset };
  };
  if (parts[0] === "snapshot")
    return {
      captured_at: data.captured_at,
      latest_telemetry_at: data.latest_telemetry_at,
    };
  if (parts[0] === "summary") return data.summary;
  if (parts[0] === "devices") {
    if (!parts[1]) return page(data.devices);
    const device = data.devices.find((d) => d.id === parts[1]);
    if (!device) throw new Error("Device not present in this snapshot.");
    if (parts[2] === "telemetry")
      return { items: data.telemetry[device.id] || [] };
    if (parts.length === 2) return device;
  }
  if (parts[0] === "incidents") {
    if (parts.length === 2) {
      const incident = data.incidents.find((i) => i.id === parts[1]);
      if (incident) return incident;
    }
    if (parts.length === 1)
      return page(
        data.incidents.filter((i) =>
          ["status", "severity", "device_id"].every(
            (key) =>
              !url.searchParams.get(key) ||
              i[key] === url.searchParams.get(key),
          ),
        ),
      );
  }
  throw new Error("This view is not present in the captured demo.");
}

export async function requestSnapshot(path, options) {
  if (!pending) {
    pending = fetch("/snapshot.json")
      .then((response) => {
        if (!response.ok)
          throw new Error("Unable to load the captured dashboard data.");
        return response.json();
      })
      .catch((error) => {
        pending = undefined;
        throw error;
      });
  }
  return snapshotRequest(await pending, path, options);
}
