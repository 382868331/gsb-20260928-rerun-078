import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { useState } from "react";
import { CheckPanel } from "./CheckPanel";
import { useTopoStore } from "../state/topoStore";
import type { PathResult } from "../types";

function Harness() {
  const [check, setCheck] = useState<PathResult | null>(null);
  return <CheckPanel check={check} setCheck={setCheck} />;
}

const twoNodes = () => {
  useTopoStore.setState({
    topo: {
      version: 0,
      nodes: [
        { id: "a", name: "A", x: 0, y: 0 },
        { id: "b", name: "B", x: 10, y: 10 },
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
};

describe("CheckPanel", () => {
  beforeEach(() => twoNodes());
  afterEach(() => vi.restoreAllMocks());

  it("renders a readable unreachable verdict when backend arrays are null", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({
          source: "a",
          target: "b",
          reachable: false,
          paths: null,
          blocked: null,
          reason: "no-route",
        }),
      })),
    );
    render(<Harness />);
    fireEvent.change(screen.getAllByRole("combobox")[0], { target: { value: "a" } });
    fireEvent.change(screen.getAllByRole("combobox")[1], { target: { value: "b" } });
    const runBtn = () => screen.getAllByText("检查连接并解释路径");
    fireEvent.click(runBtn()[runBtn().length - 1]);
    await waitFor(() => expect(screen.getByText(/不可达/)).toBeTruthy());
    expect(screen.getByText(/没有物理路由/)).toBeTruthy();
  });

  it("discards a late response when the draft was edited meanwhile", async () => {
    let resolveFetch: (v: unknown) => void = () => {};
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise((resolve) => {
            resolveFetch = resolve;
          }),
      ),
    );
    render(<Harness />);
    fireEvent.change(screen.getAllByRole("combobox")[0], { target: { value: "a" } });
    fireEvent.change(screen.getAllByRole("combobox")[1], { target: { value: "b" } });
    const runBtn = () => screen.getAllByText("检查连接并解释路径");
    fireEvent.click(runBtn()[runBtn().length - 1]);
    // user edits the draft while the slow check is still in flight
    useTopoStore.getState().addNode({ id: "c", name: "C", x: 1, y: 1 });
    resolveFetch({
      ok: true,
      json: async () => ({
        source: "a",
        target: "b",
        reachable: true,
        paths: [{ hops: [], nodeIds: ["a", "b"], reachable: true, length: 1 }],
        blocked: [],
        reason: "allowed",
      }),
    });
    await new Promise((r) => setTimeout(r, 10));
    expect(screen.queryByText(/可达 ✅/)).toBeNull();
  });
});
