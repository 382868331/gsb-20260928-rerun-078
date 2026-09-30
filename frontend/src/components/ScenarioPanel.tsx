import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import { fingerprint } from "../lib/fingerprint";
import { useTopoStore } from "../state/topoStore";
import type { DiffReport, PairResult, PathResult, Topology } from "../types";
import { PairChanges } from "./DiffPanel";

interface VersionMeta {
  version: number;
  label: string;
  created: string;
}

interface Props {
  // Called whenever the draft is replaced by a scenario switch, so the
  // parent can drop check results that belonged to the old scenario.
  onSwitched: () => void;
}

// bestPathLabel describes how a pair is connected under one scenario:
// direct (1 hop), detour (n hops), or unreachable.
function bestPathLabel(pr?: PathResult): string {
  if (!pr || !pr.reachable) return "不可达";
  const p = (pr.paths ?? [])[0];
  if (!p) return "可达";
  return p.length <= 1 ? "直达（1 跳）" : `绕行（${p.length} 跳）`;
}

function bestPathKey(pr?: PathResult): string {
  const p = (pr?.paths ?? [])[0];
  return p ? p.nodeIds.join(">") : "";
}

export function ScenarioPanel({ onSwitched }: Props) {
  const topo = useTopoStore((s) => s.topo);
  const savedFingerprint = useTopoStore((s) => s.savedFingerprint);
  const dirty = fingerprint(topo) !== savedFingerprint;
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [err, setErr] = useState("");
  const [pendingSwitch, setPendingSwitch] = useState<number | null>(null);
  const [cmpA, setCmpA] = useState("");
  const [cmpB, setCmpB] = useState("");
  const [report, setReport] = useState<DiffReport | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setVersions(await api.versions());
    } catch (e) {
      setErr((e as Error).message);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh, topo.version]);

  async function doSwitch(v: number) {
    setErr("");
    try {
      const snap = await api.loadVersion(v);
      // A scenario switch replaces the draft wholesale and clears the
      // undo history: undo must never resurrect another scenario's
      // content inside this one.
      useTopoStore.getState().replaceAll(snap.topology, { keepHistory: false });
      useTopoStore.getState().markSaved();
      setPendingSwitch(null);
      setReport(null);
      onSwitched();
    } catch (e) {
      setErr((e as Error).message);
    }
  }

  function requestSwitch(v: number) {
    if (dirty) {
      // Unsaved edits exist: force an explicit decision instead of
      // silently discarding them or overwriting the target scenario.
      setPendingSwitch(v);
    } else {
      doSwitch(v);
    }
  }

  async function saveThenSwitch(v: number) {
    setErr("");
    try {
      const snap = await api.save(topo, `切换前保存于 ${new Date().toLocaleString()}`);
      useTopoStore.getState().replaceAll(snap.topology, { keepHistory: false });
      useTopoStore.getState().markSaved();
      await refresh();
      await doSwitch(v);
    } catch (e) {
      setErr((e as Error).message);
    }
  }

  async function loadSelection(sel: string): Promise<Topology> {
    if (sel === "draft") return topo;
    const snap = await api.loadVersion(Number(sel));
    return snap.topology;
  }

  async function compare() {
    if (!cmpA || !cmpB || cmpA === cmpB) {
      setErr("请选择两个不同的方案进行比较");
      return;
    }
    setErr("");
    setBusy(true);
    try {
      const [a, b] = await Promise.all([loadSelection(cmpA), loadSelection(cmpB)]);
      setReport(await api.diff(a, b, true));
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const selLabel = (sel: string) =>
    sel === "draft" ? "当前草稿" : `v${sel}`;

  // Pairs still reachable under both scenarios but whose chosen path
  // changed (direct -> detour, detour -> direct, different detour).
  const pathChanged: PairResult[] = (report?.unchanged ?? []).filter(
    (p) =>
      p.before &&
      p.after &&
      bestPathKey(p.beforeAt) !== bestPathKey(p.afterAt),
  );

  return (
    <div className="scenarios">
      <h4>策略方案（已保存版本）</h4>
      <div className="baseline-row">
        <button onClick={refresh}>刷新列表</button>
        {dirty && <span className="dirty-tag">当前草稿有未保存修改</span>}
      </div>
      {versions.length === 0 && (
        <p className="hint">还没有已保存的方案。在工具栏“保存版本”后，这里会出现可切换、可比较的方案。</p>
      )}
      <ul className="scenario-list">
        {versions.map((m) => (
          <li key={m.version} className={m.version === topo.version && !dirty ? "current" : ""}>
            <span>
              <b>v{m.version}</b> {m.label || "（无备注）"}
              <em> {m.created}</em>
              {m.version === topo.version && !dirty && <em>（当前）</em>}
            </span>
            <button onClick={() => requestSwitch(m.version)}>切换到此方案</button>
          </li>
        ))}
      </ul>

      {pendingSwitch !== null && (
        <div className="modal-mask">
          <div className="modal">
            <h4>当前草稿有未保存的修改</h4>
            <p>
              切换到 v{pendingSwitch} 之前，请选择如何处理这些修改。
              历史方案不会被覆盖：保存会生成一个新版本。
            </p>
            <div className="modal-actions">
              <button className="primary" onClick={() => saveThenSwitch(pendingSwitch)}>
                保存为新版本并切换
              </button>
              <button onClick={() => doSwitch(pendingSwitch)}>放弃修改并切换</button>
              <button onClick={() => setPendingSwitch(null)}>取消</button>
            </div>
          </div>
        </div>
      )}

      <h4>方案比较</h4>
      <div className="row2">
        <label>方案甲
          <select value={cmpA} onChange={(e) => setCmpA(e.target.value)}>
            <option value="">选择…</option>
            <option value="draft">当前草稿{dirty ? "（未保存）" : ""}</option>
            {versions.map((m) => <option key={m.version} value={String(m.version)}>v{m.version} {m.label}</option>)}
          </select>
        </label>
        <label>方案乙
          <select value={cmpB} onChange={(e) => setCmpB(e.target.value)}>
            <option value="">选择…</option>
            <option value="draft">当前草稿{dirty ? "（未保存）" : ""}</option>
            {versions.map((m) => <option key={m.version} value={String(m.version)}>v{m.version} {m.label}</option>)}
          </select>
        </label>
      </div>
      <button className="primary" disabled={busy} onClick={compare}>
        {busy ? "比较中…" : "比较两个方案（逐跳归因）"}
      </button>
      {err && <div className="error">{err}</div>}

      {report && (
        <div className="scenario-diff">
          <div className="summary">
            {selLabel(cmpA)} → {selLabel(cmpB)}：共评估 <b>{report.totalPairs}</b> 个有序方向，
            <span className="now-ok">新增可达 {report.newReachable.length}</span>，
            <span className="now-no">新被阻断 {report.newBlocked.length}</span>，
            路径变化 {pathChanged.length}
          </div>

          {report.newReachable.length > 0 && (
            <div>
              <h4 className="now-ok">不可达 → 可达</h4>
              {report.newReachable.map((p) => (
                <PairRow key={p.source + p.target} pair={p} />
              ))}
            </div>
          )}
          {report.newBlocked.length > 0 && (
            <div>
              <h4 className="now-no">可达 → 不可达</h4>
              {report.newBlocked.map((p) => (
                <PairRow key={p.source + p.target} pair={p} />
              ))}
            </div>
          )}
          {pathChanged.length > 0 && (
            <div>
              <h4>可达性不变，但路径改变（直达 ↔ 绕行）</h4>
              {pathChanged.map((p) => (
                <PairRow key={p.source + p.target} pair={p} />
              ))}
            </div>
          )}
          {report.newReachable.length === 0 &&
            report.newBlocked.length === 0 &&
            pathChanged.length === 0 && (
              <p className="hint">两个方案对所有有序节点对的可达性与首选路径完全一致。</p>
            )}
        </div>
      )}
    </div>
  );
}

function PairRow({ pair }: { pair: PairResult }) {
  const beforePath = (pair.beforeAt?.paths ?? [])[0];
  const afterPath = (pair.afterAt?.paths ?? [])[0];
  return (
    <div className="pair-row">
      <div>
        <code>{pair.source} → {pair.target}</code>：
        <span className={pair.before ? "now-ok" : "now-no"}>{bestPathLabel(pair.beforeAt)}</span>
        {" → "}
        <span className={pair.after ? "now-ok" : "now-no"}>{bestPathLabel(pair.afterAt)}</span>
      </div>
      {beforePath && (
        <div className="hint">甲路径：{beforePath.nodeIds.join(" → ")}</div>
      )}
      {afterPath && (
        <div className="hint">乙路径：{afterPath.nodeIds.join(" → ")}</div>
      )}
      <PairChanges changes={pair.hopChanges} />
    </div>
  );
}
