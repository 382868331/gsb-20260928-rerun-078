package topo

import (
	"errors"
	"fmt"
	"sort"
)

// Validate checks structural integrity of a topology snapshot.
func (t *Topology) Validate() error {
	nodes := map[string]bool{}
	for _, n := range t.Nodes {
		if n.ID == "" {
			return errors.New("node with empty id")
		}
		if nodes[n.ID] {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		nodes[n.ID] = true
	}
	links := map[string]bool{}
	for _, l := range t.Links {
		if l.ID == "" {
			return errors.New("link with empty id")
		}
		if links[l.ID] {
			return fmt.Errorf("duplicate link id %q", l.ID)
		}
		links[l.ID] = true
		if !nodes[l.From] || !nodes[l.To] {
			return fmt.Errorf("link %q references unknown node", l.ID)
		}
	}
	rules := map[string]bool{}
	for _, r := range t.Rules {
		if r.ID == "" {
			return errors.New("rule with empty id")
		}
		if rules[r.ID] {
			return fmt.Errorf("duplicate rule id %q", r.ID)
		}
		rules[r.ID] = true
		if r.Action != ActionPermit && r.Action != ActionDeny {
			return fmt.Errorf("rule %q has invalid action %q", r.ID, r.Action)
		}
		for _, id := range r.NodeIDs {
			if !nodes[id] {
				return fmt.Errorf("rule %q installed on unknown node %q", r.ID, id)
			}
		}
		for _, id := range append(append([]string{}, r.SrcNodeIDs...), r.DstNodeIDs...) {
			if !nodes[id] {
				return fmt.Errorf("rule %q matches unknown node %q", r.ID, id)
			}
		}
	}
	return nil
}

func (t *Topology) node(id string) (Node, bool) {
	for _, n := range t.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// matchRule reports whether r applies to the connection when evaluated
// on the given node. Every populated selector must match (AND); an empty
// selector list is a wildcard.
func matchRule(r Rule, on Node, src, dst Node, conn Connection) bool {
	if r.Disabled {
		return false
	}
	if len(r.NodeIDs) > 0 && !contains(r.NodeIDs, on.ID) {
		return false
	}
	if len(r.SrcNodeIDs) > 0 && !contains(r.SrcNodeIDs, src.ID) {
		return false
	}
	if len(r.DstNodeIDs) > 0 && !contains(r.DstNodeIDs, dst.ID) {
		return false
	}
	if len(r.SrcZones) > 0 && !contains(r.SrcZones, src.Zone) {
		return false
	}
	if len(r.DstZones) > 0 && !contains(r.DstZones, dst.Zone) {
		return false
	}
	if len(r.Protocols) > 0 && !contains(r.Protocols, conn.Protocol) {
		return false
	}
	return true
}

// evaluateNode applies the ordered rule list on one node. First match wins;
// with no match the connection hits default-deny.
func (t *Topology) evaluateNode(nodeID string, src, dst Node, conn Connection) HopDecision {
	on, _ := t.node(nodeID)
	for _, r := range t.Rules {
		if matchRule(r, on, src, dst, conn) {
			return HopDecision{NodeID: nodeID, RuleID: r.ID, RuleName: r.Name, Action: r.Action}
		}
	}
	return HopDecision{NodeID: nodeID, RuleID: DefaultDenyRuleID, Action: ActionDeny}
}

// adjacency builds the directed adjacency list.
func (t *Topology) adjacency() map[string][]string {
	adj := map[string][]string{}
	for _, l := range t.Links {
		adj[l.From] = append(adj[l.From], l.To)
	}
	for k := range adj {
		sort.Strings(adj[k]) // deterministic candidate order
	}
	return adj
}

// shortestSimplePaths returns up to k loop-free routes from src to dst
// following directed links, shortest first (Yen-style enumeration). It
// returns raw graph routes; policy is evaluated separately afterwards.
func (t *Topology) shortestSimplePaths(src, dst string, k int) [][]string {
	if k <= 0 || src == dst {
		if src == dst {
			return [][]string{{src}}
		}
		return nil
	}
	adj := t.adjacency()
	bfs := func(removedNodes map[string]bool, removedEdges map[[2]string]bool, spur string, prefix []string) []string {
		if removedNodes[spur] {
			return nil
		}
		prev := map[string]string{spur: ""}
		visited := map[string]bool{spur: true}
		queue := []string{spur}
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			if u == dst {
				path := []string{}
				for cur := u; cur != ""; cur = prev[cur] {
					path = append([]string{cur}, path...)
				}
				if len(prefix) == 0 {
					return path
				}
				return append(append([]string{}, prefix...), path[1:]...)
			}
			for _, v := range adj[u] {
				if visited[v] || removedNodes[v] || removedEdges[[2]string{u, v}] {
					continue
				}
				visited[v] = true
				prev[v] = u
				queue = append(queue, v)
			}
		}
		return nil
	}

	first := bfs(nil, nil, src, nil)
	if first == nil {
		return nil
	}
	A := [][]string{first}
	B := map[string]bool{}
	for len(A) < k {
		prevPath := A[len(A)-1]
		var candidates [][]string
		for i := 0; i < len(prevPath)-1; i++ {
			spur := prevPath[i]
			root := append([]string{}, prevPath[:i+1]...)
			removedNodes := map[string]bool{}
			for _, p := range root[:len(root)-1] {
				removedNodes[p] = true
			}
			removedEdges := map[[2]string]bool{}
			for _, p := range A {
				if i+1 < len(p) && equalPrefix(p[:i+1], root) {
					removedEdges[[2]string{p[i], p[i+1]}] = true
				}
			}
			got := bfs(removedNodes, removedEdges, spur, root)
			if got != nil {
				key := joinPath(got)
				if !B[key] {
					B[key] = true
					candidates = append(candidates, got)
				}
			}
		}
		if len(candidates) == 0 {
			break
		}
		sort.Slice(candidates, func(i, j int) bool {
			if len(candidates[i]) != len(candidates[j]) {
				return len(candidates[i]) < len(candidates[j])
			}
			return joinPath(candidates[i]) < joinPath(candidates[j])
		})
		A = append(A, candidates[0])
	}
	return A
}

func equalPrefix(a, b []string) bool {
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

func joinPath(p []string) string {
	s := ""
	for i, n := range p {
		if i > 0 {
			s += ">"
		}
		s += n
	}
	return s
}

// evaluatePath walks a route node by node. A connection is evaluated on
// every node it passes through (source, intermediates, destination); the
// first node whose decision is deny blocks the path.
func (t *Topology) evaluatePath(route []string, src, dst Node, conn Connection) Path {
	p := Path{Nodes: append([]string{}, route...), BlockedAt: -1}
	for _, id := range route {
		d := t.evaluateNode(id, src, dst, conn)
		p.Decisions = append(p.Decisions, d)
		if d.Action == ActionDeny {
			p.BlockedAt = len(p.Decisions) - 1
			return p
		}
	}
	p.Feasible = true
	return p
}

// Evaluate answers a single reachability question, returning up to maxPaths
// candidate routes (feasible and blocked) with the exact rule trace of the
// active path. Default maxPaths is 5.
func (t *Topology) Evaluate(conn Connection, maxPaths int) (ReachResult, error) {
	if err := t.Validate(); err != nil {
		return ReachResult{}, err
	}
	src, ok1 := t.node(conn.Src)
	dst, ok2 := t.node(conn.Dst)
	if !ok1 {
		return ReachResult{}, fmt.Errorf("unknown source node %q", conn.Src)
	}
	if !ok2 {
		return ReachResult{}, fmt.Errorf("unknown destination node %q", conn.Dst)
	}
	if maxPaths <= 0 {
		maxPaths = 5
	}
	res := ReachResult{Conn: conn}
	if conn.Src == conn.Dst {
		p := t.evaluatePath([]string{conn.Src}, src, dst, conn)
		res.Paths = []Path{p}
		res.Reachable = p.Feasible
		if p.Feasible {
			res.ActivePath = &res.Paths[0]
			res.Reason = "same node; local policy permits"
		} else {
			res.Reason = denyReason(p)
		}
		return res, nil
	}
	routes := t.shortestSimplePaths(conn.Src, conn.Dst, maxPaths)
	if len(routes) == 0 {
		res.Reason = "no directed route exists between source and destination"
		return res, nil
	}
	for _, route := range routes {
		p := t.evaluatePath(route, src, dst, conn)
		res.Paths = append(res.Paths, p)
		if p.Feasible && res.ActivePath == nil {
			res.ActivePath = &res.Paths[len(res.Paths)-1]
		}
	}
	if res.ActivePath != nil {
		res.Reachable = true
		res.Reason = fmt.Sprintf("permitted via %s; rules checked on all %d nodes",
			joinPath(res.ActivePath.Nodes), len(res.ActivePath.Nodes))
	} else {
		res.Reason = fmt.Sprintf("%d route(s) exist but every one is blocked by policy", len(res.Paths))
	}
	return res, nil
}

func denyReason(p Path) string {
	if p.BlockedAt < 0 {
		return "permitted"
	}
	d := p.Decisions[p.BlockedAt]
	if d.RuleID == DefaultDenyRuleID {
		return fmt.Sprintf("blocked at %s: no rule matched (default deny)", d.NodeID)
	}
	return fmt.Sprintf("blocked at %s by rule %s (%s)", d.NodeID, d.RuleID, d.RuleName)
}

// EvaluateAll evaluates reachability for a set of connections.
func (t *Topology) EvaluateAll(conns []Connection, maxPaths int) ([]ReachResult, error) {
	out := make([]ReachResult, 0, len(conns))
	for _, c := range conns {
		r, err := t.Evaluate(c, maxPaths)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Diff reports connections that became newly reachable or got blocked when
// moving from before to after. Direction and rule order are evaluated
// independently in each snapshot; nothing is inferred from link changes.
func Diff(before, after *Topology, conns []Connection, maxPaths int) (DiffResult, error) {
	bRes, err := before.EvaluateAll(conns, maxPaths)
	if err != nil {
		return DiffResult{}, err
	}
	aRes, err := after.EvaluateAll(conns, maxPaths)
	if err != nil {
		return DiffResult{}, err
	}
	d := DiffResult{FromVersion: before.Version, ToVersion: after.Version}
	for i := range conns {
		switch {
		case !bRes[i].Reachable && aRes[i].Reachable:
			d.Added = append(d.Added, Change{Conn: conns[i], Before: false, After: true, Kind: "added",
				BeforeReason: bRes[i].Reason, AfterReason: aRes[i].Reason})
		case bRes[i].Reachable && !aRes[i].Reachable:
			d.Blocked = append(d.Blocked, Change{Conn: conns[i], Before: true, After: false, Kind: "blocked",
				BeforeReason: bRes[i].Reason, AfterReason: aRes[i].Reason})
		default:
			d.Unchanged++
		}
	}
	return d, nil
}
