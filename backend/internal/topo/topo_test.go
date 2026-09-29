package topo

import "testing"

// h1 - s1 - s2 - h2, plus an alternate route s1 - s3 - s2.
func diamondTopo() *Topology {
	return &Topology{
		Nodes: []Node{
			{ID: "h1", Name: "h1", Kind: "host"},
			{ID: "h2", Name: "h2", Kind: "host"},
			{ID: "s1", Name: "s1", Kind: "switch"},
			{ID: "s2", Name: "s2", Kind: "switch"},
			{ID: "s3", Name: "s3", Kind: "switch"},
		},
		Links: []Link{
			{ID: "l1", Source: "h1", Target: "s1", Kind: LinkUndirected},
			{ID: "l2", Source: "s1", Target: "s2", Kind: LinkUndirected},
			{ID: "l3", Source: "s2", Target: "h2", Kind: LinkUndirected},
			{ID: "l4", Source: "s1", Target: "s3", Kind: LinkUndirected},
			{ID: "l5", Source: "s3", Target: "s2", Kind: LinkUndirected},
		},
		Policy: Policy{DefaultAction: ActionDeny},
	}
}

func TestDefaultDenyCableIsNotReachability(t *testing.T) {
	top := diamondTopo()
	// Wires exist between every hop, but with default-deny nothing passes.
	res, err := top.FindPaths("h1", "h2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reachable {
		t.Fatal("connected links must not imply reachability under default-deny")
	}
	if res.Reason != "all-paths-denied" {
		t.Fatalf("reason=%s want all-paths-denied", res.Reason)
	}
	if len(res.Blocked) == 0 || len(res.Blocked[0].Hops) == 0 {
		t.Fatal("expected blocked candidate paths with hop trace")
	}
	first := res.Blocked[0].Hops[0]
	if first.RuleID != "" || first.Action != ActionDeny {
		t.Fatalf("deny should come from default action, got rule %q action %q", first.RuleID, first.Action)
	}
}

func TestAllowAllYieldsMultipleDetours(t *testing.T) {
	top := diamondTopo()
	top.Policy.Rules = []Rule{
		{ID: "r-allow", Order: 1, Enabled: true, Action: ActionAllow},
	}
	res, err := top.FindPaths("h1", "h2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reachable || len(res.Paths) < 2 {
		t.Fatalf("want >=2 detour paths, got reachable=%v n=%d", res.Reachable, len(res.Paths))
	}
	// shortest first: h1-s1-s2-h2 (3 hops), then the s3 detour (4 hops).
	if len(res.Paths[0].NodeIDs) != 4 || len(res.Paths[1].NodeIDs) != 5 {
		t.Fatalf("unexpected path lengths: %v, %v", res.Paths[0].NodeIDs, res.Paths[1].NodeIDs)
	}
	for _, p := range res.Paths {
		for _, h := range p.Hops {
			if h.RuleID != "r-allow" || h.Action != ActionAllow {
				t.Fatalf("hop not explained by allow rule: %+v", h)
			}
		}
	}
}

func TestRuleOrderFirstMatchWins(t *testing.T) {
	top := diamondTopo()
	// broad allow at order 2, specific deny at order 1 on the direct s1->s2 link.
	top.Policy.Rules = []Rule{
		{ID: "deny-direct", Order: 1, Enabled: true, Action: ActionDeny, LinkID: "l2"},
		{ID: "allow-all", Order: 2, Enabled: true, Action: ActionAllow},
	}
	res, err := top.FindPaths("h1", "h2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reachable {
		t.Fatal("detour via s3 should remain reachable")
	}
	for _, p := range res.Paths {
		for _, h := range p.Hops {
			if h.LinkID == "l2" {
				t.Fatal("allowed path must not use denied link l2")
			}
		}
	}
	// A blocked direct candidate must record the first-match rule (order 1).
	if len(res.Blocked) == 0 {
		t.Fatal("expected the direct path among blocked candidates")
	}
	var sawDeny bool
	for _, b := range res.Blocked {
		for _, h := range b.Hops {
			if h.LinkID == "l2" && h.RuleID == "deny-direct" && h.Action == ActionDeny {
				sawDeny = true
			}
		}
	}
	if !sawDeny {
		t.Fatal("blocked hop should cite deny-direct with its order")
	}

	// Flipping rule order changes semantics: broad allow first means l2 passes.
	top.Policy.Rules[0].Order = 3
	top.Policy.Rules[1].Order = 1
	res2, err := top.FindPaths("h1", "h2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Reachable || res2.Paths[0].Length != 3 {
		t.Fatalf("with allow first, direct 3-hop path should win: %+v", res2)
	}
	if res2.Paths[0].Hops[1].RuleID != "allow-all" {
		t.Fatal("hop must be attributed to first matching rule")
	}
}

