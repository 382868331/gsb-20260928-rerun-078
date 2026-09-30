import { useRef, useState } from "react";
import { api } from "../lib/api";
import { useTopoStore } from "../state/topoStore";
import type { Topology } from "../types";

interface Props {
  linkMode: boolean;
  setLinkMode: (v: boolean) => void;
  setLinkStart: (v: string | null) => void;
}

export function Toolbar({ linkMode, setLinkMode, setLinkStart }: Props) {
  const topo = useTopoStore((s) => s.topo);
  const past = useTopoStore((s) => s.past);
  const future = useTopoStore((s) => s.future);
  const undo = useTopoStore((s) => s.undo);
  const redo = useTopoStore((s) => s.redo);
  const addNode = useTopoStore((s) => s.addNode);
  const replaceAll = useTopoStore((s) => s.replaceAll);
  const fileRef = useRef<HTMLInputElement>(null);
  const [msg, setMsg] = useState("");
  const [versions, setVersions] = useState<{ version: number; label: string }[]>([]);

  async function listVersions() {
    try {
      setVersions(await api.versions());
    } catch (e) {
      setMsg("版本列表不可用：" + (e as Error).message);
    }
  }

  async function openVersion(v: number) {
    if (!v) return;
    try {
      const snap = await api.loadVersion(v);
      replaceAll(snap.topology, { keepHistory: false });
      setMsg("已打开不可变版本 v" + v + "（同一版本的检查解释保持一致）");
    } catch (e) {
      setMsg("打开版本失败：" + (e as Error).message);
    }
  }

  async function save() {
    setMsg("");
    try {
      const label = prompt("保存标签（版本备注）", `编辑于 ${new Date().toLocaleString()}`);
      if (label === null) return;
      const snap = await api.save(topo, label);
      replaceAll(snap.topology);
      setMsg(`已保存为版本 v${snap.meta.version}`);
    } catch (e) {
      setMsg((e as Error).message);
    }
  }

  function exportJSON() {
    const blob = new Blob([JSON.stringify(topo, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `topology-v${topo.version || "draft"}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function importJSON(file: File) {
    setMsg("");
    try {
      const text = await file.text();
      const parsed = JSON.parse(text) as Topology;
      // Server normalizes but never rewrites IDs: import keeps identity.
      const normalized = await api.importTopo(parsed);
      replaceAll(normalized, { keepHistory: false });
      setMsg(`已导入：${normalized.nodes.length} 节点 / ${normalized.links.length} 链路 / ${normalized.policy.rules.length} 规则，身份保持不变`);
    } catch (e) {
      setMsg("导入失败：" + (e as Error).message);
    }
  }

  return (
    <div className="toolbar">
      <button onClick={() => addNode()}>＋ 节点</button>
      <button className={linkMode ? "active" : ""} onClick={() => { setLinkMode(!linkMode); setLinkStart(null); }}>
        {linkMode ? "取消连线" : "连线模式"}
      </button>
      <span className="sep" />
      <button onClick={undo} disabled={past.length === 0}>撤销({past.length})</button>
      <button onClick={redo} disabled={future.length === 0}>重做({future.length})</button>
      <span className="sep" />
      <button className="primary" onClick={save}>保存版本</button>
      <select
        value=""
        onClick={listVersions}
        onChange={(e) => openVersion(Number(e.target.value))}
        title="重新打开已保存的不可变版本"
      >
        <option value={0}>打开版本…</option>
        {versions.map((m) => (
          <option key={m.version} value={m.version}>
            v{m.version} {m.label}
          </option>
        ))}
      </select>
      <button onClick={exportJSON}>导出 JSON</button>
      <button onClick={() => fileRef.current?.click()}>导入 JSON</button>
      <input
        ref={fileRef}
        type="file"
        accept="application/json"
        style={{ display: "none" }}
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) importJSON(f);
          e.target.value = "";
        }}
      />
      <span className="version-tag">当前版本：v{topo.version || "未保存草稿"}</span>
      {msg && <span className="msg">{msg}</span>}
    </div>
  );
}
