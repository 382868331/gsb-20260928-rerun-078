package topo

import "testing"

func TestCheckEmptyArraysNotNilAndReadableReason(t *testing.T) {
	top := diamondTopo() // default deny
	res, err := top.FindPaths("h1", "h2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Paths == nil || res.Blocked == nil {
		t.Fatal("paths/blocked must be non-nil slices so JSON emits [] not null")
	}
	if res.Reason == "" {
		t.Fatal("reason must always be populated")
	}

	// No physical route at all (directed link wrong way).
	dir := &Topology{
		Nodes:  []Node{{ID: "a"}, {ID: "b"}},
		Links:  []Link{{ID: "x", Source: "a", Target: "b", Kind: LinkDirected}},
		Policy: Policy{DefaultAction: ActionDeny},
	}
	res2, err := dir.FindPaths("b", "a", 3)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Reachable || res2.Reason != "no-route" {
		t.Fatalf("want no-route, got %+v", res2)
	}
	if res2.Paths == nil || res2.Blocked == nil {
		t.Fatal("no-route result still needs [] arrays")
	}
}

func TestExplainFlipAttributesOrderedRuleOnSegment(t *testing.T) {
	before := diamondTopo()
	before.Policy.Rules = []Rule{
		{ID: "deny-l2", Order: 1, Enabled: true, Action: ActionDeny, LinkID: "l2"},
		{ID: "deny-l5", Order: 1, Enabled: true, Action: ActionDeny, LinkID: "l5"},
		{ID: "allow-all", Order: 2, Enabled: true, Action: ActionAllow},
	}
	after := diamondTopo()
	after.Policy.Rules = []Rule{
		{ID: "allow-all", Order: 1, Enabled: true, Action: ActionAllow},
		{ID: "deny-l2", Order: 2, Enabled: true, Action: ActionDeny, LinkID: "l2"},
		{ID: "deny-l5", Order: 2, Enabled: true, Action: ActionDeny, LinkID: "l5"},
	}
	rep, err := ComparePolicy(before, after, false)
	if err != nil {
		t.Fatal(err)
	}
	var pair *PairResult
	for i := range rep.NewReachable {
		if rep.NewReachable[i].Source == "h1" && rep.NewReachable[i].Target == "h2" {
			pair = &rep.NewReachable[i]
		}
	}
	if pair == nil {
		t.Fatal("h1->h2 must become reachable after rule reorder")
	}
	var found bool
	for _, ch := range pair.HopChanges {
		if (ch.LinkID == "l2" || ch.LinkID == "l5") && ch.Cause == "rule-reordered" {
			if ch.BeforeRuleID != "deny-"+ch.LinkID || ch.AfterRuleID != "allow-all" {
				t.Fatalf("attribution wrong: %+v", ch)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("must point at l2 segment changing from deny-l2 to allow-all: %+v", pair.HopChanges)
	}
}

func TestExplainFlipDefaultDenyAndLinkOps(t *testing.T) {
	// default deny -> explicit allow: hop changes attributed to added rule
	before := diamondTopo()
	after := diamondTopo()
	after.Policy.Rules = []Rule{{ID: "a1", Order: 1, Enabled: true, Action: ActionAllow}}
	rep, err := ComparePolicy(before, after, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.NewBlocked) != 0 {
		t.Fatal("adding allow must not block")
	}
	if len(rep.NewReachable) == 0 {
		t.Fatal("pairs should become reachable")
	}

	// remove a link: previously reachable pair now no-route
	b2 := diamondTopo()
	b2.Policy = after.Policy
	a2 := diamondTopo()
	a2.Policy = after.Policy
	a2.Links = nil
	rep2, err := ComparePolicy(b2, a2, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep2.NewBlocked) == 0 {
		t.Fatal("deleting all links must block previously reachable pairs")
	}
	var sawLinkRemoved bool
outer:
	for _, p := range rep2.NewBlocked {
		for _, ch := range p.HopChanges {
			if ch.Cause == "link-removed" {
				sawLinkRemoved = true
				break outer
			}
		}
	}
	if !sawLinkRemoved {
		t.Fatal("expected a link-removed attribution")
	}
}

func TestPathExplanationIsDeterministic(t *testing.T) {
	top := diamondTopo()
	top.Policy.Rules = []Rule{{ID: "all", Order: 1, Enabled: true, Action: ActionAllow}}
	var first *PathResult
	for i := 0; i < 20; i++ {
		r, err := top.FindPaths("h1", "h2", 5)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = r
			continue
		}
		if !sameResult(first, r) {
			t.Fatalf("nondeterministic explanation:\n%+v\n%+v", first, r)
		}
	}
}

func sameResult(a, b *PathResult) bool {
	if a.Reachable != b.Reachable || a.Reason != b.Reason ||
		len(a.Paths) != len(b.Paths) || len(a.Blocked) != len(b.Blocked) {
		return false
	}
	for i := range a.Paths {
		pa, pb := a.Paths[i], b.Paths[i]
		if len(pa.NodeIDs) != len(pb.NodeIDs) {
			return false
		}
		for j := range pa.NodeIDs {
			if pa.NodeIDs[j] != pb.NodeIDs[j] {
				return false
			}
		}
		for j := range pa.Hops {
			if pa.Hops[j].RuleID != pb.Hops[j].RuleID || pa.Hops[j].LinkID != pb.Hops[j].LinkID {
				return false
			}
		}
	}
	return true
}
