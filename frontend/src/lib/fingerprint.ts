import type { Topology } from "../types";

// Stable semantic fingerprint of a topology: the same editing state
// always produces the same string regardless of array ordering, so it
// can detect unsaved changes and match a cached check result to the
// exact topology it was computed from.
export function fingerprint(t: Topology): string {
  const byId = (a: unknown[], b: unknown[]) => String(a[0]).localeCompare(String(b[0]));
  const nodes = t.nodes
    .map((n) => [n.id, n.name, n.kind ?? "", n.x, n.y, n.attrs ?? {}] as unknown[])
    .sort(byId);
  const links = t.links
    .map((l) => [l.id, l.source, l.target, l.kind, l.attrs ?? {}] as unknown[])
    .sort(byId);
  const rules = t.policy.rules
    .map((r) => [
      r.id, r.order, r.enabled, r.action,
      r.srcNode ?? "", r.dstNode ?? "", r.srcKind ?? "", r.dstKind ?? "",
      r.linkId ?? "", r.linkAttrName ?? "", r.linkAttrValue ?? "",
      r.srcAttrName ?? "", r.srcAttrValue ?? "",
      r.dstAttrName ?? "", r.dstAttrValue ?? "",
      r.protocol ?? "", r.note ?? "",
    ] as unknown[])
    .sort(byId);
  return JSON.stringify({
    v: t.version || 0,
    defaultAction: t.policy.defaultAction,
    nodes,
    links,
    rules,
  });
}
