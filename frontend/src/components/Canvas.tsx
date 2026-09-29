import { useRef, useState } from "react";
import { useTopoStore } from "../state/topoStore";
import type { PathResult } from "../types";

interface Props {
  check: PathResult | null;
  linkMode: boolean;
  linkStart: string | null;
  setLinkStart: (id: string | null) => void;
}

const KIND_COLOR: Record<string, string> = {
  host: "#3b82f6",
  switch: "#10b981",
  router: "#f59e0b",
  firewall: "#ef4444",
};

export function Canvas({ check, linkMode, linkStart, setLinkStart }: Props) {
  const topo = useTopoStore((s) => s.topo);
  const selectedNode = useTopoStore((s) => s.selectedNode);
  const selectedLink = useTopoStore((s) => s.selectedLink);
  const select = useTopoStore((s) => s.select);
  const updateNode = useTopoStore((s) => s.updateNode);
  const addLink = useTopoStore((s) => s.addLink);
  const svgRef = useRef<SVGSVGElement>(null);
  const [drag, setDrag] = useState<{ id: string; dx: number; dy: number } | null>(null);

  const nodeById = (id: string) => topo.nodes.find((n) => n.id === id);

  // Highlight links on the first allowed path; red if the path is blocked.
  const litLinks = new Set<string>();
  const deniedLinks = new Set<string>();
  if (check) {
    const pool = check.paths.length > 0 ? check.paths : check.blocked;
    pool.slice(0, 1).forEach((p) =>
      p.hops.forEach((h) => {
        litLinks.add(h.linkId);
        if (h.action === "deny") deniedLinks.add(h.linkId);
      }),
    );
    check.blocked.forEach((p) =>
      p.hops.forEach((h) => {
        if (h.action === "deny") deniedLinks.add(h.linkId);
      }),
    );
  }

  function pointerToSvg(ev: React.PointerEvent) {
    const rect = svgRef.current!.getBoundingClientRect();
    return { x: ev.clientX - rect.left, y: ev.clientY - rect.top };
  }

  return (
    <svg
      ref={svgRef}
      className="canvas"
      onPointerMove={(ev) => {
        if (!drag) return;
        const p = pointerToSvg(ev);
        updateNode(drag.id, {
          x: Math.round(p.x - drag.dx),
          y: Math.round(p.y - drag.dy),
        });
      }}
      onPointerUp={() => setDrag(null)}
      onPointerLeave={() => setDrag(null)}
    >
      <defs>
        <marker id="arrow" markerWidth="10" markerHeight="10" refX="9" refY="3" orient="auto">
          <path d="M0,0 L0,6 L9,3 z" fill="#64748b" />
        </marker>
        <marker id="arrow-lit" markerWidth="10" markerHeight="10" refX="9" refY="3" orient="auto">
          <path d="M0,0 L0,6 L9,3 z" fill="#22c55e" />
        </marker>
      </defs>

      {topo.links.map((l) => {
        const a = nodeById(l.source);
        const b = nodeById(l.target);
        if (!a || !b) return null;
        const mx = (a.x + b.x) / 2;
        const my = (a.y + b.y) / 2;
        const lit = litLinks.has(l.id);
        const denied = deniedLinks.has(l.id);
        const stroke = denied ? "#ef4444" : lit ? "#22c55e" : "#94a3b8";
        const width = lit || denied ? 3.5 : 2;
        return (
          <g
            key={l.id}
            className={selectedLink === l.id ? "link selected" : "link"}
            onClick={(ev) => {
              ev.stopPropagation();
              select("link", l.id);
            }}
          >
            <line
              x1={a.x}
              y1={a.y}
              x2={b.x}
              y2={b.y}
              stroke={stroke}
              strokeWidth={width}
              markerEnd={l.kind === "directed" ? `url(#${lit ? "arrow-lit" : "arrow"})` : undefined}
            />
            {l.kind === "undirected" && (
              <line x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke="none" />
            )}
            <circle cx={mx} cy={my} r={11} fill="#fff" stroke={stroke} strokeWidth={1.5} />
            <text x={mx} y={my + 3.5} textAnchor="middle" className="link-label">
              {l.id.replace(/^[^_]+_/, "").slice(0, 4)}
            </text>
          </g>
        );
      })}

      {topo.nodes.map((n) => {
        const isStart = linkMode && linkStart === n.id;
        return (
          <g
            key={n.id}
            className={selectedNode === n.id ? "node selected" : "node"}
            transform={`translate(${n.x},${n.y})`}
            onPointerDown={(ev) => {
              if (linkMode) return;
              const p = pointerToSvg(ev);
              setDrag({ id: n.id, dx: p.x - n.x, dy: p.y - n.y });
            }}
            onClick={(ev) => {
              ev.stopPropagation();
              if (linkMode) {
                if (!linkStart) {
                  setLinkStart(n.id);
                } else if (linkStart !== n.id) {
                  addLink(linkStart, n.id);
                  setLinkStart(null);
                }
                return;
              }
              select("node", n.id);
            }}
          >
            <rect
              x={-38}
              y={-20}
              width={76}
              height={40}
              rx={8}
              fill={KIND_COLOR[n.kind ?? ""] ?? "#64748b"}
              stroke={isStart ? "#f59e0b" : "#0f172a"}
              strokeWidth={isStart ? 3 : 1}
            />
            <text y={2} textAnchor="middle" className="node-name">{n.name}</text>
            <text y={16} textAnchor="middle" className="node-id">{n.id}</text>
          </g>
        );
      })}
    </svg>
  );
}
