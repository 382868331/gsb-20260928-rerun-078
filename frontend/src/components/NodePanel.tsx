import { useStore } from "../store";

export function NodePanel() {
  const { topo, selectedNode, updateNode, removeNode, addNode, addLink, setPendingLinkFrom, pendingLinkFrom, removeLink } =
    useStore();
  const node = topo.nodes.find((n) => n.id === selectedNode);

  return (
    <section className="panel">
      <h2>节点与链路</h2>
      <button onClick={() => addNode()}>+ 添加节点</button>

      {node && (
        <div className="editor">
          <label>名称<input value={node.name} onChange={(e) => updateNode(node.id, { name: e.target.value })} /></label>
          <label>区域<input value={node.zone} onChange={(e) => updateNode(node.id, { zone: e.target.value })} /></label>
          <label>类型
            <select value={node.kind} onChange={(e) => updateNode(node.id, { kind: e.target.value })}>
              {["host", "switch", "router", "firewall"].map((k) => <option key={k}>{k}</option>)}
            </select>
          </label>
          <div className="muted">身份 ID（保存 / 撤销 / 导入保持不变）：<code>{node.id}</code></div>
          <div className="row">
            <button
              className={pendingLinkFrom === node.id ? "active" : ""}
              onClick={() => setPendingLinkFrom(pendingLinkFrom === node.id ? undefined : node.id)}
            >
              {pendingLinkFrom === node.id ? "取消连线" : "以此为起点连线"}
            </button>
            <button className="danger" onClick={() => removeNode(node.id)}>删除节点</button>
          </div>
        </div>
      )}

      <h3>有向链路</h3>
      <ul className="link-list">
        {topo.links.map((l) => {
          const from = topo.nodes.find((n) => n.id === l.from)?.name ?? l.from;
          const to = topo.nodes.find((n) => n.id === l.to)?.name ?? l.to;
          return (
            <li key={l.id}>
              <span>{from} → {to}</span>
              <button className="link-del" onClick={() => removeLink(l.id)}>×</button>
            </li>
          );
        })}
        {topo.links.length === 0 && <li className="muted">暂无链路；链路是有向的，双向需要两条。</li>}
      </ul>
      {pendingLinkFrom && <div className="muted">已选择起点，点击目标节点即可创建链路。</div>}
    </section>
  );
}
