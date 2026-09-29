import type { Topology } from "./types";

// A small demo: h1 -> fw -> sw -> h2 with a backup route fw -> bk -> sw.
// Firewall permits tcp toward dmz but denies icmp; other nodes default-deny
// until more rules are added, which demonstrates that a wire is not reachability.
export const sampleTopology: Topology = {
  nodes: [
    { id: "n-h1", name: "h1", zone: "lan", kind: "host", x: 80, y: 220 },
    { id: "n-fw", name: "fw", zone: "edge", kind: "firewall", x: 280, y: 220 },
    { id: "n-sw", name: "sw", zone: "core", kind: "switch", x: 520, y: 220 },
    { id: "n-bk", name: "bk", zone: "core", kind: "switch", x: 400, y: 80 },
    { id: "n-h2", name: "h2", zone: "dmz", kind: "host", x: 740, y: 220 },
  ],
  links: [
    { id: "l-1", from: "n-h1", to: "n-fw" },
    { id: "l-2", from: "n-fw", to: "n-sw" },
    { id: "l-3", from: "n-sw", to: "n-h2" },
    { id: "l-4", from: "n-fw", to: "n-bk" },
    { id: "l-5", from: "n-bk", to: "n-sw" },
  ],
  rules: [
    {
      id: "r-deny-icmp", name: "禁止 ping DMZ",
      nodeIds: ["n-fw"], dstZones: ["dmz"], protocols: ["icmp"],
      srcZones: [], srcNodeIds: [], dstNodeIds: [],
      action: "deny", disabled: false,
    },
    {
      id: "r-permit-tcp", name: "允许 TCP 到 DMZ",
      nodeIds: ["n-fw"], dstZones: ["dmz"], protocols: ["tcp"],
      srcZones: [], srcNodeIds: [], dstNodeIds: [],
      action: "permit", disabled: false,
    },
  ],
};
