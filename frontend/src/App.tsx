import { useEffect, useRef, useState } from "react";
import { Canvas } from "./components/Canvas";
import { CheckPanel, LAST_CHECK_KEY, type CheckState } from "./components/CheckPanel";
import { DiffPanel } from "./components/DiffPanel";
import { LinkPanel } from "./components/LinkPanel";
import { NodePanel } from "./components/NodePanel";
import { PolicyPanel } from "./components/PolicyPanel";
import { ScenarioPanel } from "./components/ScenarioPanel";
import { Toolbar } from "./components/Toolbar";
import { api } from "./lib/api";
import { fingerprint } from "./lib/fingerprint";
import { useTopoStore } from "./state/topoStore";
import type { Topology } from "./types";

type Tab = "check" | "policy" | "diff" | "scenarios";

function hasContent(t: Topology | null): t is Topology {
  return !!t && (
    t.version > 0 ||
    (t.nodes?.length ?? 0) > 0 ||
    (t.links?.length ?? 0) > 0 ||
    (t.policy?.rules?.length ?? 0) > 0
  );
}

function normalize(t: Topology): Topology {
  return {
    ...t,
    nodes: t.nodes ?? [],
    links: t.links ?? [],
    policy: {
      defaultAction: t.policy?.defaultAction ?? "deny",
      rules: t.policy?.rules ?? [],
    },
  };
}

export function App() {
  const [tab, setTab] = useState<Tab>("check");
  const [check, setCheck] = useState<CheckState | null>(null);
  const [linkMode, setLinkMode] = useState(false);
  const [linkStart, setLinkStart] = useState<string | null>(null);
  const topo = useTopoStore((s) => s.topo);
  const hydrated = useRef(false);

  // Hydrate from the server-side persisted draft on first load. A
  // refresh must restore the user's saved topology, rule order and the
  // check result computed for exactly that topology — never the
  // factory sample pretending to be the saved version.
  useEffect(() => {
    (async () => {
      try {
        const draft = await api.getDraft();
        if (hasContent(draft)) {
          const t = normalize(draft);
          useTopoStore.getState().replaceAll(t, { keepHistory: false });
          useTopoStore.getState().markSaved();
          try {
            const raw = localStorage.getItem(LAST_CHECK_KEY);
            if (raw) {
              const saved = JSON.parse(raw) as { fp: string; result: CheckState["result"] };
              if (saved.fp === fingerprint(t) && saved.result) {
                setCheck({ result: saved.result, seq: useTopoStore.getState().editSeq });
              }
            }
          } catch {
            /* corrupted cache: ignore */
          }
        }
      } catch {
        /* backend offline: keep the local sample as an unsaved draft */
      }
      hydrated.current = true;
    })();
  }, []);

  // Keep the server-side draft in sync (debounced) so a reload or
  // server restart comes back to exactly this editing state.
  useEffect(() => {
    if (!hydrated.current) return;
    const timer = setTimeout(() => {
      api.putDraft(topo).catch(() => {});
    }, 600);
    return () => clearTimeout(timer);
  }, [topo]);

  return (
    <div className="app">
      <header className="app-header">
        <h1>网络拓扑设计平台</h1>
        <span className="subtitle">离线设计 · 不连接真实网络 · 版本化拓扑（供后续容量规划复用同一版本）</span>
      </header>
      <Toolbar linkMode={linkMode} setLinkMode={setLinkMode} setLinkStart={setLinkStart} />
      <div className="main">
        <div className="canvas-wrap">
          <Canvas check={check?.result ?? null} linkMode={linkMode} linkStart={linkStart} setLinkStart={setLinkStart} />
          {linkMode && (
            <div className="link-hint">
              连线模式：{linkStart ? "再点一个节点完成链路" : "先点起点节点"}
            </div>
          )}
          <div className="side-stack">
            <section className="panel">
              <h3>节点属性</h3>
              <NodePanel />
            </section>
            <section className="panel">
              <h3>链路属性</h3>
              <LinkPanel />
            </section>
          </div>
        </div>
        <div className="right-panel">
          <nav className="tabs">
            <button className={tab === "check" ? "active" : ""} onClick={() => setTab("check")}>连通性解释</button>
            <button className={tab === "policy" ? "active" : ""} onClick={() => setTab("policy")}>策略规则</button>
            <button className={tab === "diff" ? "active" : ""} onClick={() => setTab("diff")}>策略改动对比</button>
            <button className={tab === "scenarios" ? "active" : ""} onClick={() => setTab("scenarios")}>方案切换/比较</button>
          </nav>
          <section className="panel tab-body">
            {tab === "check" && <CheckPanel check={check} setCheck={setCheck} />}
            {tab === "policy" && <PolicyPanel />}
            {tab === "diff" && <DiffPanel />}
            {tab === "scenarios" && <ScenarioPanel onSwitched={() => setCheck(null)} />}
          </section>
        </div>
      </div>
    </div>
  );
}
