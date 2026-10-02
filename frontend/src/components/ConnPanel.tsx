import { useEffect, useRef, useState } from "react";
import { api } from "../lib/api";
import {
  loadConnections,
  loadTrials,
  removeTrial,
  saveConnections,
  saveTrial,
  trialKey,
  type ConnTrial,
} from "../lib/connCache";
import { normalizeConnResult } from "../lib/normalize";
import { uid } from "../lib/id";
import { PathView } from "./PathView";
import { useTopoStore } from "../state/topoStore";
import type { ConnResult, Connection } from "../types";

// ConnPanel runs connection-state-aware what-if trials: first request
// (state=new), keep-alive of an established connection
// (state=established) and return traffic (state=return) follow their
// own rules. Simulated timeout / middle-node disconnect conditions
// are pure trials: they never modify the draft or any saved version.
export function ConnPanel() {
  const topo = useTopoStore((s) => s.topo);
  const version = topo.version;
  const [src, setSrc] = useState("");
  const [dst, setDst] = useState("");
  const [timeoutSim, setTimeoutSim] = useState(false);
  const [downNodes, setDownNodes] = useState<string[]>([]);
  const [downLinks, setDownLinks] = useState<string[]>([]);
  const [result, setResult] = useState<ConnResult | null>(null);
  const [resultVersion, setResultVersion] = useState<number | null>(null);
  const [trials, setTrials] = useState<ConnTrial[]>([]);
  const [conns, setConns] = useState<Connection[]>([]);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const reqId = useRef(0);

  // Restore this version's registered connections and saved trials so
  // every explanation still matches its original topology, policy and
  // connection state after a reload.
  useEffect(() => {
    setConns(loadConnections(version));
    setTrials(loadTrials(version));
  }, [version]);

  function toggle(list: string[], set: (v: string[]) => void, id: string) {
    set(list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);
  }

  async function run() {
    if (!src || !dst) {
      setErr("请选择源和宿");
      return;
    }
    setErr("");
    setBusy(true);
    const seq = useTopoStore.getState().editSeq;
    const id = ++reqId.current;
    try {
      const res = normalizeConnResult(
        await api.connCheck(topo, src, dst, { maxPaths: 3, downNodes, downLinks, timeout: timeoutSim }),
      );
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        setResult(res);
        setResultVersion(version);
        const key = trialKey(src, dst, downNodes, downLinks, timeoutSim);
        const trial: ConnTrial = { key, version, source: src, target: dst, downNodes, downLinks, timeout: timeoutSim, result: res };
        saveTrial(version, trial);
        setTrials(loadTrials(version));
      }
    } catch (e) {
      if (reqId.current !== id) return;
      setErr((e as Error).message);
    } finally {
      if (reqId.current === id) setBusy(false);
    }
  }

  function registerConnection() {
    if (!result || !result.establishable) return;
    const path = result.forward.paths[0];
    const conn: Connection = {
      id: uid("c"),
      source: result.source,
      target: result.target,
      pathNodeIds: path ? path.nodeIds : undefined,
      state: "established",
    };
    const next = [...conns, conn];
    setConns(next);
    saveConnections(version, next);
  }

  function unregister(id: string) {
    const next = conns.filter((c) => c.id !== id);
    setConns(next);
    saveConnections(version, next);
  }

  function loadTrial(t: ConnTrial) {
    setSrc(t.source);
    setDst(t.target);
    setDownNodes(t.downNodes);
    setDownLinks(t.downLinks);
    setTimeoutSim(t.timeout);
    setResult(normalizeConnResult(t.result));
    setResultVersion(t.version);
  }

  const stateLabel: Record<string, string> = {
    established: "已建立",
    expired: "已超时失效",
    broken: "已断开",
    blocked: "无法建立",
  };
  const phaseLabel: Record<string, string> = {
    establish: "建立（首次请求 state=new）",
    maintain: "保持（已建立连接 state=established）",
    return: "回程（state=return，方向反转）",
    teardown: "失效（超时 / 中间节点断开）",
  };

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

      <fieldset className="trial-conds">
        <legend>试算条件（不影响当前草稿与任何已保存方案）</legend>
        <label className="inline">
          <input type="checkbox" checked={timeoutSim} onChange={(e) => setTimeoutSim(e.target.checked)} />
          模拟连接空闲超时
        </label>
        <div className="cond-group">
          <span>断开中间节点：</span>
          {topo.nodes.map((n) => (
            <label key={n.id} className="inline">
              <input
                type="checkbox"
                checked={downNodes.includes(n.id)}
                onChange={() => toggle(downNodes, setDownNodes, n.id)}
              />
              {n.name}
            </label>
          ))}
        </div>
        <div className="cond-group">
          <span>断开链路：</span>
          {topo.links.map((l) => (
            <label key={l.id} className="inline">
              <input
                type="checkbox"
                checked={downLinks.includes(l.id)}
                onChange={() => toggle(downLinks, setDownLinks, l.id)}
              />
              {l.id}
            </label>
          ))}
        </div>
      </fieldset>

      <button className="primary" disabled={busy} onClick={run}>
        {busy ? "计算中…" : "运行连接状态试算"}
      </button>
      {err && <div className="error">{err}</div>}

      {result && (
        <div className="conn-result">
          <div className="check-version-tag">
            {resultVersion
              ? `以下试算对应已保存版本 v${resultVersion}，不改变其他方案`
              : "以下试算对应当前未保存草稿，不改变其他方案"}
          </div>
          <div className={result.bidirectional ? "verdict ok" : "verdict no"}>
            {result.source} → {result.target}：连接状态 = {stateLabel[result.state] ?? result.state}
            {result.asymmetric && (
              <span className="warn-tag">非对称返回：去程放行但回程不通，不能认作全程可达</span>
            )}
          </div>
          {result.phases.map((ph) => (
            <div key={ph.phase} className={ph.ok ? "phase ok" : "phase no"}>
              <div className="phase-head">
                <b>{phaseLabel[ph.phase] ?? ph.phase}</b>
                <span>{ph.ok ? "成立" : "不成立"}（{ph.reason}）</span>
              </div>
              <div className="phase-detail">{ph.detail}</div>
              {ph.path && <PathView r={ph.path} />}
            </div>
          ))}
          {result.establishable && (
            <button onClick={registerConnection}>将首条可行路径登记为已有连接（供方案对比评估影响）</button>
          )}
        </div>
      )}

      {conns.length > 0 && (
        <div className="conn-list">
          <h4>已登记的已有连接（版本 v{version}）</h4>
          <ul>
            {conns.map((c) => (
              <li key={c.id}>
                <code>{c.id}</code> {c.source} → {c.target}
                {c.pathNodeIds && <span className="hint">（路径：{c.pathNodeIds.join(" → ")}）</span>}
                <button onClick={() => unregister(c.id)}>移除</button>
              </li>
            ))}
          </ul>
          <p className="hint">在“方案对比”页可查看策略/路由修改后，这些已有连接与新连接分别受到的影响。</p>
        </div>
      )}

      {trials.length > 0 && (
        <div className="trial-list">
          <h4>本版本已保存的试算（重开后仍对应原拓扑、策略与连接状态）</h4>
          <ul>
            {trials.map((t) => (
              <li key={t.key}>
                <button onClick={() => loadTrial(t)}>
                  {t.source} → {t.target}
                  {t.timeout && "＋超时"}
                  {t.downNodes.length > 0 && `＋断节点:${t.downNodes.join(",")}`}
                  {t.downLinks.length > 0 && `＋断链路:${t.downLinks.join(",")}`}
                </button>
                <span className={t.result.bidirectional ? "hint" : "warn-tag"}>
                  {stateLabel[t.result.state] ?? t.result.state}
                </span>
                <button onClick={() => { removeTrial(version, t.key); setTrials(loadTrials(version)); }}>删除</button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

