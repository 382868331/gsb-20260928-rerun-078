import type { ConnCheckResult } from "../types";

// Saved what-if trials of the connection-state check. Each trial is
// keyed to the exact topology version, policy and connection-state
// parameters it was computed against, so reopening the page restores
// explanations that still correspond to that original context. Trials
// are read-only records: saving one never mutates the draft, other
// scenarios, or other trials.
export interface ConnTrial {
  id: string;
  version: number; // 0 = unsaved draft
  source: string;
  target: string;
  afterTimeout: boolean;
  downNode: string; // "" = none
  savedAt: string;
  result: ConnCheckResult;
}

const KEY = "topo-conn-trials-v1";

function readAll(): ConnTrial[] {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) || "[]") as unknown;
    return Array.isArray(v) ? (v as ConnTrial[]) : [];
  } catch {
    return [];
  }
}

function writeAll(trials: ConnTrial[]) {
  try {
    localStorage.setItem(KEY, JSON.stringify(trials));
  } catch {
    /* storage full or unavailable: caching is best-effort */
  }
}

export function listConnTrials(version: number): ConnTrial[] {
  return readAll().filter((t) => t.version === version);
}

export function saveConnTrial(t: Omit<ConnTrial, "id" | "savedAt">): ConnTrial {
  const trial: ConnTrial = {
    ...t,
    id: "ct-" + Date.now().toString(36) + "-" + Math.random().toString(36).slice(2, 7),
    savedAt: new Date().toISOString(),
  };
  writeAll([...readAll(), trial]);
  return trial;
}

export function deleteConnTrial(id: string) {
  writeAll(readAll().filter((t) => t.id !== id));
}

