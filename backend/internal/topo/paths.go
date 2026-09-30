package topo

import (
	"fmt"
	"sort"
)

// PathResult explains why a connection can or cannot pass:
// every path returned (blocked ones included) reports the exact rule
// that allowed or denied each hop, in traversal order.
type PathResult struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Reachable bool   `json:"reachable"`
	Paths     []Path `json:"paths"`   // up to K allowed paths, shortest first
	Blocked   []Path `json:"blocked"` // candidate paths rejected by policy
	Reason    string `json:"reason"`  // machine readable summary
}

// FindPaths performs an ordered-rule-aware Yen K-shortest-paths search.
// A mere cable between two nodes never implies reachability: every hop
// is evaluated against the policy in rule order, and only paths whose
// hops are all allowed count as reachable.
func (t *Topology) FindPaths(src, dst string, k int) (*PathResult, error) {
	res := &PathResult{
		Source: src, Target: dst,
		Paths: []Path{}, Blocked: []Path{}, // always emit [], never null
	}
	eng, err := BuildEngine(t)
	if err != nil {
		return nil, err
	}
	if _, ok := eng.nodes[src]; !ok {
		return nil, fmt.Errorf("unknown source node %q", src)
	}
	if _, ok := eng.nodes[dst]; !ok {
		return nil, fmt.Errorf("unknown target node %q", dst)
	}
	if src == dst {
		res.Reachable = true
		res.Reason = "same-node"
		res.Paths = []Path{{Hops: []Hop{}, NodeIDs: []string{src}, Reachable: true, Length: 0}}
		return res, nil
	}
	if k <= 0 {
		k = 3
	}

	candidates := yen(eng, src, dst, k*4)
	for _, cand := range candidates {
		p := t.evaluatePath(eng, cand)
		if p.Reachable {
			res.Paths = append(res.Paths, p)
			if len(res.Paths) >= k {
				break
			}
		} else if len(res.Blocked) < k {
			res.Blocked = append(res.Blocked, p)
		}
	}
	res.Reachable = len(res.Paths) > 0
	switch {
	case res.Reachable:
		res.Reason = "allowed"
	case len(res.Blocked) > 0:
		res.Reason = "all-paths-denied"
	default:
		res.Reason = "no-route"
	}
	return res, nil
}

func (t *Topology) evaluatePath(eng *engine, nodes []string) Path {
	p := Path{Hops: []Hop{}, NodeIDs: nodes, Reachable: len(nodes) >= 1}
	for i := 0; i+1 < len(nodes); i++ {
		linkID, reverse, ok := eng.edgeBetween(nodes[i], nodes[i+1])
		if !ok {
			p.Reachable = false
			break
		}
		d := t.DecideHop(nodes[i], nodes[i+1], linkID)
		action := t.Policy.DefaultAction
		if d.RuleID != "" {
			if d.Allowed {
				action = ActionAllow
			} else {
				action = ActionDeny
			}
		}
		hop := Hop{
			From: nodes[i], To: nodes[i+1], LinkID: linkID,
			Reverse: reverse, RuleID: d.RuleID, RuleOrder: d.RuleOrder,
			Action: action,
		}
		if !d.Allowed {
			p.Reachable = false
		}
		// Keep evaluating subsequent hops so blocked explanations cover
		// the full route; the first denied hop is still what made the
		// path unreachable, but downstream attribution stays aligned.
		p.Hops = append(p.Hops, hop)
	}
	p.Length = len(p.Hops)
	return p
}

// edgeBetween returns the link id used to walk u -> v and whether it
// is a reverse traversal of an undirected link.
func (e *engine) edgeBetween(u, v string) (string, bool, bool) {
	for _, edge := range e.out[u] {
		if edge[1] == v {
			return edge[0], edge[2] == "1", true
		}
	}
	return "", false, false
}

