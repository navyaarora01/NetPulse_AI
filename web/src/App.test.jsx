import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  render,
  screen,
  waitFor,
  fireEvent,
} from "@testing-library/react";
import App, { Chart } from "./App.jsx";
beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({ ok: true, json: async () => ({ items: [] }) })),
  );
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
describe("dashboard", () => {
  it("renders backend empty states and demo instructions", async () => {
    render(<App />);
    await screen.findByText("No incidents in this view");
    fireEvent.click(screen.getByRole("button", { name: "Demo guide" }));
    expect(screen.getByText("make demo")).toBeInTheDocument();
  });
  it("surfaces API failures without manufacturing live data", async () => {
    fetch.mockResolvedValue({
      ok: false,
      json: async () => ({
        error: { message: "Service temporarily unavailable" },
      }),
    });
    render(<App />);
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "Service temporarily unavailable",
      ),
    );
    expect(screen.getByText("DISCONNECTED")).toBeInTheDocument();
  });
  it("exposes chart units to assistive technology", () => {
    render(
      <Chart
        data={[
          { timestamp: "2026-01-01T10:00:00Z", metrics: { latency_ms: 43 } },
        ]}
        field="latency_ms"
        unit="ms"
      />,
    );
    expect(screen.getByRole("img")).toHaveAccessibleName(/43.0/);
  });
});
