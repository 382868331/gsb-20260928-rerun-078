import type { ConnDiffReport, ConnResult, DiffReport, PathResult } from "../types";

// Every array the UI maps over is guaranteed to exist, so a backend
// response of paths:null / phases:null can never blank the page.

export function normalizePathResult(r: PathResult): PathResult {
  return {
    ...r,
    paths: (r.paths ?? []).map(normalizePath),
    blocked: (r.blocked ?? []).map(normalizePath),
    reason: r.reason || (r.reachable ? "allowed" : "no-route"),
  };
}

function normalizePath(p: PathResult["paths"][number]): PathResult["paths"][number] {
  return { ...p, hops: p.hops ?? [], nodeIds: p.nodeIds ?? [] };
}

export function normalizeConnResult(r: ConnResult): ConnResult {
  return {
    ...r,
    forward: normalizePathResult(r.forward),
    maintain: normalizePathResult(r.maintain),
    return: normalizePathResult(r.return),
    phases: (r.phases ?? []).map((ph) => ({
      ...ph,
      path: ph.path ? normalizePathResult(ph.path) : undefined,
    })),
  };
}

export function normalizeDiffReport(r: DiffReport): DiffReport {
  return {
    ...r,
    newReachable: r.newReachable ?? [],
    newBlocked: r.newBlocked ?? [],
  };
}

export function normalizeConnDiffReport(r: ConnDiffReport): ConnDiffReport {
  return {
    newConns: normalizeDiffReport(r.newConns),
    existing: (r.existing ?? []).map((e) => ({ ...e, afterHops: e.afterHops ?? [] })),
  };
}

