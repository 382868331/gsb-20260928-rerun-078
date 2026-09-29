import { useTopoStore } from "../state/topoStore";

export function LinkPanel() {
  const topo = useTopoStore((s) => s.topo);
  const selectedLink = useTopoStore((s) => s.selectedLink);
  const updateLink = useTopoStore((s) => s.updateLink);
  const removeLink = useTopoStore((s) => s.removeLink);
  const link = topo.links.find((l) => l.id === selectedLink);

  if (!link) {
    return <p className="hint">点击链路线条可切换方向属性并查看 ID。</p>;
  }
  const nameOf = (id: string) => topo.nodes.find((n) => n.id === id)?.name ?? id;
  return (
    <div className="form">
      <label>ID（身份，不可改）<input value={link.id} readOnly /></label>
      <div className="readonly">{nameOf(link.source)} → {nameOf(link.target)}</div>
      <label>方向
        <select
          value={link.kind}
          onChange={(e) =>
            updateLink(link.id, { kind: e.target.value as "directed" | "undirected" })
          }
        >
          <option value="directed">有向（仅 source → target）</option>
          <option value="undirected">无向（双向可走）</option>
        </select>
      </label>
      <label>协议（attrs.protocol，供规则匹配）
        <input
          value={link.attrs?.protocol ?? ""}
          onChange={(e) =>
            updateLink(link.id, {
              attrs: { ...(link.attrs ?? {}), protocol: e.target.value },
            })
          }
        />
      </label>
      <button className="danger" onClick={() => removeLink(link.id)}>删除链路</button>
    </div>
  );
}
