import { create } from "zustand";
import type { Rule, RuleAction, TopoLink, TopoNode, Topology } from "../types";
import { clone, uid } from "../lib/id";
import { sampleTopology } from "../lib/sample";

interface TopoState {
  topo: Topology;
  // editSeq increments on every draft mutation; async check/diff
  // responses captured before an edit use it to detect staleness.
  editSeq: number;
  // cleanEditSeq is the editSeq at the last save/snapshot load.
  // editSeq !== cleanEditSeq means there are unsaved changes.
  cleanEditSeq: number;
  // hydrated becomes true once the backend draft has been loaded (or
  // loading failed); until then the sample is only a placeholder.
  hydrated: boolean;
  // policyBaseline is the snapshot used for "what changed" diffs:
  // captured on save/import/baseline-set, independent of the draft.
  policyBaseline: Topology | null;
  past: Topology[];
  future: Topology[];
  selectedNode: string | null;
  selectedLink: string | null;
  selectedRule: string | null;

  commit: (label?: string) => void;
  undo: () => void;
  redo: () => void;
  setBaseline: () => void;

  addNode: (partial?: Partial<TopoNode>) => string;
  updateNode: (id: string, patch: Partial<TopoNode>) => void;
  removeNode: (id: string) => void;
  addLink: (source: string, target: string, kind?: TopoLink["kind"]) => string | null;
  updateLink: (id: string, patch: Partial<TopoLink>) => void;
  removeLink: (id: string) => void;

  addRule: () => string;
  updateRule: (id: string, patch: Partial<Rule>) => void;
  removeRule: (id: string) => void;
  moveRule: (id: string, delta: -1 | 1) => void;
  setDefaultAction: (a: RuleAction) => void;

  select: (kind: "node" | "link" | "rule", id: string | null) => void;
  replaceAll: (t: Topology, options?: { keepHistory?: boolean }) => void;
  // loadSnapshot replaces the draft with a saved/restored topology,
  // clears undo history and marks the state clean (no unsaved edits).
  loadSnapshot: (t: Topology) => void;
  markClean: () => void;
  setHydrated: (v: boolean) => void;
  bumpEdit: () => void;
}

const MAX_HISTORY = 100;

function orderedRules(rules: Rule[]): Rule[] {
  return [...rules].sort((a, b) => a.order - b.order);
}

