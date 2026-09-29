import type { DiffReport, PathResult, Topology } from "../types";

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `${res.status} ${res.statusText}`);
  }
  return res.json() as Promise<T>;
}

export const api = {
  getDraft: () => req<Topology>("/api/draft"),
  putDraft: (t: Topology) =>
    req<Topology>("/api/draft", { method: "PUT", body: JSON.stringify(t) }),
  save: (topology: Topology, label: string) =>
    req<{ meta: { version: number }; topology: Topology }>("/api/save", {
      method: "POST",
      body: JSON.stringify({ topology, label }),
    }),
  versions: () => req<{ version: number; label: string; created: string }[]>("/api/versions"),
  loadVersion: (v: number) =>
    req<{ topology: Topology }>(`/api/versions/${v}`),
  check: (topology: Topology, source: string, target: string, maxPaths = 3) =>
    req<PathResult>("/api/check", {
      method: "POST",
      body: JSON.stringify({ topology, source, target, maxPaths }),
    }),
  diff: (before: Topology, after: Topology, detailed = false) =>
    req<DiffReport>("/api/diff", {
      method: "POST",
      body: JSON.stringify({ before, after, detailed }),
    }),
  importTopo: (t: Topology) =>
    req<Topology>("/api/import", { method: "POST", body: JSON.stringify(t) }),
};
