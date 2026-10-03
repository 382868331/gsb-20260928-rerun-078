import type { PathResult } from "../types";

// Shared rendering of one PathResult (allowed + blocked candidates
// with per-hop rule traces). Used by the plain reachability check and
// by every connection-lifecycle phase.
export function PathView({ r, title }: { r: PathResult; title?: string }) {
  const paths = r.paths ?? [];
  const blocked = r.blocked ?? [];
  return (
    <div className="explanation">
      {title && <div className="path-title">{title}</div>}
      {paths.length > 0 && (
        <div>
          <h4>可行路径（{paths.length} 条，最短优先）</h4>
          {paths.map((p, i) => (
            <div key={i} className="path">
              <div className="path-nodes">路径{i + 1}：{(p.nodeIds ?? []).join(" → ")}（{p.length} 跳）</div>
              {p.detail && <div className="nat-decision nat-denied">{p.detail}</div>}
              <ol className="hops">
                {(p.hops ?? []).map((h, j) => (
                  <li key={j}>
                    <span className={h.action}>{h.action === "allow" ? "放行" : "拒绝"}</span>
                    {` ${h.from} → ${h.to}`} 经链路 <code>{h.linkId}</code>
                    {h.reverse && <em>（反向走无向链路）</em>}
                    {h.ruleId
                      ? <>，命中规则 <code>{h.ruleId}</code>（顺序 {h.ruleOrder}）</>
                      : "，无规则命中 → 取默认动作"}
                    {h.flow && (
                      <div className="hop-flow">
                        匹配地址：{formatAddress(h.flow.source)} → {formatAddress(h.flow.destination)}
                        {h.flow.protocol ? `，${h.flow.protocol}` : ""}
                      </div>
                    )}
                    {h.nat && (
                      <div className={`nat-decision nat-${h.nat.decision}`}>
                        转换：{natText(h.nat.action)} {h.nat.ruleId ? `规则 ${h.nat.ruleId}，` : ""}
                        {h.nat.decision} — {h.nat.detail ?? formatAddress(h.nat.before) + " → " + formatAddress(h.nat.after)}
                      </div>
                    )}
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
              <div className="path-nodes">{(p.nodeIds ?? []).join(" → ")}</div>
              {p.detail && <div className="nat-decision nat-denied">{p.detail}</div>}
              <ol className="hops">
                {(p.hops ?? []).map((h, j) => (
                  <li key={j} className={h.action === "deny" ? "deny-hop" : ""}>
                    <span className={h.action}>{h.action === "allow" ? "放行" : "拒绝"}</span>
                    {` ${h.from} → ${h.to}`} 经 <code>{h.linkId}</code>
                    {h.ruleId
                      ? <>，规则 <code>{h.ruleId}</code>（顺序 {h.ruleOrder}）</>
                      : "，默认动作"}
                    {h.nat && <div className={`nat-decision nat-${h.nat.decision}`}>{h.nat.detail}</div>}
                  </li>
                ))}
              </ol>
            </div>
          ))}
        </div>
      )}
      {paths.length === 0 && blocked.length === 0 && (
        <div className="hint">该阶段无候选路径（{reasonText(r.reason)}）</div>
      )}
    </div>
  );
}

function formatAddress(a?: { ip?: string; port?: number }): string {
  if (!a?.ip) return "—";
  return `${a.ip}:${a.port ?? 0}`;
}

function natText(action: string): string {
  if (action === "snat") return "源地址转换";
  if (action === "un-snat") return "回程反向还原";
  return action;
}

export function reasonText(reason: string): string {
  switch (reason) {
    case "allowed": return "存在策略全放行的路径";
    case "all-paths-denied": return "有拓扑路径，但每一条都被规则拦截";
    case "no-route": return "按链路方向无可达路由";
    case "same-node": return "源宿相同";
    default: return reason || "未知";
  }
}
