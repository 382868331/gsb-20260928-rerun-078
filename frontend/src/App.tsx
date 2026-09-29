import { useState } from "react";
import { Canvas } from "./components/Canvas";
import { CheckPanel } from "./components/CheckPanel";
import { DiffPanel } from "./components/DiffPanel";
import { LinkPanel } from "./components/LinkPanel";
import { NodePanel } from "./components/NodePanel";
import { PolicyPanel } from "./components/PolicyPanel";
import { Toolbar } from "./components/Toolbar";
import type { PathResult } from "./types";

type Tab = "check" | "policy" | "diff";

export function App() {
  const [tab, setTab] = useState<Tab>("check");
  const [check, setCheck] = useState<PathResult | null>(null);
  const [linkMode, setLinkMode] = useState(false);
  const [linkStart, setLinkStart] = useState<string | null>(null);

  return (
    <div className="app">
      <header className="app-header">
        <h1>网络拓扑设计平台</h1>
        <span className="subtitle">离线设计 · 不连接真实网络 · 版本化拓扑（供后续容量规划复用同一版本）</span>
      </header>
      <Toolbar linkMode={linkMode} setLinkMode={setLinkMode} setLinkStart={setLinkStart} />
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
            <button className={tab === "diff" ? "active" : ""} onClick={() => setTab("diff")}>策略改动对比</button>
          </nav>
          <section className="panel tab-body">
            {tab === "check" && <CheckPanel check={check} setCheck={setCheck} />}
            {tab === "policy" && <PolicyPanel />}
            {tab === "diff" && <DiffPanel />}
          </section>
        </div>
      </div>
    </div>
  );
}
