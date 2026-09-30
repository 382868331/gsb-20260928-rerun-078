import { useState } from "react";
import { api } from "../lib/api";
import { useTopoStore } from "../state/topoStore";
import type { DiffReport, PairResult } from "../types";

export function DiffPanel() {
  const topo = useTopoStore((s) => s.topo);
  const baseline = useTopoStore((s) => s.policyBaseline);
  const setBaseline = useTopoStore((s) => s.setBaseline);
  const [report, setReport] = useState<DiffReport | null>(null);
  const [err, setErr] = useState("");

  async function compare() {
    if (!baseline) {
      setErr("请先点击“以当前策略为基线”");
      return;
    }
    setErr("");
    try {
      setReport(await api.diff(baseline, topo, true));
    } catch (e) {
      setErr((e as Error).message);
    }
  }

  return (
    <div className="diff">
      <div className="baseline-row">
        <button onClick={setBaseline}>以当前策略为基线</button>
        <button className="primary" onClick={compare} disabled={!baseline}>
          比较：哪些连接新增可达 / 被阻断
        </button>
      </div>
      {baseline && (
        <p className="hint">基线 v{baseline.version}（{baseline.policy.rules.length} 条规则），对全部有序节点对逐一计算方向。</p>
      )}
      {err && <div className="error">{err}</div>}
      {report && (
        <div>
          <div className="summary">
            共评估 <b>{report.totalPairs}</b> 个有序方向：
            <span className="now-ok">新增可达 {report.newReachable.length}</span>，
            <span className="now-no">新被阻断 {report.newBlocked.length}</span>
          </div>
          <div className="row2 diff-cols">
            <div>
              <h4 className="now-ok">新增可达</h4>
              {report.newReachable.length === 0 && <p className="hint">无</p>}
              {report.newReachable.map((p) => <PairCard key={p.source + p.target} p={p} />)}
            </div>
            <div>
              <h4 className="now-no">被阻断</h4>
              {report.newBlocked.length === 0 && <p className="hint">无</p>}
              {report.newBlocked.map((p) => <PairCard key={p.source + p.target} p={p} />)}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function kindText(k: PairResult["beforeKind"]): string {
  switch (k) {
    case "direct": return "直达";
    case "detour": return "绕行";
    case "denied": return "默认/规则拒绝";
    case "no-route": return "无物理路由";
    default: return k;
  }
}

function PairCard({ p }: { p: PairResult }) {
  const changed = (p.segments ?? []).filter((s) => s.cause !== "none");
  return (
    <div className="pair-card">
      <div className="pair-head">
        <b>{p.source} → {p.target}</b>
        <span className="hint">{kindText(p.beforeKind)} → {kindText(p.afterKind)}</span>
      </div>
      <div className="pair-paths">
        <div>修订前：{(p.beforePath ?? []).length ? (p.beforePath ?? []).join(" → ") : "—"}</div>
        <div>修订后：{(p.afterPath ?? []).length ? (p.afterPath ?? []).join(" → ") : "—"}</div>
      </div>
      {p.summary && <div className="pair-summary">{p.summary}</div>}
      <ul className="seg-list">
        {changed.map((s, i) => (
          <li key={i} className={"seg seg-" + s.cause}>
            <code>{s.from} → {s.to}</code>
            {s.linkId ? <>（链路 <code>{s.linkId}</code>）</> : null}：{s.detail}
          </li>
        ))}
        {changed.length === 0 && <li className="hint">无逐段规则差异（节点/链路拓扑变动）</li>}
      </ul>
    </div>
  );
}
