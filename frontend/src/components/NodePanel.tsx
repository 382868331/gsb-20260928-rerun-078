import { useTopoStore } from "../state/topoStore";

const KINDS = ["host", "switch", "router", "firewall"];

export function NodePanel() {
  const topo = useTopoStore((s) => s.topo);
  const selectedNode = useTopoStore((s) => s.selectedNode);
  const updateNode = useTopoStore((s) => s.updateNode);
  const removeNode = useTopoStore((s) => s.removeNode);
  const node = topo.nodes.find((n) => n.id === selectedNode);

  if (!node) {
    return (
      <p className="hint">在画布中点击节点进行编辑；拖拽可移动。身份字段 <code>id</code> 只读。</p>
    );
  }
  return (
    <div className="form">
      <label>ID（身份，不可改）<input value={node.id} readOnly /></label>
      <label>名称
        <input value={node.name} onChange={(e) => updateNode(node.id, { name: e.target.value })} />
      </label>
      <label>类型
        <select value={node.kind ?? "host"} onChange={(e) => updateNode(node.id, { kind: e.target.value })}>
          {KINDS.map((k) => <option key={k} value={k}>{k}</option>)}
        </select>
      </label>
      <div className="coord">x: {node.x} · y: {node.y}</div>
      <button className="danger" onClick={() => removeNode(node.id)}>删除节点（同时删除关联链路）</button>
    </div>
  );
}