// yen is a compact Yen K-shortest simple-paths implementation over
// hop count. limit caps how many candidates we generate.
func yen(e *engine, src, dst string, limit int) [][]string {
	via, dist := dijkstra(e, src, nil, nil)
	if _, ok := dist[dst]; !ok {
		return nil
	}
	A := [][]string{reconstruct(via, src, dst)}
	var B [][]string
	contains := func(list [][]string, p []string) bool {
		for _, q := range list {
			if sameSeq(p, q) {
				return true
			}
		}
		return false
	}

	for len(A) < limit {
		prev := A[len(A)-1]
		progressed := false
		for i := 0; i < len(prev)-1; i++ {
			spurNode := prev[i]
			rootPath := append([]string{}, prev[:i+1]...)

			removedEdges := map[string]bool{}
			for _, p := range A {
				if i+1 < len(p) && sameSeq(p[:i+1], rootPath) {
					if linkID, _, ok := e.edgeBetween(p[i], p[i+1]); ok {
						removedEdges[linkID+":"+p[i]+":"+p[i+1]] = true
					}
				}
			}
			blockedNodes := map[string]bool{}
			for _, n := range rootPath[:len(rootPath)-1] {
				blockedNodes[n] = true
			}

			via2, dist2 := dijkstra(e, spurNode, blockedNodes, removedEdges)
			if _, ok := dist2[dst]; ok {
				spur := reconstruct(via2, spurNode, dst)
				candidate := append(append([]string{}, rootPath...), spur[1:]...)
				if !contains(A, candidate) && !contains(B, candidate) {
					B = append(B, candidate)
					progressed = true
				}
			}
		}
		if len(B) == 0 {
			break
		}
		best := 0
		for j := 1; j < len(B); j++ {
			if lessPath(B[j], B[best]) {
				best = j
			}
		}
		A = append(A, B[best])
		B = append(B[:best], B[best+1:]...)
		_ = progressed
	}
	return A
}

// lessPath orders K-path candidates deterministically: fewer hops first,
// then lexicographic node ids. This makes explanations for one saved
// version identical on every run, regardless of Go map iteration.
func lessPath(a, b []string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func sameSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func reconstruct(via map[string]string, src, dst string) []string {
	seq := []string{dst}
	for cur := dst; cur != src; {
		prev, ok := via[cur]
		if !ok {
			return nil
		}
		cur = prev
		seq = append(seq, cur)
	}
	for i, j := 0, len(seq)-1; i < j; i, j = i+1, j-1 {
		seq[i], seq[j] = seq[j], seq[i]
	}
	return seq
}

// dijkstra with unit hop weights. Keys in blockedEdges have the form
// linkID:u:v so parallel links and direction are distinguished.
func dijkstra(e *engine, src string, blockedNodes, blockedEdges map[string]bool) (map[string]string, map[string]int) {
	const inf = 1 << 30
	dist := map[string]int{}
	via := map[string]string{}
	for id := range e.nodes {
		dist[id] = inf
	}
	if blockedNodes[src] {
		return via, map[string]int{}
	}
	dist[src] = 0
	visited := map[string]bool{}
	// Fixed iteration order removes map-randomness from equal-cost ties.
	ordered := make([]string, 0, len(e.nodes))
	for id := range e.nodes {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for {
		u := ""
		best := inf
		for _, id := range ordered {
			d := dist[id]
			if !visited[id] && d < best {
				u, best = id, d
			}
		}
		if u == "" || best == inf {
			break
		}
		visited[u] = true
		for _, edge := range e.out[u] {
			linkID, v := edge[0], edge[1]
			if blockedNodes[v] || blockedEdges[linkID+":"+u+":"+v] {
				continue
			}
			if best+1 < dist[v] {
				dist[v] = best + 1
				via[v] = u
			}
		}
	}
	out := map[string]int{}
	for id, d := range dist {
		if d < inf {
			out[id] = d
		}
	}
	return via, out
}
