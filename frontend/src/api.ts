import type { Connection, DiffResult, ReachResult, Topology } from "./types";

const BASE = "/api";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export const api = {
  save: (t: Topology) =>
    req<Topology>("/topology", { method: "POST", body: JSON.stringify(t) }),
  undo: () => req<Topology>("/undo", { method: "POST" }),
  draft: () => req<Topology>("/topology"),
  versions: () => req<{ versions: string[]; canUndo: boolean }>("/versions"),
  evaluate: (conn: Connection, maxPaths = 5, version?: string) =>
    req<ReachResult>("/evaluate", {
      method: "POST",
      body: JSON.stringify({ conn, maxPaths, version }),
    }),
  diff: (fromVersion: string, conns: Connection[], maxPaths = 5, toVersion?: string) =>
    req<DiffResult>("/diff", {
      method: "POST",
      body: JSON.stringify({ fromVersion, toVersion, conns, maxPaths }),
    }),
};
