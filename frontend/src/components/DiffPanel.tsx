import { useState } from "react";
import { api } from "../lib/api";
import { useTopoStore } from "../state/topoStore";
import type { DiffReport } from "../types";

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
      setReport(await api.diff(baseline, topo, false));
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
              <ul>
                {report.newReachable.map((p) => (
                  <li key={p.source + p.target}>{p.source} → {p.target}</li>
                ))}
                {report.newReachable.length === 0 && <li className="hint">无</li>}
              </ul>
            </div>
            <div>
              <h4 className="now-no">被阻断</h4>
              <ul>
                {report.newBlocked.map((p) => (
                  <li key={p.source + p.target}>{p.source} → {p.target}</li>
                ))}
                {report.newBlocked.length === 0 && <li className="hint">无</li>}
              </ul>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