func TestDirectionMatters(t *testing.T) {
	top := &Topology{
		Nodes: []Node{{ID: "a"}, {ID: "b"}},
		Links: []Link{{ID: "x", Source: "a", Target: "b", Kind: LinkDirected}},
		Policy: Policy{
			DefaultAction: ActionDeny,
			Rules:         []Rule{{ID: "r", Order: 1, Enabled: true, Action: ActionAllow}},
		},
	}
	fwd, err := top.FindPaths("a", "b", 1)
	if err != nil || !fwd.Reachable {
		t.Fatalf("forward direction should pass: %v %v", fwd, err)
	}
	rev, err := top.FindPaths("b", "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Reachable || rev.Reason != "no-route" {
		t.Fatalf("reverse of directed link must not route: %+v", rev)
	}

	// Rule direction matters independently of link direction:
	// rule matches b->a hop only, so a->b falls back to default deny.
	top2 := diamondTopo()
	top2.Links[0].Kind = LinkUndirected
	top2.Policy.Rules = []Rule{{
		ID: "r", Order: 1, Enabled: true, Action: ActionAllow,
		SrcNode: "s1", DstNode: "h1",
	}}
	r2, err := top2.FindPaths("h1", "s1", 1)
	if err != nil || r2.Reachable {
		t.Fatalf("directional rule must not match reverse hop: %+v", r2)
	}
}

func TestPolicyDiffDetectsReachabilityChange(t *testing.T) {
	before := diamondTopo()
	before.Policy.DefaultAction = ActionDeny
	after := diamondTopo()
	after.Policy.Rules = []Rule{{ID: "allow", Order: 1, Enabled: true, Action: ActionAllow}}

	rep, err := ComparePolicy(before, after, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range rep.NewReachable {
		if p.Source == "h1" && p.Target == "h2" {
			found = true
		}
		if p.Source == "h2" && p.Target == "h1" && !p.After {
			t.Fatal("inconsistent pair state")
		}
	}
	if !found {
		t.Fatal("h1->h2 should be newly reachable")
	}
	if len(rep.NewBlocked) != 0 {
		t.Fatal("adding an allow should not block anything")
	}
}

func TestNormalizePreservesIdentities(t *testing.T) {
	top := &Topology{
		Nodes:  []Node{{ID: "uuid-1", Name: ""}, {ID: "uuid-2"}},
		Links:  []Link{{ID: "lk-9", Source: "uuid-1", Target: "uuid-2", Kind: LinkDirected}},
		Policy: Policy{Rules: []Rule{{ID: "rule-7", Action: ActionDeny}}},
	}
	if err := top.Normalize(); err != nil {
		t.Fatal(err)
	}
	if top.Nodes[0].ID != "uuid-1" || top.Links[0].ID != "lk-9" || top.Policy.Rules[0].ID != "rule-7" {
		t.Fatal("normalize must never reassign IDs")
	}
	if top.Nodes[0].Name != "uuid-1" {
		t.Fatal("empty name may default to ID but ID must stay")
	}
}

func TestDetourAfterLinkDenyKeepsTrace(t *testing.T) {
	top := diamondTopo()
	top.Policy.Rules = []Rule{
		{ID: "deny-l2", Order: 1, Enabled: true, Action: ActionDeny, LinkID: "l2"},
		{ID: "allow", Order: 2, Enabled: true, Action: ActionAllow},
	}
	res, _ := top.FindPaths("h1", "h2", 3)
	if !res.Reachable {
		t.Fatal("denying one link should fail over to the detour")
	}
	p0 := res.Paths[0]
	if len(p0.NodeIDs) != 5 || p0.NodeIDs[2] != "s3" {
		t.Fatalf("expected path through s3, got %v", p0.NodeIDs)
	}
	for _, h := range p0.Hops {
		if h.RuleID != "allow" {
			t.Fatalf("detour hops must name the rule actually used: %+v", h)
		}
	}
}
