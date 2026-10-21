import { afterEach, expect, it, vi } from "vitest";
import { request, setToken } from "./api.js";
afterEach(() => {
  setToken("");
  vi.unstubAllGlobals();
});
it("reports proxy failures without JSON parsing errors", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: false,
      status: 502,
      json: async () => {
        throw new Error("not JSON");
      },
    })),
  );
  await expect(request("/devices")).rejects.toThrow("Request failed (502)");
});
it("keeps the operator token in request headers", async () => {
  const fetcher = vi.fn(async () => ({
    ok: true,
    json: async () => ({ items: [] }),
  }));
  vi.stubGlobal("fetch", fetcher);
  setToken("test-operator-token");
  await request("/devices");
  expect(fetcher.mock.calls[0][1].headers.Authorization).toBe(
    "Bearer test-operator-token",
  );
});
