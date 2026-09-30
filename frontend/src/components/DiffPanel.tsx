import { useRef, useState } from "react";
import { api } from "../lib/api";
import { useTopoStore } from "../state/topoStore";
import type { DiffReport, HopChange } from "../types";

function PairChanges({ changes }: { changes?: HopChange[] }) {
  const list = changes ?? [];
  if (list.length === 0) {
    return <div className="hint">逐跳归因：未定位到具体规则或链路变化</div>;
  }
  return (
    <ul className="hop-changes">
      {list.map((c, i) => (
        <li key={i} className={`cause cause-${c.cause}`}>
          <code>{c.linkId || "—"}</code> {c.from} → {c.to}：{causeText(c.cause)}
          {c.detail ? ` ${c.detail}` : ""}
          <span className="before-after">
            {`  [${actionText(c.beforeAction)} → ${actionText(c.afterAction)}]`}
          </span>
        </li>
      ))}
    </ul>
  );
}

function causeText(cause: string): string {
  switch (cause) {
    case "rule-reordered": return "规则重排改变首条命中";
    case "rule-changed": return "规则内容修改";
    case "rule-enabled": return "规则被启用";
    case "rule-disabled": return "规则被停用";
    case "rule-added": return "新增规则";
    case "rule-removed": return "规则被删除";
    case "default-changed": return "默认动作变化";
    case "link-added": return "链路新增（直达/绕行变化）";
    case "link-removed": return "链路删除";
    case "link-direction-changed": return "链路方向变化";
    default: return cause;
  }
}

function actionText(a?: string): string {
  switch (a) {
    case "allow": return "放行";
    case "deny": return "拒绝";
    case "default-allow": return "默认放行";
    case "default-deny": return "默认拒绝";
    case "no-route": return "无路由";
    default: return a ?? "—";
  }
}

export function DiffPanel() {
  const topo = useTopoStore((s) => s.topo);
  const baseline = useTopoStore((s) => s.policyBaseline);
  const setBaseline = useTopoStore((s) => s.setBaseline);
  const [report, setReport] = useState<DiffReport | null>(null);
  const [err, setErr] = useState("");
  const reqId = useRef(0);

  async function compare() {
    if (!baseline) {
      setErr("请先点击“以当前策略为基线”");
      return;
    }
    setErr("");
    const seq = useTopoStore.getState().editSeq;
    const id = ++reqId.current;
    try {
      const rep = await api.diff(baseline, topo, false);
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        setReport(rep);
      }
    } catch (e) {
      if (reqId.current !== id) return;
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
                  <li key={p.source + p.target}>
                    <div>{p.source} → {p.target}</div>
                    <PairChanges changes={p.hopChanges} />
                  </li>
                ))}
                {report.newReachable.length === 0 && <li className="hint">无</li>}
              </ul>
            </div>
            <div>
              <h4 className="now-no">被阻断</h4>
              <ul>
                {report.newBlocked.map((p) => (
                  <li key={p.source + p.target}>
                    <div>{p.source} → {p.target}</div>
                    <PairChanges changes={p.hopChanges} />
                  </li>
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
