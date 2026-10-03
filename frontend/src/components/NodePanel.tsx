import { useTopoStore } from "../state/topoStore";

const KINDS = ["host", "switch", "router", "firewall"];

// nodeAddr previews the address this node presents on the wire.
function nodeAddr(n: { id: string; attrs?: Record<string, string> }): string {
  return n.attrs?.ip || n.id;
}

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
      <label>地址（ip 属性，连接检查使用）
        <input
          value={node.attrs?.ip ?? ""}
          placeholder={node.id}
          onChange={(e) => updateNode(node.id, { attrs: { ...(node.attrs ?? {}), ip: e.target.value } })}
        />
      </label>
      <fieldset className="nat-conf">
        <legend>地址转换（NAT）</legend>
        <label className="inline">
          <input
            type="checkbox"
            checked={node.nat?.enabled ?? false}
            onChange={(e) =>
              updateNode(node.id, {
                nat: {
                  enabled: e.target.checked,
                  address: node.nat?.address ?? "",
                  portStart: node.nat?.portStart ?? 6000,
                  portEnd: node.nat?.portEnd ?? 6099,
                },
              })
            }
          />
          该节点作为地址转换设备
        </label>
        {node.nat?.enabled && (
          <>
            <label>转换后地址
              <input
                value={node.nat.address}
                onChange={(e) => updateNode(node.id, { nat: { ...node.nat!, address: e.target.value } })}
              />
            </label>
            <label>端口池起
              <input
                type="number"
                value={node.nat.portStart}
                onChange={(e) => updateNode(node.id, { nat: { ...node.nat!, portStart: Number(e.target.value) } })}
              />
            </label>
            <label>端口池止
              <input
                type="number"
                value={node.nat.portEnd}
                onChange={(e) => updateNode(node.id, { nat: { ...node.nat!, portEnd: Number(e.target.value) } })}
              />
            </label>
            <p className="hint">
              经过该设备的连接把源地址 {nodeAddr(node)} 转换为 {node.nat.address || "（未填）"}，
              端口从 {node.nat.portStart}-{node.nat.portEnd} 中按最低可用分配；回程必须命中同一映射。
            </p>
          </>
        )}
      </fieldset>
      <div className="coord">x: {node.x} · y: {node.y}</div>
      <button className="danger" onClick={() => removeNode(node.id)}>删除节点（同时删除关联链路）</button>
    </div>
  );
}
