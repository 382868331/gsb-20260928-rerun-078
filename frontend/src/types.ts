export type LinkKind = "directed" | "undirected";
export type RuleAction = "allow" | "deny";

export interface Attrs {
  [k: string]: string;
}

export interface TopoNode {
  id: string;
  name: string;
  kind?: string;
  x: number;
  y: number;
  attrs?: Attrs;
}

export interface TopoLink {
  id: string;
  source: string;
  target: string;
  kind: LinkKind;
  attrs?: Attrs;
}

export interface Rule {
  id: string;
  order: number;
  enabled: boolean;
  action: RuleAction;
  srcNode?: string;
  dstNode?: string;
  srcKind?: string;
  dstKind?: string;
  linkId?: string;
  linkAttrName?: string;
  linkAttrValue?: string;
  srcAttrName?: string;
  srcAttrValue?: string;
  dstAttrName?: string;
  dstAttrValue?: string;
  protocol?: string;
  note?: string;
}

export interface Policy {
  defaultAction: RuleAction;
  rules: Rule[];
}

export interface Topology {
  version: number;
  nodes: TopoNode[];
  links: TopoLink[];
  policy: Policy;
}

export interface Hop {
  from: string;
  to: string;
  linkId: string;
  reverse: boolean;
  ruleId?: string;
  ruleOrder?: number;
  action: RuleAction;
}

export interface PathInfo {
  hops: Hop[];
  nodeIds: string[];
  reachable: boolean;
  length: number;
}

export interface PathResult {
  source: string;
  target: string;
  reachable: boolean;
  paths: PathInfo[];
  blocked: PathInfo[];
  reason: string;
}

export interface PairResult {
  source: string;
  target: string;
  before: boolean;
  after: boolean;
  change: "none" | "now-reachable" | "now-blocked";
  hopChanges?: HopChange[];
  beforeAt?: PathResult;
  afterAt?: PathResult;
}

export type HopChangeCause =
  | "rule-reordered"
  | "rule-changed"
  | "rule-enabled"
  | "rule-disabled"
  | "rule-added"
  | "rule-removed"
  | "default-changed"
  | "link-added"
  | "link-removed"
  | "link-direction-changed"
  | "unchanged"
  | "unknown";

export interface HopChange {
  from: string;
  to: string;
  linkId: string;
  beforeRuleId?: string;
  beforeRuleOrder?: number;
  beforeAction?: string;
  afterRuleId?: string;
  afterRuleOrder?: number;
  afterAction?: string;
  cause: HopChangeCause;
  detail?: string;
}

export interface DiffReport {
  beforeVersion: number;
  afterVersion: number;
  newReachable: PairResult[];
  newBlocked: PairResult[];
  unchanged?: PairResult[];
  totalPairs: number;
}
