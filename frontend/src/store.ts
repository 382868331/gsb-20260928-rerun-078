import { create } from "zustand";
import type { Link, NodeT, Rule, Topology } from "./types";

const LS_KEY = "topo-draft-v1";

let counter = 0;
export function genId(kind: string): string {
  counter += 1;
  return `${kind}-${Date.now().toString(36)}-${counter}`;
}

function loadInitial(): Topology {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Topology;
      if (parsed.nodes && parsed.links && parsed.rules) return parsed;
    }
  } catch {
    // ignore corrupt storage
  }
  return { nodes: [], links: [], rules: [] };
}

export function serverPayload(t: Topology): Topology {
  return {
    nodes: t.nodes.map(({ x: _x, y: _y, ...rest }) => rest),
    links: t.links,
    rules: t.rules,
  };
}

function persist(t: Topology) {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(t));
  } catch {
    // storage may be unavailable; editor keeps working in memory
  }
}

interface State {
  topo: Topology;
  history: Topology[];
  selectedNode?: string;
  selectedRule?: string;
  pendingLinkFrom?: string;
  setTopo: (t: Topology, pushHistory?: boolean) => void;
  undo: () => void;
  addNode: (partial?: Partial<NodeT>) => string;
  updateNode: (id: string, patch: Partial<NodeT>) => void;
  removeNode: (id: string) => void;
  addLink: (from: string, to: string) => void;
  removeLink: (id: string) => void;
  addRule: () => string;
  updateRule: (id: string, patch: Partial<Rule>) => void;
  removeRule: (id: string) => void;
  moveRule: (id: string, dir: -1 | 1) => void;
  selectNode: (id?: string) => void;
  selectRule: (id?: string) => void;
  setPendingLinkFrom: (id?: string) => void;
  exportJson: () => string;
  importJson: (raw: string) => void;
}

export const useStore = create<State>((set, get) => ({
  topo: loadInitial(),
  history: [],

  setTopo: (t, pushHistory = true) => {
    const { topo, history } = get();
    persist(t);
    set({ topo: t, history: pushHistory ? [...history.slice(-49), topo] : history });
  },

  undo: () => {
    const { history } = get();
    if (history.length === 0) return;
    const prev = history[history.length - 1];
    persist(prev);
    set({
      topo: prev,
      history: history.slice(0, -1),
      selectedNode: undefined,
      selectedRule: undefined,
    });
  },

  addNode: (partial) => {
    const count = get().topo.nodes.length;
    const id = partial?.id ?? genId("n");
    const node: NodeT = {
      id,
      name: partial?.name ?? `节点-${count + 1}`,
      zone: partial?.zone ?? "lan",
      kind: partial?.kind ?? "host",
      x: partial?.x ?? 120 + (count % 6) * 120,
      y: partial?.y ?? 120 + Math.floor(count / 6) * 110,
    };
    get().setTopo({ ...get().topo, nodes: [...get().topo.nodes, node] });
    set({ selectedNode: id });
    return id;
  },

  updateNode: (id, patch) => {
    const nodes = get().topo.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n));
    get().setTopo({ ...get().topo, nodes });
  },

  removeNode: (id) => {
    const { topo } = get();
    get().setTopo({
      ...topo,
      nodes: topo.nodes.filter((n) => n.id !== id),
      links: topo.links.filter((l) => l.from !== id && l.to !== id),
      rules: topo.rules.map((r) => ({
        ...r,
        nodeIds: r.nodeIds.filter((x) => x !== id),
        srcNodeIds: r.srcNodeIds.filter((x) => x !== id),
        dstNodeIds: r.dstNodeIds.filter((x) => x !== id),
      })),
    });
    set({ selectedNode: undefined });
  },

  addLink: (from, to) => {
    if (from === to) return;
    const exists = get().topo.links.some((l) => l.from === from && l.to === to);
    if (exists) return;
    const link: Link = { id: genId("l"), from, to };
    get().setTopo({ ...get().topo, links: [...get().topo.links, link] });
    set({ pendingLinkFrom: undefined });
  },

  removeLink: (id) => {
    const topo = get().topo;
    get().setTopo({ ...topo, links: topo.links.filter((l) => l.id !== id) });
  },

  addRule: () => {
    const id = genId("r");
    const rule: Rule = {
      id,
      name: `规则-${get().topo.rules.length + 1}`,
      nodeIds: [],
      srcZones: [],
      dstZones: [],
      srcNodeIds: [],
      dstNodeIds: [],
      protocols: [],
      action: "permit",
      disabled: false,
    };
    get().setTopo({ ...get().topo, rules: [...get().topo.rules, rule] });
    set({ selectedRule: id });
    return id;
  },

  updateRule: (id, patch) => {
    const rules = get().topo.rules.map((r) => (r.id === id ? { ...r, ...patch } : r));
    get().setTopo({ ...get().topo, rules });
  },

  removeRule: (id) => {
    const topo = get().topo;
    get().setTopo({ ...topo, rules: topo.rules.filter((r) => r.id !== id) });
    set({ selectedRule: undefined });
  },

  moveRule: (id, dir) => {
    const rules = [...get().topo.rules];
    const i = rules.findIndex((r) => r.id === id);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= rules.length) return;
    [rules[i], rules[j]] = [rules[j], rules[i]];
    get().setTopo({ ...get().topo, rules });
  },

  selectNode: (id) => set({ selectedNode: id }),
  selectRule: (id) => set({ selectedRule: id }),
  setPendingLinkFrom: (id) => set({ pendingLinkFrom: id }),

  exportJson: () => {
    const { topo } = get();
    return JSON.stringify(serverPayload(topo), null, 2);
  },

  importJson: (raw) => {
    const parsed = JSON.parse(raw) as Topology;
    if (!Array.isArray(parsed.nodes) || !Array.isArray(parsed.links) || !Array.isArray(parsed.rules)) {
      throw new Error("文件必须包含 nodes / links / rules 数组");
    }
    const ids = new Set<string>();
    for (const n of parsed.nodes) {
      if (!n.id) throw new Error("节点缺少 id，导入会破坏身份保持");
      if (ids.has(n.id)) throw new Error(`节点 id 重复: ${n.id}`);
      ids.add(n.id);
    }
    for (const l of parsed.links) {
      if (!ids.has(l.from) || !ids.has(l.to)) {
        throw new Error(`链路 ${l.id} 引用了不存在的节点`);
      }
    }
    // Imported IDs are kept verbatim: never regenerate node/link/rule ids.
    get().setTopo({ nodes: parsed.nodes, links: parsed.links, rules: parsed.rules });
  },
}));
