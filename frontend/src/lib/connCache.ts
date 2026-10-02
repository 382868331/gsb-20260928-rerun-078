import type { Connection, ConnResult } from "../types";

// Connection-state trial results and registered established
// connections are cached per saved version, so reopening the page
// restores each explanation against the exact topology, policy and
// connection state it was computed from. Trials never write back to
// any version: this cache is the only place they live.

export interface ConnTrial {
  key: string;
  version: number;
  source: string;
  target: string;
  downNodes: string[];
  downLinks: string[];
  timeout: boolean;
  result: ConnResult;
}

interface VersionConnData {
  connections: Connection[];
  trials: Record<string, ConnTrial>;
}

const KEY = "topo-conns-v1";

function readAll(): Record<string, VersionConnData> {
  try {
    return JSON.parse(localStorage.getItem(KEY) || "{}") as Record<string, VersionConnData>;
  } catch {
    return {};
  }
}

function writeAll(all: Record<string, VersionConnData>) {
  try {
    localStorage.setItem(KEY, JSON.stringify(all));
  } catch {
    /* best-effort cache */
  }
}

function entry(version: number): VersionConnData {
  const all = readAll();
  return all[String(version)] ?? { connections: [], trials: {} };
}

function put(version: number, data: VersionConnData) {
  if (!version) return; // unsaved drafts have no stable identity
  const all = readAll();
  all[String(version)] = data;
  writeAll(all);
}

export function trialKey(source: string, target: string, downNodes: string[], downLinks: string[], timeout: boolean): string {
  const n = [...downNodes].sort().join(",");
  const l = [...downLinks].sort().join(",");
  return [source, target, "n:" + n, "l:" + l, timeout ? "t:1" : "t:0"].join("|");
}

export function loadConnections(version: number): Connection[] {
  if (!version) return [];
  return entry(version).connections ?? [];
}

export function saveConnections(version: number, connections: Connection[]) {
  const d = entry(version);
  d.connections = connections;
  put(version, d);
}

export function loadTrials(version: number): ConnTrial[] {
  if (!version) return [];
  const t = entry(version).trials ?? {};
  return Object.values(t).sort((a, b) => a.key.localeCompare(b.key));
}

export function saveTrial(version: number, trial: ConnTrial) {
  const d = entry(version);
  d.trials = { ...(d.trials ?? {}), [trial.key]: trial };
  put(version, d);
}

export function removeTrial(version: number, key: string) {
  const d = entry(version);
  if (d.trials) delete d.trials[key];
  put(version, d);
}

