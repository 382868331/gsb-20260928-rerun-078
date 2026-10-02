import { useMemo, useRef, useState } from "react";
import { api, normalizeTopology } from "../lib/api";
import {
  deleteConnTrial,
  listConnTrials,
  saveConnTrial,
  type ConnTrial,
} from "../lib/connCache";
import { useVersions } from "../lib/versions";
import { useTopoStore } from "../state/topoStore";
import type { ConnCheckResult, ConnDiffReport, ConnPhase, PathInfo, Topology } from "../types";

// normalizeConnResult guarantees every array the UI maps over exists,
// so a null field in a saved or fetched result can never blank the page.
function normalizeConnResult(r: ConnCheckResult): ConnCheckResult {
  return {
    ...r,
    forwardNodes: r.forwardNodes ?? [],
    returnNodes: r.returnNodes ?? [],
    phases: (r.phases ?? []).map((p) => ({
      ...p,
      paths: p.paths ?? [],
      blocked: p.blocked ?? [],
      notes: p.notes ?? [],
    })),
  };
}

// trialTopology builds the what-if topology: optionally with one
// intermediate node (and its links / referencing rules) disconnected.
// It is a detached clone -- a trial never edits the working draft or
// any saved scenario.
function trialTopology(topo: Topology, downNode: string): Topology {
  if (!downNode) return topo;
  const removedLinks = new Set(
    topo.links.filter((l) => l.source === downNode || l.target === downNode).map((l) => l.id),
  );
  return {
    ...topo,
    nodes: topo.nodes.filter((n) => n.id !== downNode),
    links: topo.links.filter((l) => l.source !== downNode && l.target !== downNode),
    policy: {
      ...topo.policy,
      rules: topo.policy.rules.filter(
        (r) =>
          r.srcNode !== downNode &&
          r.dstNode !== downNode &&
          !(r.linkId && removedLinks.has(r.linkId)),
      ),
    },
  };
}

