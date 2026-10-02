import type { PathResult } from "../types";

// Check explanations are cached per saved version so reopening the
// page restores the result that belongs to the restored topology --
// never a verdict computed against some other draft.
export interface CachedCheck {
  version: number;
  source: string;
  target: string;
  result: PathResult;
}

const KEY = "topo-checks-v1";

function readAll(): Record<string, CachedCheck> {
  try {
    return JSON.parse(localStorage.getItem(KEY) || "{}") as Record<string, CachedCheck>;
  } catch {
    return {};
  }
}

export function saveCheckForVersion(version: number, source: string, target: string, result: PathResult) {
  if (!version) return; // unsaved drafts have no stable identity to restore against
  const all = readAll();
  all[String(version)] = { version, source, target, result };
  try {
    localStorage.setItem(KEY, JSON.stringify(all));
  } catch {
    /* storage full or unavailable: caching is best-effort */
  }
}

export function loadCheckForVersion(version: number): CachedCheck | null {
  if (!version) return null;
  const hit = readAll()[String(version)];
  if (!hit || !hit.result) return null;
  // Older cache entries may carry null arrays; normalize on read so a
  // restored explanation can never blank the page.
  return { ...hit, result: normalizePathResult(hit.result) };
}

// normalizePathResult guarantees every array the UI maps over exists,
// so a payload of paths:null / blocked:null is always renderable.
export function normalizePathResult(r: PathResult): PathResult {
  return {
    ...r,
    paths: r.paths ?? [],
    blocked: r.blocked ?? [],
    reason: r.reason || (r.reachable ? "allowed" : "no-route"),
  };
}
