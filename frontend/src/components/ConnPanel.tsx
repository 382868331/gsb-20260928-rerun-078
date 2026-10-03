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
  const [srcPort, setSrcPort] = useState(49152);
  const [dstPort, setDstPort] = useState(80);
  const [expiredConns, setExpiredConns] = useState<string[]>([]);
  const [natRanges, setNatRanges] = useState<Record<string, [number, number]>>({});
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const reqId = useRef(0);

  const natDevices = topo.nodes.filter((n) => n.nat?.enabled);

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
        await api.connCheck(topo, src, dst, {
          maxPaths: 3, downNodes, downLinks, timeout: timeoutSim,
          srcPort, dstPort,
          existing: conns.filter((c) => (c.mappings?.length ?? 0) > 0),
          expiredConns,
          natRanges,
        }),
      );
      if (reqId.current === id && useTopoStore.getState().editSeq === seq) {
        setResult(res);
        setResultVersion(version);
        const key = trialKey(src, dst, downNodes, downLinks, timeoutSim, srcPort, dstPort, expiredConns, natRanges);
        const trial: ConnTrial = {
          key, version, source: src, target: dst, downNodes, downLinks, timeout: timeoutSim,
          srcPort, dstPort, expiredConns, natRanges, result: res,
        };
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
      srcAddr: addrOf(result.source),
      srcPort,
      dstAddr: addrOf(result.target),
      dstPort,
      mappings: result.newMappings,
    };
    const next = [...conns, conn];
    setConns(next);
    saveConnections(version, next);
  }

  function addrOf(id: string): string {
    return topo.nodes.find((n) => n.id === id)?.attrs?.ip || id;
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
    setSrcPort(t.srcPort || 49152);
    setDstPort(t.dstPort || 80);
    setExpiredConns(t.expiredConns ?? []);
    setNatRanges(t.natRanges ?? {});
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
  const dirLabel: Record<string, string> = {
    forward: "去程",
    return: "回程",
    "late-return": "迟到回程",
  };
  const decisionLabel: Record<string, string> = {
    "new-mapping": "新建映射",
    matched: "命中映射",
    expired: "映射已超时",
    "port-reused": "端口被新连接占用",
    "no-mapping": "无对应映射",
    "pool-exhausted": "转换池耗尽",
    "range-changed": "转换范围已改变",
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
        <div className="cond-group">
          <span>连接端口：</span>
          <label className="inline">源端口
            <input type="number" className="port" value={srcPort} onChange={(e) => setSrcPort(Number(e.target.value))} />
          </label>
          <label className="inline">宿端口
            <input type="number" className="port" value={dstPort} onChange={(e) => setDstPort(Number(e.target.value))} />
          </label>
        </div>
        <label className="inline">
          <input type="checkbox" checked={timeoutSim} onChange={(e) => setTimeoutSim(e.target.checked)} />
          模拟连接空闲超时
        </label>
        {conns.some((c) => (c.mappings?.length ?? 0) > 0) && (
          <div className="cond-group">
            <span>模拟已有连接映射超时：</span>
            {conns.filter((c) => (c.mappings?.length ?? 0) > 0).map((c) => (
              <label key={c.id} className="inline">
                <input
                  type="checkbox"
                  checked={expiredConns.includes(c.id)}
                  onChange={() => toggle(expiredConns, setExpiredConns, c.id)}
                />
                {c.id}（{c.source}→{c.target}）
              </label>
            ))}
          </div>
        )}
        {natDevices.length > 0 && (
          <div className="cond-group">
            <span>模拟转换范围改变：</span>
            {natDevices.map((n) => {
              const ov = natRanges[n.id];
              return (
                <span key={n.id} className="inline">
                  <label className="inline">
                    <input
                      type="checkbox"
                      checked={!!ov}
                      onChange={(e) => {
                        const next = { ...natRanges };
                        if (e.target.checked) next[n.id] = [n.nat!.portStart, n.nat!.portEnd];
                        else delete next[n.id];
                        setNatRanges(next);
                      }}
                    />
                    {n.name || n.id}
                  </label>
                  {ov && (
                    <>
                      <input
                        type="number" className="port" value={ov[0]}
                        onChange={(e) => setNatRanges({ ...natRanges, [n.id]: [Number(e.target.value), ov[1]] })}
                      />
                      <input
                        type="number" className="port" value={ov[1]}
                        onChange={(e) => setNatRanges({ ...natRanges, [n.id]: [ov[0], Number(e.target.value)] })}
                      />
                    </>
                  )}
                </span>
              );
            })}
          </div>
        )}
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
          {result.natEvents.length > 0 && (
            <div className="nat-trace">
              <h4>地址转换轨迹（同一条连接在各设备前后的地址与端口）</h4>
              <table>
                <thead>
                  <tr><th>方向</th><th>设备</th><th>转换前</th><th>转换后</th><th>决定</th><th>依据</th></tr>
                </thead>
                <tbody>
                  {result.natEvents.map((ev, i) => (
                    <tr key={i} className={ev.decision === "matched" || ev.decision === "new-mapping" ? "nat-ok" : "nat-bad"}>
                      <td>{dirLabel[ev.direction] ?? ev.direction}</td>
                      <td>{ev.device}</td>
                      <td>{ev.preAddr}:{ev.prePort}</td>
                      <td>{ev.postAddr ? `${ev.postAddr}:${ev.postPort}` : "—"}</td>
                      <td>{decisionLabel[ev.decision] ?? ev.decision}</td>
                      <td>{ev.detail}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <div className="nat-maps">
                <div>
                  <b>本次新连接选用的转换：</b>
                  {result.newMappings.length === 0 && <span className="hint">（路径上没有地址转换设备）</span>}
                  <ul>
                    {result.newMappings.map((m, i) => (
                      <li key={i}>{m.device}：{m.insideAddr}:{m.insidePort} → {m.natAddr}:{m.natPort}（对端 {m.peerAddr}:{m.peerPort}）</li>
                    ))}
                  </ul>
                </div>
                <div>
                  <b>已有连接沿用原转换（未改动）：</b>
                  {result.keptMappings.length === 0 && <span className="hint">（无）</span>}
                  <ul>
                    {result.keptMappings.map((m, i) => (
                      <li key={i}>[{m.connId}] {m.device}：{m.insideAddr}:{m.insidePort} → {m.natAddr}:{m.natPort}</li>
                    ))}
                  </ul>
                </div>
              </div>
            </div>
          )}
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