export function ConnPanel() {
  const topo = useTopoStore((s) => s.topo);
  const [src, setSrc] = useState("");
  const [dst, setDst] = useState("");
  const [afterTimeout, setAfterTimeout] = useState(false);
  const [downNode, setDownNode] = useState("");
  const [result, setResult] = useState<ConnCheckResult | null>(null);
  const [trials, setTrials] = useState<ConnTrial[]>(() => listConnTrials(topo.version));
  const [viewing, setViewing] = useState<ConnTrial | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const reqId = useRef(0);

  const isTrial = afterTimeout || downNode !== "";

  async function run() {
    if (!src || !dst) {
      setErr("请选择源和宿");
      return;
    }
    setErr("");
    setBusy(true);
    setViewing(null);
    const seq = useTopoStore.getState().editSeq;
    const id = ++reqId.current;
    try {
      const t = trialTopology(topo, downNode);
      const res = normalizeConnResult(await api.connCheck(t, src, dst, 3, afterTimeout));
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        setResult(res);
      }
    } catch (e) {
      if (reqId.current !== id) return;
      setErr((e as Error).message);
    } finally {
      if (reqId.current === id) setBusy(false);
    }
  }

  function saveTrial() {
    if (!result) return;
    const t = saveConnTrial({
      version: topo.version,
      source: result.source,
      target: result.target,
      afterTimeout,
      downNode,
      result,
    });
    setTrials(listConnTrials(topo.version));
    setViewing(t);
  }

  function removeTrial(id: string) {
    deleteConnTrial(id);
    setTrials(listConnTrials(topo.version));
    if (viewing?.id === id) setViewing(null);
  }

  const shown = viewing ? viewing.result : result;
  const intermediate = useMemo(
    () => topo.nodes.filter((n) => n.id !== src && n.id !== dst),
    [topo.nodes, src, dst],
  );

  return (
    <div className="conn">
      <div className="row2">
        <label>从
          <select value={src} onChange={(e) => setSrc(e.target.value)}>
            <option value="">选择源…</option>
            {topo.nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.id})</option>)}
          </select>
        </label>
        <label>到
          <select value={dst} onChange={(e) => setDst(e.target.value)}>
            <option value="">选择宿…</option>
            {topo.nodes.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.id})</option>)}
          </select>
        </label>
      </div>
      <fieldset className="trial-box">
        <legend>试算条件（不改变当前草稿与其他方案）</legend>
        <label className="inline">
          <input
            type="checkbox"
            checked={afterTimeout}
            onChange={(e) => setAfterTimeout(e.target.checked)}
          />
          模拟连接已超时（保持/返回流量按新连接规则重审）
        </label>
        <label>模拟中间节点断开
          <select value={downNode} onChange={(e) => setDownNode(e.target.value)}>
            <option value="">（不断开）</option>
            {intermediate.map((n) => <option key={n.id} value={n.id}>{n.name} ({n.id})</option>)}
          </select>
        </label>
      </fieldset>
      <div className="btn-row">
        <button className="primary" disabled={busy} onClick={run}>
          {busy ? "计算中…" : isTrial ? "按试算条件检查" : "检查连接状态"}
        </button>
        {result && !viewing && (
          <button onClick={saveTrial}>保存本次{isTrial ? "试算" : "检查"}结果</button>
        )}
      </div>
      {err && <div className="error">{err}</div>}
      {viewing && (
        <div className="check-version-tag">
          正在查看已保存试算：v{viewing.version || "草稿"} · {viewing.source} → {viewing.target}
          {viewing.afterTimeout ? " · 已超时" : ""}
          {viewing.downNode ? " · 断开 " + viewing.downNode : ""}
          {" · 保存于 "}{new Date(viewing.savedAt).toLocaleString()}
          <button className="link-btn" onClick={() => setViewing(null)}>返回当前结果</button>
        </div>
      )}
      {shown && <ConnExplanation r={shown} />}

      {trials.length > 0 && (
        <div className="trials">
          <h4>已保存的检查 / 试算（对应版本 v{topo.version || "草稿"}，重开后仍按原拓扑与连接状态解释）</h4>
          <ul>
            {trials.map((t) => (
              <li key={t.id}>
                <button className="link-btn" onClick={() => setViewing(t)}>
                  {t.source} → {t.target}
                  {t.afterTimeout ? " · 超时" : ""}
                  {t.downNode ? " · 断开 " + t.downNode : ""}
                  {" · "}{t.result.reachable ? "可达" : "不可达"}
                </button>
                <span className="hint"> {new Date(t.savedAt).toLocaleString()} </span>
                <button className="link-btn danger" onClick={() => removeTrial(t.id)}>删除</button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <ConnImpact />
    </div>
  );
}

function ConnExplanation({ r }: { r: ConnCheckResult }) {
  return (
    <div className="explanation">
      <div className={r.reachable ? "verdict ok" : "verdict no"}>
        {r.source} → {r.target}：{r.reachable ? "连接可建立且双向可达 ✅" : "连接不可用 ❌"}
        <span className="reason">{connReasonText(r.reason)}</span>
      </div>
      {r.asymmetric && (
        <div className="warn">
          去程与返程路径不对称（去程 {r.forwardNodes.join(" → ")}，返程 {r.returnNodes.join(" → ")}）：
          去程的一次放行不代表返程可达，返程已独立验证。
        </div>
      )}
      {r.phases.map((p) => (
        <PhaseView key={p.name} p={p} />
      ))}
    </div>
  );
}

function PhaseView({ p }: { p: ConnPhase }) {
  return (
    <div className="phase">
      <h4>
        {p.title}
        <span className={p.reachable ? "verdict-mini ok" : "verdict-mini no"}>
          {p.reachable ? "通过" : "不通过"}
        </span>
        <span className="reason">{phaseReasonText(p.reason)}</span>
      </h4>
      {p.paths.map((path, i) => (
        <PathView key={"p" + i} label={"可行路径 " + (i + 1)} p={path} />
      ))}
      {p.blocked.map((path, i) => (
        <PathView key={"b" + i} label={"被阻断候选 " + (i + 1)} p={path} blocked />
      ))}
      {p.notes.length > 0 && (
        <ul className="notes">
          {p.notes.map((n, i) => <li key={i}>{n}</li>)}
        </ul>
      )}
    </div>
  );
}

function PathView({ label, p, blocked }: { label: string; p: PathInfo; blocked?: boolean }) {
  return (
    <div className={blocked ? "path blocked-path" : "path"}>
      <div className="path-nodes">{label}：{(p.nodeIds ?? []).join(" → ") || "（无节点）"}（{p.length} 跳）</div>
      <ol className="hops">
        {(p.hops ?? []).map((h, j) => (
          <li key={j} className={h.action === "deny" ? "deny-hop" : ""}>
            <span className={h.action}>{h.action === "allow" ? "放行" : "拒绝"}</span>
            {" " + h.from + " → " + h.to} 经链路 <code>{h.linkId}</code>
            {h.reverse && <em>（反向走无向链路）</em>}
            {h.ruleId
              ? <>，命中规则 <code>{h.ruleId}</code>（顺序 {h.ruleOrder}）</>
              : "，无规则命中 → 取默认动作"}
          </li>
        ))}
      </ol>
    </div>
  );
}

// ConnImpact compares a saved baseline with the current draft and
// reports the impact on new vs existing connections separately.
function ConnImpact() {
  const topo = useTopoStore((s) => s.topo);
  const dirty = useTopoStore((s) => s.editSeq !== s.cleanEditSeq);
  const { versions } = useVersions();
  const [baseline, setBaseline] = useState("");
  const [report, setReport] = useState<ConnDiffReport | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const reqId = useRef(0);

  async function compare() {
    if (!baseline) {
      setErr("请选择作为基线的已保存版本");
      return;
    }
    setErr("");
    setBusy(true);
    const seq = useTopoStore.getState().editSeq;
    const id = ++reqId.current;
    try {
      const snap = await api.loadVersion(Number(baseline));
      const before = normalizeTopology(snap.topology);
      const rep = await api.connDiff(before, topo, true);
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        setReport({
          ...rep,
          newConnImpact: rep.newConnImpact ?? [],
          estConnImpact: rep.estConnImpact ?? [],
          timeoutRisks: rep.timeoutRisks ?? [],
        });
      }
    } catch (e) {
      if (reqId.current !== id) return;
      setErr((e as Error).message);
    } finally {
      if (reqId.current === id) setBusy(false);
    }
  }

  return (
    <div className="conn-impact">
      <h4>修改影响：新连接与已有连接分别评估</h4>
      <div className="baseline-row">
        <label>基线版本
          <select value={baseline} onChange={(e) => setBaseline(e.target.value)}>
            <option value="">选择版本…</option>
            {versions.map((m) => (
              <option key={m.version} value={m.version}>v{m.version} · {m.label || "（无备注）"}</option>
            ))}
          </select>
        </label>
        <button onClick={compare} disabled={busy}>
          {busy ? "评估中…" : dirty ? "与当前草稿（未保存）比较" : "与当前草稿比较"}
        </button>
      </div>
      <p className="hint">
        规则重排、连接超时与节点断开的影响按连接状态分别列出；已有连接不因为新连接规则收紧而立刻中断。
      </p>
      {err && <div className="error">{err}</div>}
      {report && (
        <div>
          <div className="summary">
            共评估 <b>{report.totalPairs}</b> 个有序方向：
            新连接受影响 <b>{report.newConnImpact.length}</b>，
            已有连接受影响 <b>{report.estConnImpact.length}</b>，
            超时风险 <b>{report.timeoutRisks.length}</b>
          </div>
          <ImpactList title="新连接影响" pairs={report.newConnImpact} kind="new" />
          <ImpactList title="已有连接影响" pairs={report.estConnImpact} kind="est" />
          <ImpactList title="连接超时风险（当前可保持，超时后无法重建）" pairs={report.timeoutRisks} kind="risk" />
        </div>
      )}
    </div>
  );
}

function ImpactList({ title, pairs, kind }: { title: string; pairs: ConnDiffReport["newConnImpact"]; kind: "new" | "est" | "risk" }) {
  return (
    <div className="impact-list">
      <h5>{title}</h5>
      {pairs.length === 0 && <div className="hint">无</div>}
      <ul>
        {pairs.map((p) => (
          <li key={p.source + "->" + p.target + kind}>
            <b>{p.source} → {p.target}</b>
            {kind !== "risk" && (
              <span className={(kind === "new" ? p.newChange : p.estChange) === "now-reachable" ? "now-ok" : "now-no"}>
                {" "}{kind === "new"
                  ? (p.newChange === "now-reachable" ? "新连接变为可达" : "新连接变为不可达")
                  : (p.estChange === "now-reachable" ? "已有连接恢复" : "已有连接中断")}
              </span>
            )}
            <ul className="notes">
              {(p.notes ?? []).map((n, i) => <li key={i}>{n}</li>)}
            </ul>
          </li>
        ))}
      </ul>
    </div>
  );
}

function connReasonText(reason: string): string {
  switch (reason) {
    case "two-way-established": return "（首次请求与返回流量均通过）";
    case "establish-blocked": return "（首次请求被规则拒绝或无路由）";
    case "return-blocked": return "（去程可过，但返回流量不可达）";
    default: return reason;
  }
}

function phaseReasonText(reason: string): string {
  switch (reason) {
    case "allowed": return "（存在放行路径）";
    case "all-paths-denied": return "（所有候选路径被规则拦截）";
    case "no-route": return "（按链路方向无路由）";
    case "same-node": return "（源宿相同）";
    case "explained": return "";
    case "no-established-path": return "（没有已建立的路径）";
    default: return reason;
  }
}