export const useTopoStore = create<TopoState>((set, get) => ({
  topo: clone(sampleTopology),
  editSeq: 0,
  cleanEditSeq: 0,
  hydrated: false,
  policyBaseline: null,
  past: [],
  future: [],
  selectedNode: null,
  selectedLink: null,
  selectedRule: null,

  commit: () =>
    set((s) => ({
      past: [...s.past.slice(-MAX_HISTORY + 1), clone(s.topo)],
      future: [],
    })),

  undo: () =>
    set((s) => {
      if (s.past.length === 0) return s;
      const prev = s.past[s.past.length - 1];
      return {
        editSeq: s.editSeq + 1,
        topo: prev,
        past: s.past.slice(0, -1),
        future: [clone(s.topo), ...s.future].slice(0, MAX_HISTORY),
      };
    }),

  redo: () =>
    set((s) => {
      if (s.future.length === 0) return s;
      const [next, ...rest] = s.future;
      return {
        editSeq: s.editSeq + 1,
        topo: next,
        future: rest,
        past: [...s.past, clone(s.topo)].slice(-MAX_HISTORY),
      };
    }),

  setBaseline: () => set({ policyBaseline: clone(get().topo) }),

  addNode: (partial) => {
    const id = partial?.id ?? uid("n");
    const node: TopoNode = {
      id,
      name: partial?.name ?? `节点-${get().topo.nodes.length + 1}`,
      kind: partial?.kind ?? "host",
      x: partial?.x ?? 120 + Math.round(Math.random() * 400),
      y: partial?.y ?? 100 + Math.round(Math.random() * 300),
      attrs: partial?.attrs,
    };
    set((s) => {
      s.commit();
      return {
        editSeq: s.editSeq + 1,
        topo: { ...s.topo, nodes: [...s.topo.nodes, node] },
        selectedNode: id,
        selectedLink: null,
        selectedRule: null,
      };
    });
    return id;
  },

  updateNode: (id, patch) =>
    set((s) => ({
      editSeq: s.editSeq + 1,
      topo: {
        ...s.topo,
        nodes: s.topo.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
      },
    })),

  removeNode: (id) =>
    set((s) => {
      const removedLinks = new Set(
        s.topo.links.filter((l) => l.source === id || l.target === id).map((l) => l.id),
      );
      s.commit();
      return {
        editSeq: s.editSeq + 1,
        selectedNode: s.selectedNode === id ? null : s.selectedNode,
        selectedLink:
          s.selectedLink &&
          s.topo.links.some((l) => l.id === s.selectedLink && (l.source === id || l.target === id))
            ? null
            : s.selectedLink,
        topo: {
          ...s.topo,
          nodes: s.topo.nodes.filter((n) => n.id !== id),
          links: s.topo.links.filter((l) => l.source !== id && l.target !== id),
          policy: {
            ...s.topo.policy,
            rules: s.topo.policy.rules.filter(
              (r) => r.srcNode !== id && r.dstNode !== id && !(r.linkId && removedLinks.has(r.linkId)),
            ),
          },
        },
      };
    }),

  addLink: (source, target, kind = "undirected") => {
    if (!source || !target || source === target) return null;
    const exists = get().topo.links.some(
      (l) =>
        l.source === source &&
        l.target === target &&
        (kind === "directed" ? l.kind === "directed" : true),
    );
    if (exists) return null;
    const id = uid("l");
    const link: TopoLink = { id, source, target, kind };
    set((s) => {
      s.commit();
      return {
        editSeq: s.editSeq + 1,
        topo: { ...s.topo, links: [...s.topo.links, link] },
        selectedLink: id,
        selectedNode: null,
        selectedRule: null,
      };
    });
    return id;
  },

  updateLink: (id, patch) =>
    set((s) => ({
      editSeq: s.editSeq + 1,
      topo: {
        ...s.topo,
        links: s.topo.links.map((l) => (l.id === id ? { ...l, ...patch } : l)),
      },
    })),

  removeLink: (id) =>
    set((s) => {
      s.commit();
      return {
        editSeq: s.editSeq + 1,
        topo: {
          ...s.topo,
          links: s.topo.links.filter((l) => l.id !== id),
          policy: {
            ...s.topo.policy,
            rules: s.topo.policy.rules.filter((r) => r.linkId !== id),
          },
        },
        selectedLink: s.selectedLink === id ? null : s.selectedLink,
      };
    }),

  addRule: () => {
    const id = uid("r");
    const maxOrder = get().topo.policy.rules.reduce((m, r) => Math.max(m, r.order), 0);
    const rule: Rule = {
      id,
      order: maxOrder + 10,
      enabled: true,
      action: "allow",
      note: "",
    };
    set((s) => {
      s.commit();
      return {
        editSeq: s.editSeq + 1,
        topo: { ...s.topo, policy: { ...s.topo.policy, rules: [...s.topo.policy.rules, rule] } },
        selectedRule: id,
      };
    });
    return id;
  },

  updateRule: (id, patch) =>
    set((s) => ({
      editSeq: s.editSeq + 1,
      topo: {
        ...s.topo,
        policy: {
          ...s.topo.policy,
          rules: s.topo.policy.rules.map((r) => (r.id === id ? { ...r, ...patch } : r)),
        },
      },
    })),

  removeRule: (id) =>
    set((s) => {
      s.commit();
      return {
        editSeq: s.editSeq + 1,
        topo: {
          ...s.topo,
          policy: { ...s.topo.policy, rules: s.topo.policy.rules.filter((r) => r.id !== id) },
        },
        selectedRule: s.selectedRule === id ? null : s.selectedRule,
      };
    }),

  moveRule: (id, delta) =>
    set((s) => {
      const sorted = orderedRules(s.topo.policy.rules);
      const idx = sorted.findIndex((r) => r.id === id);
      const j = idx + delta;
      if (idx < 0 || j < 0 || j >= sorted.length) return s;
      s.commit();
      const a = sorted[idx];
      const b = sorted[j];
      const orderA = a.order;
      const rules = s.topo.policy.rules.map((r) => {
        if (r.id === a.id) return { ...r, order: b.order };
        if (r.id === b.id) return { ...r, order: orderA };
        return r;
      });
      return { editSeq: s.editSeq + 1, topo: { ...s.topo, policy: { ...s.topo.policy, rules } } };
    }),

  setDefaultAction: (a) =>
    set((s) => {
      s.commit();
      return { editSeq: s.editSeq + 1, topo: { ...s.topo, policy: { ...s.topo.policy, defaultAction: a } } };
    }),

  select: (kind, id) =>
    set({
      selectedNode: kind === "node" ? id : null,
      selectedLink: kind === "link" ? id : null,
      selectedRule: kind === "rule" ? id : null,
    }),

  replaceAll: (t, options) =>
    set((s) => {
      const next = clone(t);
      if (options?.keepHistory === false) {
      return { topo: next, editSeq: s.editSeq + 1, past: [], future: [] };
      }
      return {
        editSeq: s.editSeq + 1,
        topo: next,
        past: [...s.past.slice(-MAX_HISTORY + 1), clone(s.topo)],
        future: [],
      };
    }),

  loadSnapshot: (t) =>
    set((s) => ({
      topo: clone(t),
      editSeq: s.editSeq + 1,
      cleanEditSeq: s.editSeq + 1,
      past: [],
      future: [],
    })),

  markClean: () => set((s) => ({ cleanEditSeq: s.editSeq })),

  setHydrated: (v) => set({ hydrated: v }),

  bumpEdit: () => set((s) => ({ editSeq: s.editSeq + 1 })),
}));
