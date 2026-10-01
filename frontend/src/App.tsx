import { useEffect, useState } from "react";
import { Canvas } from "./components/Canvas";
import { CheckPanel } from "./components/CheckPanel";
import { DiffPanel } from "./components/DiffPanel";
import { LinkPanel } from "./components/LinkPanel";
import { NodePanel } from "./components/NodePanel";
import { PolicyPanel } from "./components/PolicyPanel";
import { ScenarioBar } from "./components/ScenarioBar";
import { Toolbar } from "./components/Toolbar";
import { api, normalizeTopology } from "./lib/api";
import { loadCheckForVersion, type CachedCheck } from "./lib/checkCache";
import { useTopoStore } from "./state/topoStore";
import type { PathResult } from "./types";

type Tab = "check" | "policy" | "diff";

export function App() {
  const [tab, setTab] = useState<Tab>("check");
  const [check, setCheck] = useState<PathResult | null>(null);
  // checkVersion records which saved version the displayed explanation
  // belongs to, so a stale verdict is never shown against another draft.
  const [checkVersion, setCheckVersion] = useState<number | null>(null);
  const [linkMode, setLinkMode] = useState(false);
  const [linkStart, setLinkStart] = useState<string | null>(null);
  const hydrated = useTopoStore((s) => s.hydrated);

  // Hydrate from the backend on startup: the persisted draft (saved
  // topology, rule order, default action) replaces the placeholder
  // sample, and the check result cached for that version comes back
  // with it. An empty backend draft means nothing was ever saved --
  // the sample then stays visibly marked as an unsaved scratch.
  useEffect(() => {
    let cancelled = false;
    api
      .getDraft()
      .then((raw) => {
        if (cancelled) return;
        const d = normalizeTopology(raw);
        const isEmpty = d.version === 0 && d.nodes.length === 0 && d.links.length === 0 && d.policy.rules.length === 0;
        if (!isEmpty) {
          useTopoStore.getState().loadSnapshot(d);
          const cached = loadCheckForVersion(d.version);
          if (cached) {
            setCheck(cached.result);
            setCheckVersion(cached.version);
          }
        }
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) useTopoStore.getState().setHydrated(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  function onScenarioLoaded(cached: CachedCheck | null) {
    setCheck(cached ? cached.result : null);
    setCheckVersion(cached ? cached.version : null);
  }

  function onCheckResult(version: number, r: PathResult | null) {
    setCheck(r);
    setCheckVersion(r ? version : null);
  }

  return (
    <div className="app">
      <header className="app-header">
        <h1>网络拓扑设计平台</h1>
        <span className="subtitle">离线设计 · 不连接真实网络 · 版本化拓扑（供后续容量规划复用同一版本）</span>
      </header>
      <Toolbar linkMode={linkMode} setLinkMode={setLinkMode} setLinkStart={setLinkStart} />
      <ScenarioBar onScenarioLoaded={onScenarioLoaded} />
      {!hydrated && <div className="hint hydrate-hint">正在恢复已保存的拓扑…</div>}
      <div className="main">
        <div className="canvas-wrap">
          <Canvas check={check} linkMode={linkMode} linkStart={linkStart} setLinkStart={setLinkStart} />
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
            <button className={tab === "diff" ? "active" : ""} onClick={() => setTab("diff")}>方案对比</button>
          </nav>
          <section className="panel tab-body">
            {tab === "check" && <CheckPanel check={check} checkVersion={checkVersion} onCheckResult={onCheckResult} />}
            {tab === "policy" && <PolicyPanel />}
            {tab === "diff" && <DiffPanel />}
          </section>
        </div>
      </div>
    </div>
  );
}
