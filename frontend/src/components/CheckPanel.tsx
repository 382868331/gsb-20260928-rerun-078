import { useRef, useState } from "react";
import { api } from "../lib/api";
import { saveCheckForVersion } from "../lib/checkCache";
import { normalizePathResult } from "../lib/normalize";
import { PathView, reasonText } from "./PathView";
import { useTopoStore } from "../state/topoStore";
import type { PathResult } from "../types";

interface Props {
  check: PathResult | null;
  // checkVersion is the saved version the displayed explanation was
  // computed against; null when it belongs to an unsaved draft.
  checkVersion: number | null;
  onCheckResult: (version: number, r: PathResult | null) => void;
}

export function CheckPanel({ check, checkVersion, onCheckResult }: Props) {
  const topo = useTopoStore((s) => s.topo);
  const [src, setSrc] = useState("");
  const [dst, setDst] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const reqId = useRef(0);

  async function run() {
    if (!src || !dst) {
      setErr("请选择源和宿");
      return;
    }
    setErr("");
    setBusy(true);
    const seq = useTopoStore.getState().editSeq;
    const version = topo.version;
    const id = ++reqId.current;
    try {
      const res = normalizePathResult(await api.check(topo, src, dst, 3));
      // A topology edit or scenario switch may have happened while the
      // check was in flight. Never let a late result overwrite
      // explanations for the now-current version.
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        onCheckResult(version, res);
        saveCheckForVersion(version, src, dst, res);
      }
    } catch (e) {
      if (reqId.current !== id) return;
      setErr((e as Error).message);
    } finally {
      if (reqId.current === id) setBusy(false);
    }
  }

  return (
    <div className="check">
      <div className="row2">
        <label>从
          <select value={src} onChange={(e) => setSrc(e.target.value)}>
            <option value="">选择源…</option>
            {topo.nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.id})</option>)}
          </select>
        </label>
        <label>到（方向独立计算）
          <select value={dst} onChange={(e) => setDst(e.target.value)}>
            <option value="">选择宿…</option>
            {topo.nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.id})</option>)}
          </select>
        </label>
      </div>
      <button disabled={busy} onClick={run}>{busy ? "计算中…" : "检查连接并解释路径"}</button>
      {err && <div className="error">{err}</div>}
      {check && (
        <div className="check-version-tag">
          {checkVersion
            ? `以下解释对应已保存版本 v${checkVersion}`
            : "以下解释对应当前未保存草稿"}
        </div>
      )}
      {check && (
        <div>
          <div className={check.reachable ? "verdict ok" : "verdict no"}>
            {check.source} → {check.target}：{check.reachable ? "可达 ✅" : "不可达 ❌"}
            <span className="reason">（{reasonText(check.reason)}）</span>
          </div>
          <PathView r={check} />
          <p className="hint">结论依据逐跳规则评估，而不是“两点间有线即连通”。</p>
        </div>
      )}
    </div>
  );
}

