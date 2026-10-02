package topo

import (
	"strings"
	"testing"
)

// statefulTopo: h1 -l1- s1 -l2- h2, all undirected. Rules:
//   r-deny-h2: deny new connections whose first hop leaves h2
//   r-new:     allow all other new-connection traffic
//   r-est:     allow established traffic in both directions
// Default deny. So h1 can initiate a connection, h2 cannot, and once
// established, return traffic h2->h1 passes via the established rule.
func statefulTopo() *Topology {
	return &Topology{
		Nodes: []Node{
			{ID: "h1", Kind: "host"}, {ID: "s1", Kind: "switch"}, {ID: "h2", Kind: "host"},
		},
		Links: []Link{
			{ID: "l1", Source: "h1", Target: "s1", Kind: LinkUndirected},
			{ID: "l2", Source: "s1", Target: "h2", Kind: LinkUndirected},
		},
		Policy: Policy{
			DefaultAction: ActionDeny,
			Rules: []Rule{
				{ID: "r-deny-h2", Order: 5, Enabled: true, Action: ActionDeny, SrcNode: "h2", ConnState: ConnNew},
				{ID: "r-new", Order: 10, Enabled: true, Action: ActionAllow, ConnState: ConnNew},
				{ID: "r-est", Order: 20, Enabled: true, Action: ActionAllow, ConnState: ConnEstablished},
			},
		},
	}
}

func TestConnCheckPhasesAndStateRules(t *testing.T) {
	topo := statefulTopo()
	res, err := topo.ConnCheck("h1", "h2", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Phases) != 4 {
		t.Fatalf("want 4 phases, got %d", len(res.Phases))
	}
	if !res.Reachable || res.Reason != "two-way-established" {
		t.Fatalf("want two-way-established, got %+v", res)
	}
	// New-connection rules only allow h1 as source: h2 cannot initiate.
	back, err := topo.ConnCheck("h2", "h1", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if back.Reachable {
		t.Fatalf("h2 must not initiate a new connection under r-new: %+v", back)
	}
	if back.Phases[0].Reachable {
		t.Fatal("establish phase for h2->h1 must fail")
	}
	// But the return phase of h1->h2 passes via the established rule.
	ret := res.Phases[2]
	if ret.Name != PhaseReturn || !ret.Reachable {
		t.Fatalf("return phase must pass via established rule: %+v", ret)
	}
}

func TestConnCheckAsymmetricReturnNotCoveredByForwardAllow(t *testing.T) {
	// Forward path allowed for all states, but the only return link is
	// directed the wrong way: no return route exists at all.
	topo := &Topology{
		Nodes: []Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Links: []Link{
			{ID: "l1", Source: "a", Target: "b", Kind: LinkDirected},
			{ID: "l2", Source: "b", Target: "c", Kind: LinkDirected},
		},
		Policy: Policy{
			DefaultAction: ActionDeny,
			Rules: []Rule{
				{ID: "all", Order: 1, Enabled: true, Action: ActionAllow},
			},
		},
	}
	res, err := topo.ConnCheck("a", "c", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Phases[0].Reachable {
		t.Fatal("forward establish must pass")
	}
	if res.Reachable {
		t.Fatal("one-way permit must not count as full reachability")
	}
	if res.Reason != "return-blocked" {
		t.Fatalf("want return-blocked, got %s", res.Reason)
	}
}

func TestConnCheckTimeoutAndNodeDown(t *testing.T) {
	topo := statefulTopo()
	// Delete the established-allow rule: existing connections lose
	// their state rule; after a timeout everything is denied.
	topo.Policy.Rules = topo.Policy.Rules[:1]
	res, err := topo.ConnCheck("h1", "h2", 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Phases[1].Reachable || res.Phases[2].Reachable {
		t.Fatalf("after timeout with no established rule, maintain/return must fail: %+v", res.Phases)
	}
	td := res.Phases[3]
	if td.Name != PhaseTeardown || len(td.Notes) == 0 {
		t.Fatalf("teardown phase must carry hop-by-hop notes: %+v", td)
	}
	// Node-down detour explanation on a diamond.
	diamond := &Topology{
		Nodes: []Node{{ID: "a"}, {ID: "x"}, {ID: "y"}, {ID: "b"}},
		Links: []Link{
			{ID: "l1", Source: "a", Target: "x", Kind: LinkUndirected},
			{ID: "l2", Source: "x", Target: "b", Kind: LinkUndirected},
			{ID: "l3", Source: "a", Target: "y", Kind: LinkUndirected},
			{ID: "l4", Source: "y", Target: "b", Kind: LinkUndirected},
		},
		Policy: Policy{DefaultAction: ActionAllow, Rules: []Rule{}},
	}
	dres, err := diamond.ConnCheck("a", "b", 3, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range dres.Phases[3].Notes {
		if strings.Contains(n, "断开") && strings.Contains(n, "绕过") {
			found = true
		}
	}
	if !found {
		t.Fatalf("teardown must explain intermediate-node failure and detour: %+v", dres.Phases[3].Notes)
	}
}

func TestConnDiffSeparatesNewAndEstablished(t *testing.T) {
	before := statefulTopo()
	after := statefulTopo()
	// After: the broad new-connection allow is removed, established stays.
	after.Policy.Rules = append(after.Policy.Rules[:1], after.Policy.Rules[2:]...)
	rep, err := ConnDiff(before, after, true)
	if err != nil {
		t.Fatal(err)
	}
	var pair *ConnPairResult
	for i := range rep.TimeoutRisks {
		if rep.TimeoutRisks[i].Source == "h1" && rep.TimeoutRisks[i].Target == "h2" {
			pair = &rep.TimeoutRisks[i]
		}
	}
	if pair == nil {
		t.Fatalf("h1->h2 must appear as a timeout risk: %+v", rep)
	}
	if pair.NewChange != "now-blocked" || pair.EstChange != "none" {
		t.Fatalf("new connections blocked, existing unaffected: %+v", pair)
	}
	if pair.AfterDetail == nil {
		t.Fatal("detailed diff must attach the after connection check")
	}
}
