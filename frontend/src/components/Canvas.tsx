import type { Path } from "../types";
import { useStore } from "../store";

interface Props {
  activePath?: Path;
  candidatePaths?: Path[];
}

const R = 26;

function nodePos(id: string, nodes: { id: string; x?: number; y?: number }[]) {
  const n = nodes.find((x) => x.id === id);
  return { x: n?.x ?? 0, y: n?.y ?? 0 };
}

export function Canvas({ activePath, candidatePaths }: Props) {
  const { topo, selectedNode, pendingLinkFrom, selectNode, setPendingLinkFrom, addLink, updateNode } =
    useStore();

  const activeLinks = new Set<string>();
  const blockedLinks = new Set<string>();
  const activeNodes = new Set<string>();
  const blockedNode = activePath && activePath.blockedAt >= 0 ? activePath.nodes[activePath.blockedAt] : undefined;
  if (activePath) {
    activePath.nodes.forEach((n) => activeNodes.add(n));
    for (let i = 0; i < activePath.nodes.length - 1; i++) {
      activeLinks.add(`${activePath.nodes[i]}>${activePath.nodes[i + 1]}`);
    }
  }
  for (const p of candidatePaths ?? []) {
    if (p.blockedAt >= 0) {
      blockedLinks.add(`${p.nodes[p.blockedAt - 1] ?? p.nodes[0]}>${p.nodes[p.blockedAt]}`);
    }
  }

  function onDragNode(e: React.PointerEvent<SVGGElement>, id: string) {
    e.preventDefault();
    const svg = (e.currentTarget.ownerSVGElement) as SVGSVGElement;
    const pt = svg.createSVGPoint();
    const move = (ev: PointerEvent) => {
      pt.x = ev.clientX;
      pt.y = ev.clientY;
      const p = pt.matrixTransform(svg.getScreenCTM()?.inverse());
      updateNode(id, { x: Math.max(40, p.x), y: Math.max(40, p.y) });
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

  return (
    <svg className="canvas" role="img" aria-label="拓扑画布">
      <defs>
        <marker id="arrow" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
          <path d="M0,0 L10,4 L0,8 z" fill="#8a94a6" />
        </marker>
        <marker id="arrow-ok" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
          <path d="M0,0 L10,4 L0,8 z" fill="#16a34a" />
        </marker>
        <marker id="arrow-deny" markerWidth="10" markerHeight="8" refX="9" refY="4" orient="auto">
          <path d="M0,0 L10,4 L0,8 z" fill="#dc2626" />
        </marker>
      </defs>

      {topo.links.map((l) => {
        const a = nodePos(l.from, topo.nodes);
        const b = nodePos(l.to, topo.nodes);
        const dx = b.x - a.x;
        const dy = b.y - a.y;
        const len = Math.hypot(dx, dy) || 1;
        const ux = dx / len;
        const uy = dy / len;
        const x1 = a.x + ux * R;
        const y1 = a.y + uy * R;
        const x2 = b.x - ux * R;
        const y2 = b.y - uy * R;
        const key = `${l.from}>${l.to}`;
        const onActive = activeLinks.has(key);
        const onBlocked = blockedLinks.has(key) && !onActive;
        return (
          <g key={l.id}>
            <line
              x1={x1} y1={y1} x2={x2} y2={y2}
              stroke={onActive ? "#16a34a" : onBlocked ? "#dc2626" : "#8a94a6"}
              strokeWidth={onActive || onBlocked ? 3 : 1.8}
              markerEnd={`url(#${onActive ? "arrow-ok" : onBlocked ? "arrow-deny" : "arrow"})`}
            />
          </g>
        );
      })}

      {topo.nodes.map((n) => {
        const selected = selectedNode === n.id;
        const pending = pendingLinkFrom === n.id;
        const onPath = activeNodes.has(n.id);
        const isBlocked = blockedNode === n.id;
        return (
          <g
            key={n.id}
            transform={`translate(${n.x ?? 0},${n.y ?? 0})`}
            className="node"
            style={{ cursor: "grab" }}
            onPointerDown={(e) => onDragNode(e, n.id)}
            onClick={(e) => {
              e.stopPropagation();
              if (pendingLinkFrom && pendingLinkFrom !== n.id) {
                addLink(pendingLinkFrom, n.id);
              } else {
                selectNode(n.id);
              }
            }}
          >
            <circle
              r={R}
              className={isBlocked ? "node-circle blocked" : onPath ? "node-circle path" : "node-circle"}
              stroke={selected ? "#2563eb" : pending ? "#d97706" : "#475569"}
              strokeWidth={selected || pending ? 3 : 1.5}
            />
            <text className="node-label" textAnchor="middle" dy="4">{n.name}</text>
            <text className="node-zone" textAnchor="middle" y={R + 14}>{n.zone} · {n.kind}</text>
          </g>
        );
      })}

      {topo.nodes.length === 0 && (
        <text x="40" y="40" className="hint">先添加节点；选中一个节点后点击另一节点可建立有向链路。</text>
      )}
      <rect
        x={0} y={0} width={2000} height={1400}
        fill="transparent"
        onClick={() => { selectNode(undefined); setPendingLinkFrom(undefined); }}
      />
    </svg>
  );
}
