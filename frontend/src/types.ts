export interface NodeT {
  id: string;
  name: string;
  zone: string;
  kind: string;
  x?: number;
  y?: number;
}

export interface Link {
  id: string;
  from: string;
  to: string;
}

export interface Rule {
  id: string;
  name: string;
  nodeIds: string[];
  srcZones: string[];
  dstZones: string[];
  srcNodeIds: string[];
  dstNodeIds: string[];
  protocols: string[];
  action: "permit" | "deny";
  disabled: boolean;
}

export interface Topology {
  version?: string;
  nodes: NodeT[];
  links: Link[];
  rules: Rule[];
  createdAt?: string;
}

export interface Connection {
  src: string;
  dst: string;
  protocol: string;
}

export interface HopDecision {
  nodeId: string;
  ruleId: string;
  ruleName?: string;
  action: string;
}

export interface Path {
  nodes: string[];
  feasible: boolean;
  decisions: HopDecision[];
  blockedAt: number;
}

export interface ReachResult {
  conn: Connection;
  reachable: boolean;
  activePath?: Path;
  paths: Path[];
  reason: string;
}

export interface Change {
  conn: Connection;
  before: boolean;
  after: boolean;
  kind: "added" | "blocked";
  beforeReason?: string;
  afterReason?: string;
}

export interface DiffResult {
  fromVersion: string;
  toVersion: string;
  added: Change[];
  blocked: Change[];
  unchanged: number;
}
