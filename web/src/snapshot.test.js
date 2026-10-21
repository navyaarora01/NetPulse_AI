import { describe, expect, it } from "vitest";
import { snapshotRequest } from "./snapshot.js";

const data = {
  devices: [{ id: "router-a", name: "Original router" }],
  incidents: [
    {
      id: "incident-a",
      device_id: "router-a",
      status: "open",
      severity: "critical",
    },
    {
      id: "incident-b",
      device_id: "router-b",
      status: "resolved",
      severity: "medium",
    },
  ],
  telemetry: {
    "router-a": [{ timestamp: "2026-10-21T08:00:00Z", samples: 10 }],
  },
  summary: { devices: { total: 1 } },
};

describe("captured dashboard data", () => {
  it("preserves recorded device, incident and telemetry identities", () => {
    expect(snapshotRequest(data, "/devices/router-a")).toBe(data.devices[0]);
    expect(snapshotRequest(data, "/incidents/incident-a")).toBe(
      data.incidents[0],
    );
    expect(snapshotRequest(data, "/devices/router-a/telemetry").items).toBe(
      data.telemetry["router-a"],
    );
  });
  it("filters incidents before pagination", () => {
    expect(
      snapshotRequest(
        data,
        "/incidents?status=open&severity=critical&device_id=router-a",
      ).items,
    ).toEqual([data.incidents[0]]);
    expect(snapshotRequest(data, "/incidents?limit=1&offset=1").items).toEqual([
      data.incidents[1],
    ]);
  });
  it("rejects mutations and missing resources", () => {
    expect(() =>
      snapshotRequest(data, "/incidents/incident-a/resolve", {
        method: "POST",
      }),
    ).toThrow("read-only");
    expect(data.incidents[0].status).toBe("open");
    expect(() => snapshotRequest(data, "/devices/unknown")).toThrow(
      "not present",
    );
  });
});
