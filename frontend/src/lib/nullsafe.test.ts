import { describe, expect, it } from "vitest";
import { normalizeTopology } from "./api";
import { normalizePathResult } from "./checkCache";

describe("null-safe hydration (first-load white screen regression)", () => {
  it("normalizeTopology tolerates null and missing policy", () => {
    const empty = normalizeTopology(null);
    expect(empty.nodes).toEqual([]);
    expect(empty.links).toEqual([]);
    expect(empty.policy.rules).toEqual([]);
    expect(empty.policy.defaultAction).toBe("deny");
    const half = normalizeTopology({ version: 3 } as never);
    expect(half.version).toBe(3);
    expect(half.nodes).toEqual([]);
    expect(half.policy.rules).toEqual([]);
  });

  it("normalizeTopology converts null arrays to empty lists", () => {
    const t = normalizeTopology({
      version: 1,
      nodes: null,
      links: null,
      policy: { defaultAction: "allow", rules: null },
    } as never);
    expect(t.nodes).toEqual([]);
    expect(t.links).toEqual([]);
    expect(t.policy.rules).toEqual([]);
    expect(t.policy.defaultAction).toBe("allow");
  });

  it("normalizePathResult converts null path lists", () => {
    const r = normalizePathResult({
      source: "a", target: "b", reachable: false,
      paths: null, blocked: null, reason: "",
    } as never);
    expect(r.paths).toEqual([]);
    expect(r.blocked).toEqual([]);
    expect(r.reason).toBe("no-route");
  });
});

