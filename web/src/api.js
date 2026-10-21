let token = "";
export function setToken(value) {
  token = value;
}
export async function request(path, options = {}) {
  if (import.meta.env.VITE_SNAPSHOT_MODE === "true") {
    const { requestSnapshot } = await import("./snapshot.js");
    return requestSnapshot(path, options);
  }
  const response = await fetch(`/api/v1${path}`, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...options.headers,
    },
  });
  const body = await response.json().catch(() => null);
  if (!response.ok)
    throw new Error(
      body?.error?.message || `Request failed (${response.status})`,
    );
  if (!body)
    throw new Error(
      "The API returned an invalid response. Check that the backend is running.",
    );
  return body;
}
export function stamp(value) {
  return value
    ? new Date(value).toLocaleString([], {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      })
    : "Not seen yet";
}
