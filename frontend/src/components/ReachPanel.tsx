import { useState } from "react";
import { api } from "../api";
import { useStore, serverPayload } from "../store";
import type { DiffResult, Path, ReachResult } from "../types";

const PROTOCOLS = ["tcp", "udp", "icmp"];

function Trace({ p }: { p: Path }) {
  const { topo } = useStore();
  const name = (id: string) => topo.nodes.find((n) => n.id === id)?.name ?? id;
  return (
    <div className={p.feasible ? "trace ok" : "trace denied"}>
      <div className="trace-route">
        {p.nodes.map((id, i) => {
          const d = p.decisions[i];
          const blocked = p.blockedAt === i;
          return (
            <span key={id} className={blocked ? "hop blocked" : "hop"}>
              {name(id)}
              <small>
                {d.action === "permit" ? "OK " : "DENY "}
                {d.ruleId === "default-deny" ? "default-deny" : d.ruleId}
              </small>
            </span>
          );
        })}
      </div>
    </div>
  );
}

export function ReachPanel({ result, setResult }: { result: ReachResult | null; setResult: (r: ReachResult | null) => void }) {
  const { topo } = useStore();
  const [src, setSrc] = useState("");
  const [dst, setDst] = useState("");
  const [proto, setProto] = useState("tcp");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function run() {
    setError("");
    if (!src || !dst) { setError("请选择源节点和目的节点"); return; }
    setBusy(true);
    try {
      await api.save(serverPayload(topo));
      setResult(await api.evaluate({ src, dst, protocol: proto }, 5));
    } catch (e) {
      setError((e as Error).message);
    } finally { setBusy(false); }
  }

  return (
    <section className="panel">
      <h2>连接可达性解释</h2>
      <div className="row">
        <select value={src} onChange={(e) => setSrc(e.target.value)}>
          <option value="">源节点</option>
          {topo.nodes.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
        </select>
        <span>→</span>
        <select value={dst} onChange={(e) => setDst(e.target.value)}>
          <option value="">目的节点</option>
          {topo.nodes.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
        </select>
        <select value={proto} onChange={(e) => setProto(e.target.value)}>
          {PROTOCOLS.map((p) => <option key={p}>{p}</option>)}
        </select>
        <button disabled={busy} onClick={run}>{busy ? "计算中…" : "能否通过？"}</button>
      </div>
      {error && <div className="error">{error}</div>}
      {result && (
        <div className="result">
          <div className={result.reachable ? "verdict ok" : "verdict denied"}>
            {result.reachable ? "可达 ✓" : "不可达 ✕"}
            <span className="reason">{result.reason}</span>
          </div>
          <h3>候选路径与实际命中的规则（共 {result.paths.length} 条）</h3>
          {result.paths.map((p, i) => (
            <div key={i} className="path-item">
              <strong className={p === result.activePath ? "ok" : ""}>
                路径 {i + 1}{p === result.activePath ? "（实际采用）" : p.feasible ? "（可行备选）" : "（被阻断）"}
              </strong>
              <Trace p={p} />
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

export function DiffPanel({ diff, setDiff }: { diff: DiffResult | null; setDiff: (d: DiffResult | null) => void }) {
  const { topo } = useStore();
  const [pairs, setPairs] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function compare() {
    setError("");
    setBusy(true);
    try {
      const before = await api.draft().catch(() => null);
      if (!before) throw new Error("后端无已保存版本，请先执行一次可达性计算或保存");
      const after = await api.save(serverPayload(topo));
      const conns = parsePairs(pairs, topo.nodes.map((n) => n.id));
      setDiff(await api.diff(before.version as string, conns, 5, after.version));
    } catch (e) {
      setError((e as Error).message);
    } finally { setBusy(false); }
  }

  return (
    <section className="panel">
      <h2>策略改动影响对比</h2>
      <p className="muted">比较“上次保存版本 → 当前编辑”。可留空对所有节点对（tcp）计算，或按 src,dst,protocol 每行一对填写。</p>
      <textarea rows={3} value={pairs} onChange={(e) => setPairs(e.target.value)} placeholder="n-h1,n-h2,tcp" />
      <button disabled={busy} onClick={compare}>{busy ? "比较中…" : "比较新增可达 / 被阻断"}</button>
      {error && <div className="error">{error}</div>}
      {diff && (
        <div className="diff">
          <h3>新增可达（{diff.added.length}）</h3>
          {diff.added.map((c, i) => (
            <div key={i} className="change ok">{connText(c.conn)} — {c.afterReason}</div>
          ))}
          {diff.added.length === 0 && <div className="muted">无</div>}
          <h3>被阻断（{diff.blocked.length}）</h3>
          {diff.blocked.map((c, i) => (
            <div key={i} className="change denied">{connText(c.conn)} — {c.afterReason}</div>
          ))}
          {diff.blocked.length === 0 && <div className="muted">无</div>}
          <div className="muted">未变化：{diff.unchanged}　版本：{diff.fromVersion.slice(0, 8)} → {diff.toVersion.slice(0, 8)}</div>
        </div>
      )}
    </section>
  );
}

function connText(c: { src: string; dst: string; protocol: string }) {
  return `${c.src} → ${c.dst} [${c.protocol}]`;
}

function parsePairs(text: string, nodeIds: string[]) {
  const lines = text.split("\n").map((l) => l.trim()).filter(Boolean);
  if (lines.length > 0) {
    return lines.map((l) => {
      const [src, dst, protocol = "tcp"] = l.split(",").map((x) => x.trim());
      return { src, dst, protocol };
    });
  }
  const conns: { src: string; dst: string; protocol: string }[] = [];
  for (let i = 0; i < nodeIds.length; i++) {
    for (let j = 0; j < nodeIds.length; j++) {
      if (i !== j) conns.push({ src: nodeIds[i], dst: nodeIds[j], protocol: "tcp" });
    }
  }
  return conns;
}
