import { describe, expect, it } from "vitest";
import { normalizeConnResult, normalizeDiffReport, normalizePathResult } from "./normalize";
import type { ConnResult, DiffReport, PathResult } from "../types";

// Regression: null arrays from the backend used to blank the whole
// page on first load. Normalizers must always produce real arrays.
describe("normalize null-safety", () => {
  it("path result arrays are never null", () => {
    const r = normalizePathResult({ source: "a", target: "b", reachable: false } as unknown as PathResult);
    expect(r.paths).toEqual([]);
    expect(r.blocked).toEqual([]);
    expect(r.reason).toBe("no-route");
  });

  it("diff report lists are never null", () => {
    const r = normalizeDiffReport({ totalPairs: 2 } as unknown as DiffReport);
    expect(r.newReachable).toEqual([]);
    expect(r.newBlocked).toEqual([]);
  });

  it("conn result phases and phase paths are never null", () => {
    const raw = {
      source: "a", target: "b", state: "blocked",
      forward: { source: "a", target: "b", reachable: false },
      maintain: { source: "a", target: "b", reachable: false },
      return: { source: "b", target: "a", reachable: false },
      phases: [{ phase: "establish", ok: false, reason: "no-route", detail: "x", path: null }],
    } as unknown as ConnResult;
    const r = normalizeConnResult(raw);
    expect(r.phases).toHaveLength(1);
    expect(r.forward.paths).toEqual([]);
    expect(r.phases[0].path).toBeUndefined();
  });
});

