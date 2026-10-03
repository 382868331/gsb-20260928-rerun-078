import { useRef, useState } from "react";
import { api, normalizeTopology } from "../lib/api";
import { loadConnections } from "../lib/connCache";
import { normalizeConnDiffReport } from "../lib/normalize";
import { useVersions } from "../lib/versions";
import { useTopoStore } from "../state/topoStore";
import type { ConnDiffReport, HopChange, Topology } from "../types";

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
          {ruleTag(c.beforeRuleId, c.beforeRuleOrder, "前")}
          {ruleTag(c.afterRuleId, c.afterRuleOrder, "后")}
        </li>
      ))}
    </ul>
  );
}

function ruleTag(id?: string, order?: number, side?: string) {
  if (!id) return null;
  return (
    <span className="rule-tag">
      {` ${side}:${id}(#${order ?? "?"})`}
    </span>
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

// pathShape summarizes a path as direct (1 hop), detour, or none, so
// the panel can call out "直达 → 绕行 / 不可达" transitions explicitly.
function pathShape(p: { before: boolean; after: boolean; beforeAt?: { paths?: { length: number }[] }; afterAt?: { paths?: { length: number }[] } }): string {
  const bLen = p.beforeAt?.paths?.[0]?.length;
  const aLen = p.afterAt?.paths?.[0]?.length;
  const shape = (ok: boolean, len?: number) => (!ok ? "不可达" : len === 1 ? "直达" : len ? `绕行(${len}跳)` : "可达");
  return `${shape(p.before, bLen)} → ${shape(p.after, aLen)}`;
}

export function DiffPanel() {
  const topo = useTopoStore((s) => s.topo);
  const dirty = useTopoStore((s) => s.editSeq !== s.cleanEditSeq);
  const { versions } = useVersions();
  const [selA, setSelA] = useState<string>("draft");
  const [selB, setSelB] = useState<string>("draft");
  const [report, setReport] = useState<ConnDiffReport | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const reqId = useRef(0);

  async function resolve(sel: string): Promise<Topology> {
    if (sel === "draft") return topo;
    const snap = await api.loadVersion(Number(sel));
    return normalizeTopology(snap.topology);
  }

  function label(sel: string): string {
    if (sel === "draft") return `当前草稿（基于 v${topo.version || "空"}${dirty ? "，未保存" : ""}）`;
    return `v${sel}`;
  }

  async function compare() {
    setErr("");
    setBusy(true);
    const seq = useTopoStore.getState().editSeq;
    const id = ++reqId.current;
    try {
      const [a, b] = await Promise.all([resolve(selA), resolve(selB)]);
      // New connections (state=new) over all ordered pairs, plus the
      // registered existing connections of the "before" side on their
      // pinned paths -- the two impacts are reported separately.
      const beforeVersion = selA === "draft" ? topo.version : Number(selA);
      const conns = loadConnections(beforeVersion);
      const rep = normalizeConnDiffReport(await api.connDiff(a, b, conns, true));
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        setReport(rep);
      }
    } catch (e) {
      if (reqId.current !== id) return;
      setErr((e as Error).message);
    } finally {
      if (reqId.current === id) setBusy(false);
    }
  }

  const options = (
    <>
      <option value="draft">当前草稿{dirty ? "（未保存）" : ""}</option>
      {versions.map((m) => (
        <option key={m.version} value={String(m.version)}>
          v{m.version} · {m.label || "（无备注）"}
        </option>
      ))}
    </>
  );

  return (
    <div className="diff">
      <div className="baseline-row">
        <label>方案 A
          <select value={selA} onChange={(e) => setSelA(e.target.value)}>{options}</select>
        </label>
        <label>方案 B
          <select value={selB} onChange={(e) => setSelB(e.target.value)}>{options}</select>
        </label>
        <button className="primary" onClick={compare} disabled={busy}>
          {busy ? "比较中…" : "比较两个方案"}
        </button>
      </div>
      <p className="hint">
        对全部有序节点对逐一计算方向；逐跳归因标明是哪条规则（ID + 顺序）、
        默认动作或链路变化使路径在直达 / 绕行 / 不可达之间切换。
      </p>
      {err && <div className="error">{err}</div>}
      {report && (
        <div>
          <div className="summary">
            {label(selA)} → {label(selB)}：新连接（首次请求）共评估 <b>{report.newConns.totalPairs}</b> 个有序方向，
            <span className="now-ok">新增可达 {report.newConns.newReachable.length}</span>，
            <span className="now-no">新被阻断 {report.newConns.newBlocked.length}</span>；
            已有连接 {report.existing.length} 条中
            <span className="now-ok"> 保持 {report.existing.filter((e) => e.afterStatus === "survives").length} 条</span>，
            <span className="now-no">断开 {report.existing.filter((e) => e.afterStatus !== "survives").length} 条</span>
          </div>
          {report.existing.length > 0 && (
            <div className="conn-impact">
              <h4>已有连接影响（按原路径逐段评估 state=established）</h4>
              <ul>
                {report.existing.map((e) => (
                  <li key={e.connId} className={e.afterStatus === "survives" ? "now-ok" : "now-no"}>
                    <code>{e.connId}</code> {e.source} → {e.target}：
                    {e.afterStatus === "survives"
                      ? e.mappingStatus === "kept"
                        ? "保持（沿用原转换）"
                        : "保持"
                      : e.afterStatus === "broken-path"
                        ? "路径断开"
                        : e.afterStatus === "broken-nat"
                          ? "转换失效"
                          : "策略拒绝"}
                    <div className="hint">{e.detail}</div>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {report.existing.length === 0 && (
            <p className="hint">未登记已有连接：可在“连接检查”页试算后登记，此处将分别说明新连接与已有连接的影响。</p>
          )}
          <div className="row2 diff-cols">
            <div>
              <h4 className="now-ok">新连接新增可达</h4>
              <ul>
                {report.newConns.newReachable.map((p) => (
                  <li key={p.source + p.target}>
                    <div>{p.source} → {p.target}<span className="shape">{pathShape(p)}</span></div>
                    <PairChanges changes={p.hopChanges} />
                  </li>
                ))}
                {report.newConns.newReachable.length === 0 && <li className="hint">无</li>}
              </ul>
            </div>
            <div>
              <h4 className="now-no">新连接被阻断</h4>
              <ul>
                {report.newConns.newBlocked.map((p) => (
                  <li key={p.source + p.target}>
                    <div>{p.source} → {p.target}<span className="shape">{pathShape(p)}</span></div>
                    <PairChanges changes={p.hopChanges} />
                  </li>
                ))}
                {report.newConns.newBlocked.length === 0 && <li className="hint">无</li>}
              </ul>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
