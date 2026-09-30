import { describe, expect, it, beforeEach } from "vitest";
import { useTopoStore } from "./topoStore";

describe("topoStore identity and history", () => {
  beforeEach(() => {
    useTopoStore.setState({
      topo: {
        version: 0,
        nodes: [
          { id: "a", name: "A", x: 0, y: 0 },
          { id: "b", name: "B", x: 0, y: 0 },
        ],
        links: [],
        policy: { defaultAction: "deny" as const, rules: [] },
      },
      past: [],
      future: [],
      selectedNode: null,
      selectedLink: null,
      selectedRule: null,
      policyBaseline: null,
    });
  });

  it("adds links with stable ids and supports undo", () => {
    const s = useTopoStore.getState();
    const linkId = s.addLink("a", "b", "directed");
    expect(linkId).toBeTruthy();
    expect(useTopoStore.getState().topo.links[0].source).toBe("a");
    useTopoStore.getState().undo();
    expect(useTopoStore.getState().topo.links).toHaveLength(0);
    useTopoStore.getState().redo();
    expect(useTopoStore.getState().topo.links[0].id).toBe(linkId);
  });

  it("rule reordering preserves rule identities", () => {
    const s = useTopoStore.getState();
    const r1 = s.addRule();
    const r2 = s.addRule();
    useTopoStore.getState().updateRule(r1, { order: 10 });
    useTopoStore.getState().updateRule(r2, { order: 20 });
    useTopoStore.getState().moveRule(r1, 1);
    const rules = useTopoStore.getState().topo.policy.rules;
    const byId = Object.fromEntries(rules.map((r) => [r.id, r.order]));
    expect(byId[r1]).toBe(20);
    expect(byId[r2]).toBe(10);
  });

  it("removing a node drops attached links but keeps node ids elsewhere", () => {
    const s = useTopoStore.getState();
    s.addLink("a", "b", "undirected");
    s.addNode({ id: "c", name: "C", x: 1, y: 1 });
    const idB = useTopoStore.getState().topo.nodes.find((n) => n.id === "b")!.id;
    expect(idB).toBe("b");
    useTopoStore.getState().removeNode("a");
    const after = useTopoStore.getState().topo;
    expect(after.links).toHaveLength(0);
    expect(after.nodes.map((n) => n.id)).toEqual(["b", "c"]);
  });

  it("bumps editSeq on every draft edit so late check results can be discarded", () => {
    const before = useTopoStore.getState().editSeq;
    const id = useTopoStore.getState().addNode({ id: "z", name: "Z", x: 0, y: 0 });
    expect(useTopoStore.getState().editSeq).toBe(before + 1);
    useTopoStore.getState().updateNode(id, { name: "Z2" });
    expect(useTopoStore.getState().editSeq).toBe(before + 2);
    const rid = useTopoStore.getState().addRule();
    const seq3 = useTopoStore.getState().editSeq;
    useTopoStore.getState().updateRule(rid, { action: "deny" });
    expect(useTopoStore.getState().editSeq).toBe(seq3 + 1);
    useTopoStore.getState().setDefaultAction("allow");
    expect(useTopoStore.getState().editSeq).toBe(seq3 + 2);
    useTopoStore.getState().removeNode(id);
    expect(useTopoStore.getState().editSeq).toBe(seq3 + 3);
  });
});
