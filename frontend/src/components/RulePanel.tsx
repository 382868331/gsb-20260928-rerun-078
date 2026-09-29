import { useStore } from "../store";
import type { Rule } from "../types";

function csv(v: string[]): string {
  return v.join(", ");
}

function parseCsv(s: string): string[] {
  return s.split(",").map((x) => x.trim()).filter(Boolean);
}

function RuleEditor({ rule }: { rule: Rule }) {
  const { topo, updateRule, removeRule, moveRule } = useStore();
  const toggleNode = (list: "nodeIds" | "srcNodeIds" | "dstNodeIds", id: string) => {
    const cur = rule[list];
    updateRule(rule.id, {
      [list]: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id],
    });
  };

  return (
    <div className="rule-editor">
      <div className="row">
        <input value={rule.name} onChange={(e) => updateRule(rule.id, { name: e.target.value })} />
        <button onClick={() => moveRule(rule.id, -1)} title="上移（规则按顺序匹配）">↑</button>
        <button onClick={() => moveRule(rule.id, 1)} title="下移">↓</button>
        <button className="danger" onClick={() => removeRule(rule.id)}>删</button>
      </div>
      <div className="row">
        <select value={rule.action} onChange={(e) => updateRule(rule.id, { action: e.target.value as Rule["action"] })}>
          <option value="permit">permit 放行</option>
          <option value="deny">deny 阻断</option>
        </select>
        <label className="inline"><input type="checkbox" checked={rule.disabled}
          onChange={(e) => updateRule(rule.id, { disabled: e.target.checked })} /> 停用</label>
      </div>
      <label>协议（逗号分隔，留空=任意）
        <input value={csv(rule.protocols)} onChange={(e) => updateRule(rule.id, { protocols: parseCsv(e.target.value) })} placeholder="tcp, udp" />
      </label>
      <label>源区域 / 目的区域
        <div className="row">
          <input value={csv(rule.srcZones)} onChange={(e) => updateRule(rule.id, { srcZones: parseCsv(e.target.value) })} placeholder="src zones" />
          <input value={csv(rule.dstZones)} onChange={(e) => updateRule(rule.id, { dstZones: parseCsv(e.target.value) })} placeholder="dst zones" />
        </div>
      </label>
      <fieldset>
        <legend>规则安装节点（留空=所有节点）</legend>
        <div className="chips">
          {topo.nodes.map((n) => (
            <label key={n.id} className="inline chip">
              <input type="checkbox" checked={rule.nodeIds.includes(n.id)} onChange={() => toggleNode("nodeIds", n.id)} />
              {n.name}
            </label>
          ))}
        </div>
      </fieldset>
      <fieldset>
        <legend>指定源/目的节点（留空=按区域或任意）</legend>
        <div className="chips">
          {topo.nodes.map((n) => (
            <span key={n.id} className="chip">
              <label className="inline">
                <input type="checkbox" checked={rule.srcNodeIds.includes(n.id)} onChange={() => toggleNode("srcNodeIds", n.id)} />
                源 {n.name}
              </label>
              <label className="inline">
                <input type="checkbox" checked={rule.dstNodeIds.includes(n.id)} onChange={() => toggleNode("dstNodeIds", n.id)} />
                目 {n.name}
              </label>
            </span>
          ))}
        </div>
      </fieldset>
    </div>
  );
}

export function RulePanel() {
  const { topo, selectedRule, selectRule, addRule } = useStore();
  return (
    <section className="panel">
      <h2>策略规则（自上而下，首条命中生效）</h2>
      <button onClick={() => addRule()}>+ 添加规则</button>
      <ol className="rule-list">
        {topo.rules.map((r, i) => (
          <li key={r.id}>
            <button
              className={`rule-head ${selectedRule === r.id ? "active" : ""} ${r.action}`}
              onClick={() => selectRule(selectedRule === r.id ? undefined : r.id)}
            >
              <span className="order">{i + 1}</span>
              <span>{r.name || r.id}</span>
              <span className={`badge ${r.action}`}>{r.disabled ? "停用" : r.action}</span>
            </button>
            {selectedRule === r.id && <RuleEditor rule={r} />}
          </li>
        ))}
        {topo.rules.length === 0 && <li className="muted">暂无规则；无规则命中即默认拒绝（default deny）。</li>}
      </ol>
    </section>
  );
}
