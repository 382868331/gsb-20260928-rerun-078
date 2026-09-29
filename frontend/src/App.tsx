import { useRef, useState } from "react";
import { Canvas } from "./components/Canvas";
import { NodePanel } from "./components/NodePanel";
import { ReachPanel, DiffPanel } from "./components/ReachPanel";
import { RulePanel } from "./components/RulePanel";
import { serverPayload, useStore } from "./store";
import { api } from "./api";
import type { DiffResult, ReachResult } from "./types";
import { sampleTopology } from "./sample";

export default function App() {
  const { topo, history, undo, exportJson, importJson, addNode, setTopo } = useStore();
  const [result, setResult] = useState<ReachResult | null>(null);
  const [diff, setDiff] = useState<DiffResult | null>(null);
  const [toast, setToast] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);

  function flash(msg: string) {
    setToast(msg);
    window.setTimeout(() => setToast(""), 2500);
  }

  async function save() {
    try {
      const saved = await api.save(serverPayload(topo));
      flash(`已保存版本 ${(saved.version ?? "").slice(0, 10)}（节点 ID 不变）`);
    } catch (e) {
      flash(`保存失败：${(e as Error).message}`);
    }
  }

  async function serverUndo() {
    undo();
    try { await api.undo(); } catch { /* 本地撤销优先；后端栈为空时忽略 */ }
    flash("已撤销到上一个编辑版本");
  }

  function doExport() {
    const blob = new Blob([exportJson()], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "topology.json";
    a.click();
    URL.revokeObjectURL(url);
  }

  function onImportFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    file.text().then((raw) => {
      try {
        importJson(raw);
        setResult(null);
        setDiff(null);
        flash("导入成功：节点 / 链路 / 规则 ID 全部保留");
      } catch (err) {
        flash(`导入失败：${(err as Error).message}`);
      }
    });
    e.target.value = "";
  }

  return (
    <div className="app">
      <header className="topbar">
        <h1>网络拓扑设计平台</h1>
        <span className="tag">离线仿真 · 不连接真实网络 · 拓扑版本驱动</span>
        <div className="spacer" />
        <button onClick={() => addNode()}>加节点</button>
        <button onClick={() => { setTopo(structuredClone(sampleTopology)); setResult(null); setDiff(null); }}>加载示例</button>
        <button onClick={save}>保存版本</button>
        <button onClick={serverUndo} disabled={history.length === 0}>撤销({history.length})</button>
        <button onClick={doExport}>导出 JSON</button>
        <button onClick={() => fileRef.current?.click()}>导入 JSON</button>
        <input ref={fileRef} type="file" accept="application/json" hidden onChange={onImportFile} />
      </header>
      <div className="layout">
        <div className="col side">
          <NodePanel />
          <RulePanel />
        </div>
        <div className="col canvas-col">
          <Canvas activePath={result?.activePath} candidatePaths={result?.paths ?? []} />
        </div>
        <div className="col side">
          <ReachPanel result={result} setResult={setResult} />
          <DiffPanel diff={diff} setDiff={setDiff} />
        </div>
      </div>
      {toast && <div className="toast">{toast}</div>}
    </div>
  );
}
