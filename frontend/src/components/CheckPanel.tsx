import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import { useTopoStore } from "../state/topoStore";
import type { PathResult } from "../types";

interface Props {
  check: PathResult | null;
  setCheck: (r: PathResult | null) => void;
}

export function CheckPanel({ check, setCheck }: Props) {
  const topo = useTopoStore((s) => s.topo);
  const [src, setSrc] = useState("");
  const [dst, setDst] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  // Monotonic token: a check response landing after a newer edit or
  // request is discarded, so late results never overwrite new edits.
  const runSeq = useRef(0);

  useEffect(() => {
    runSeq.current += 1;
  }, [topo]);

  async function run() {
    if (!src || !dst) {
      setErr("请选择源和宿");
      return;
    }
    setErr("");
    setBusy(true);
    const seq = ++runSeq.current;
    const snapshot = JSON.stringify(topo);
    try {
      const res = await api.check(topo, src, dst, 3);
      // The draft changed while the backend was computing: stale result.
      if (
        seq !== runSeq.current ||
        JSON.stringify(useTopoStore.getState().topo) !== snapshot
      ) {
        return;
      }
      setCheck(res);
    } catch (e) {
      if (seq === runSeq.current) setErr((e as Error).message);
    } finally {
      if (seq === runSeq.current) setBusy(false);
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
      {check && <Explanation r={check} />}
    </div>
  );
}

function Explanation({ r }: { r: PathResult }) {
  const paths = Array.isArray(r.paths) ? r.paths : [];
  const blocked = Array.isArray(r.blocked) ? r.blocked : [];
  return (
    <div className="explanation">
      <div className={r.reachable ? "verdict ok" : "verdict no"}>
        {r.source} → {r.target}：{r.reachable ? "可达 ✅" : "不可达 ❌"}
        <span className="reason">{reasonText(r.reason)}</span>
      </div>
      {!r.reachable && (
        <p className="hint">
          {r.reason === "all-paths-denied"
            ? "物理链路存在候选路径，但每条至少有一跳被有序规则或默认动作拒绝。"
            : r.reason === "no-route"
              ? "按链路方向没有物理路由（缺链路或方向相反），与策略放行无关。"
              : "当前没有可行路径，详见下方候选路径的逐跳解释。"}
        </p>
      )}

      {paths.length > 0 && (
        <div>
          <h4>可行路径（{paths.length} 条，最短优先，可作绕行）</h4>
          {paths.map((p, i) => (
            <div key={i} className="path">
              <div className="path-nodes">路径{i + 1}：{p.nodeIds.join(" → ")}（{p.length} 跳）</div>
              <ol className="hops">
                {(p.hops ?? []).map((h, j) => (
                  <li key={j}>
                    <span className={h.action}>{h.action === "allow" ? "放行" : "拒绝"}</span>
                    {` ${h.from} → ${h.to}`} 经链路 <code>{h.linkId}</code>
                    {h.reverse && <em>（反向走无向链路）</em>}
                    {h.ruleId
                      ? <>，命中规则 <code>{h.ruleId}</code>（顺序 {h.ruleOrder}）</>
                      : "，无规则命中 → 取默认动作"}
                  </li>
                ))}
              </ol>
            </div>
          ))}
        </div>
      )}

      {blocked.length > 0 && (
        <div>
          <h4>被策略阻断的候选路径</h4>
          {blocked.map((p, i) => (
            <div key={i} className="path blocked-path">
              <div className="path-nodes">{p.nodeIds.join(" → ")}</div>
              <ol className="hops">
                {(p.hops ?? []).map((h, j) => (
                  <li key={j} className={h.action === "deny" ? "deny-hop" : ""}>
                    <span className={h.action}>{h.action === "allow" ? "放行" : "拒绝"}</span>
                    {` ${h.from} → ${h.to}`} 经 <code>{h.linkId}</code>
                    {h.ruleId
                      ? <>，规则 <code>{h.ruleId}</code>（顺序 {h.ruleOrder}）</>
                      : "，默认动作"}
                  </li>
                ))}
              </ol>
            </div>
          ))}
        </div>
      )}
      <p className="hint">结论依据逐跳规则评估，而不是“两点间有线即连通”。</p>
    </div>
  );
}

function reasonText(reason: string): string {
  switch (reason) {
    case "allowed": return "（存在策略全放行的路径）";
    case "all-paths-denied": return "（有物理/拓扑路径，但每一条都被规则拦截）";
    case "no-route": return "（按链路方向无可达路由）";
    case "same-node": return "（源宿相同）";
    default: return reason;
  }
}
