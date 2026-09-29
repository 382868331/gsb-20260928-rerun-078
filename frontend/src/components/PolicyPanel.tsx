import { useTopoStore } from "../state/topoStore";
import type { Rule, RuleAction } from "../types";

export function PolicyPanel() {
  const topo = useTopoStore((s) => s.topo);
  const selectedRule = useTopoStore((s) => s.selectedRule);
  const addRule = useTopoStore((s) => s.addRule);
  const updateRule = useTopoStore((s) => s.updateRule);
  const removeRule = useTopoStore((s) => s.removeRule);
  const moveRule = useTopoStore((s) => s.moveRule);
  const select = useTopoStore((s) => s.select);
  const setDefaultAction = useTopoStore((s) => s.setDefaultAction);

  const sorted = [...topo.policy.rules].sort((a, b) => a.order - b.order);
  const rule = topo.policy.rules.find((r) => r.id === selectedRule);
  const nodeOptions = topo.nodes;
  const linkOptions = topo.links;

  return (
    <div className="policy">
      <div className="default-row">
        <span>无规则命中时默认：</span>
        {(["deny", "allow"] as RuleAction[]).map((a) => (
          <label key={a} className="inline">
            <input
              type="radio"
              checked={topo.policy.defaultAction === a}
              onChange={() => setDefaultAction(a)}
            />
            {a === "deny" ? "拒绝" : "放行"}
          </label>
        ))}
      </div>

      <table className="rule-table">
        <thead>
          <tr><th>顺序</th><th>动作</th><th>匹配</th><th>启用</th><th></th></tr>
        </thead>
        <tbody>
          {sorted.map((r, i) => (
            <tr
              key={r.id}
              className={selectedRule === r.id ? "sel" : ""}
              onClick={() => select("rule", r.id)}
            >
              <td>{r.order}</td>
              <td className={r.action}>{r.action === "allow" ? "放行" : "拒绝"}</td>
              <td>{describeMatch(r) || "（全部流量）"}</td>
              <td>
                <input
                  type="checkbox"
                  checked={r.enabled}
                  onChange={(e) => updateRule(r.id, { enabled: e.target.checked })}
                />
              </td>
              <td>
                <button onClick={(e) => { e.stopPropagation(); moveRule(r.id, -1); }} disabled={i === 0}>↑</button>
                <button onClick={(e) => { e.stopPropagation(); moveRule(r.id, 1); }} disabled={i === sorted.length - 1}>↓</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <button onClick={addRule}>新增规则</button>

      {rule && (
        <div className="form rule-editor">
          <div className="editor-title">规则 {rule.id} — 顺序决定优先级，首个命中生效</div>
          <div className="row2">
            <label>动作
              <select value={rule.action} onChange={(e) => updateRule(rule.id, { action: e.target.value as RuleAction })}>
                <option value="allow">放行 allow</option>
                <option value="deny">拒绝 deny</option>
              </select>
            </label>
            <label>顺序号
              <input type="number" value={rule.order} onChange={(e) => updateRule(rule.id, { order: Number(e.target.value) })} />
            </label>
          </div>
          <div className="row2">
            <label>源节点
              <select value={rule.srcNode ?? ""} onChange={(e) => updateRule(rule.id, { srcNode: e.target.value || undefined })}>
                <option value="">（不限）</option>
                {nodeOptions.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
              </select>
            </label>
            <label>宿节点（方向敏感）
              <select value={rule.dstNode ?? ""} onChange={(e) => updateRule(rule.id, { dstNode: e.target.value || undefined })}>
                <option value="">（不限）</option>
                {nodeOptions.map((n) => <option key={n.id} value={n.id}>{n.name}</option>)}
              </select>
            </label>
          </div>
          <div className="row2">
            <label>源类型
              <input value={rule.srcKind ?? ""} placeholder="host/switch/..." onChange={(e) => updateRule(rule.id, { srcKind: e.target.value || undefined })} />
            </label>
            <label>宿类型
              <input value={rule.dstKind ?? ""} placeholder="host/switch/..." onChange={(e) => updateRule(rule.id, { dstKind: e.target.value || undefined })} />
            </label>
          </div>
          <label>指定链路（匹配该链路的跳）
            <select value={rule.linkId ?? ""} onChange={(e) => updateRule(rule.id, { linkId: e.target.value || undefined })}>
              <option value="">（不限）</option>
              {linkOptions.map((l) => <option key={l.id} value={l.id}>{l.id} ({l.source}→{l.target})</option>)}
            </select>
          </label>
          <label>链路协议（等于 link.attrs.protocol）
            <input value={rule.protocol ?? ""} placeholder="如 tcp / bgp" onChange={(e) => updateRule(rule.id, { protocol: e.target.value || undefined })} />
          </label>
          <label>备注
            <input value={rule.note ?? ""} onChange={(e) => updateRule(rule.id, { note: e.target.value })} />
          </label>
          <button className="danger" onClick={() => removeRule(rule.id)}>删除规则</button>
        </div>
      )}
    </div>
  );
}

function describeMatch(r: Rule): string {
  const parts: string[] = [];
  if (r.srcNode || r.dstNode) parts.push(`${r.srcNode ?? "*"} → ${r.dstNode ?? "*"}`);
  if (r.srcKind) parts.push(`src:${r.srcKind}`);
  if (r.dstKind) parts.push(`dst:${r.dstKind}`);
  if (r.linkId) parts.push(`link:${r.linkId}`);
  if (r.protocol) parts.push(`proto:${r.protocol}`);
  return parts.join("，");
}
