import type { Connection, ConnResult, Packet } from "../types";

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
  packet?: Packet;
  result: ConnResult;
}

interface VersionConnData {
  connections: Connection[];
  trials: Record<string, ConnTrial>;
}

const KEY = "topo-conns-v1";

export function migrateVersionData(fromVersion: number, toVersion: number) {
  if (!fromVersion || !toVersion || fromVersion === toVersion) return;
  const all = readAll();
  const from = all[String(fromVersion)];
  if (!from) return;
  const connections = [...(all[String(toVersion)]?.connections ?? []), ...(from.connections ?? [])];
  const trials = { ...(all[String(toVersion)]?.trials ?? {}) };
  Object.values(from.trials ?? {}).forEach((trial) => {
    trials[trial.key] = { ...trial, version: toVersion };
  });
  all[String(toVersion)] = { connections, trials };
  writeAll(all);
}

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

export function trialKey(source: string, target: string, downNodes: string[], downLinks: string[], timeout: boolean, packet?: Packet): string {
  const n = [...downNodes].sort().join(",");
  const l = [...downLinks].sort().join(",");
  const flow = packet
    ? [packet.connectionId ?? "auto", packet.source.ip, packet.source.port, packet.destination.ip, packet.destination.port, packet.protocol ?? ""].join(":")
    : "no-packet";
  return [source, target, "n:" + n, "l:" + l, timeout ? "t:1" : "t:0", "f:" + flow].join("|");
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
