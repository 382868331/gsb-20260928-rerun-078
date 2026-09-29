import type { Topology } from "../types";

export const sampleTopology: Topology = {
  version: 0,
  nodes: [
    { id: "h1", name: "主机A", kind: "host", x: 80, y: 200 },
    { id: "s1", name: "交换机1", kind: "switch", x: 260, y: 120 },
    { id: "s2", name: "交换机2", kind: "switch", x: 460, y: 200 },
    { id: "s3", name: "交换机3", kind: "switch", x: 260, y: 320 },
    { id: "h2", name: "主机B", kind: "host", x: 660, y: 200 },
  ],
  links: [
    { id: "l1", source: "h1", target: "s1", kind: "undirected" },
    { id: "l2", source: "s1", target: "s2", kind: "undirected" },
    { id: "l3", source: "s2", target: "h2", kind: "undirected" },
    { id: "l4", source: "s1", target: "s3", kind: "undirected" },
    { id: "l5", source: "s3", target: "s2", kind: "undirected" },
  ],
  policy: {
    defaultAction: "deny",
    rules: [
      {
        id: "r1", order: 10, enabled: true, action: "allow",
        srcKind: "host", dstKind: "switch", note: "主机可上联交换机",
      },
      {
        id: "r2", order: 20, enabled: true, action: "allow",
        srcKind: "switch", dstKind: "host", note: "交换机可下联主机",
      },
      {
        id: "r3", order: 5, enabled: false, action: "deny",
        linkId: "l2", note: "启用后阻断直连，迫使绕行 s3",
      },
      {
        id: "r4", order: 15, enabled: true, action: "allow",
        srcKind: "switch", dstKind: "switch", note: "交换机之间互通",
      },
    ],
  },
};
