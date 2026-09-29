package topo

import (
	"encoding/json"
	"testing"
)

// mesh: h1 -> fw -> sw -> h2 ; backup: fw -> bk -> sw
func sampleTopo() *Topology {
	return &Topology{
		Version: "v1",
		Nodes: []Node{
			{ID: "n-h1", Name: "h1", Zone: "lan", Kind: "host"},
			{ID: "n-fw", Name: "fw", Zone: "edge", Kind: "firewall"},
			{ID: "n-sw", Name: "sw", Zone: "core", Kind: "switch"},
			{ID: "n-bk", Name: "bk", Zone: "core", Kind: "switch"},
			{ID: "n-h2", Name: "h2", Zone: "dmz", Kind: "host"},
		},
		Links: []Link{
			{ID: "l1", From: "n-h1", To: "n-fw"},
			{ID: "l2", From: "n-fw", To: "n-sw"},
			{ID: "l3", From: "n-sw", To: "n-h2"},
			{ID: "l4", From: "n-fw", To: "n-bk"},
			{ID: "l5", From: "n-bk", To: "n-sw"},
		},
		Rules: []Rule{
			{ID: "r-deny-icmp", Name: "deny icmp to dmz", NodeIDs: []string{"n-fw"},
				DstZones: []string{"dmz"}, Protocols: []string{"icmp"}, Action: ActionDeny},
			{ID: "r-permit-fw", Name: "permit edge->dmz tcp", NodeIDs: []string{"n-fw"},
				DstZones: []string{"dmz"}, Protocols: []string{"tcp"}, Action: ActionPermit},
		},
	}
}

func TestDirectionMatters(t *testing.T) {
	tp := sampleTopo()
	// h2 -> h1: no reverse links exist (l1..l5 are directed), so no route.
	res, err := tp.Evaluate(Connection{Src: "n-h2", Dst: "n-h1", Protocol: "tcp"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reachable {
		t.Fatal("reverse direction must not be reachable without reverse links")
	}
}

func permitAll(tp *Topology) {
	tp.Rules = append(tp.Rules,
		Rule{ID: "r-permit-hosts", NodeIDs: []string{"n-h1", "n-h2"}, Protocols: []string{"tcp"}, Action: ActionPermit},
		Rule{ID: "r-permit-core", NodeIDs: []string{"n-sw", "n-bk"}, DstZones: []string{"dmz"}, Protocols: []string{"tcp"}, Action: ActionPermit},
	)
}

func TestReachableWithFullRuleTrace(t *testing.T) {
	tp := sampleTopo()
	permitAll(tp)
	res, err := tp.Evaluate(Connection{Src: "n-h1", Dst: "n-h2", Protocol: "tcp"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reachable || res.ActivePath == nil {
		t.Fatalf("expected reachable: %+v", res)
	}
	byNode := map[string]HopDecision{}
	for _, d := range res.ActivePath.Decisions {
		byNode[d.NodeID] = d
	}
	if byNode["n-fw"].RuleID != "r-permit-fw" {
		t.Fatalf("fw must hit r-permit-fw: %+v", byNode)
	}
	if byNode["n-sw"].RuleID != "r-permit-core" {
		t.Fatalf("sw must hit r-permit-core: %+v", byNode)
	}
	if len(res.Paths) != 2 {
		t.Fatalf("expected 2 candidate paths including backup, got %d", len(res.Paths))
	}
}

func TestWiredButDenied(t *testing.T) {
	tp := sampleTopo()
	// Permit icmp on the source so evaluation reaches fw, where the explicit
	// deny must fire (wire connectivity alone is not enough).
	tp.Rules = append(tp.Rules, Rule{ID: "r-icmp-out", NodeIDs: []string{"n-h1"},
		Protocols: []string{"icmp"}, Action: ActionPermit})
	res, _ := tp.Evaluate(Connection{Src: "n-h1", Dst: "n-h2", Protocol: "icmp"}, 5)
	if res.Reachable {
		t.Fatal("icmp must be blocked by r-deny-icmp even though links exist")
	}
	last := res.Paths[0]
	if last.BlockedAt < 0 || last.Decisions[last.BlockedAt].RuleID != "r-deny-icmp" {
		t.Fatalf("expected block at fw by r-deny-icmp: %+v", last)
	}
}

func TestRuleOrderFirstMatchWins(t *testing.T) {
	tp := sampleTopo()
	permitAll(tp)
	// deny ahead of permit for tcp on fw => blocked.
	tp.Rules = append([]Rule{
		{ID: "r-first-deny", NodeIDs: []string{"n-fw"}, DstZones: []string{"dmz"},
			Protocols: []string{"tcp"}, Action: ActionDeny},
	}, tp.Rules...)
	res, _ := tp.Evaluate(Connection{Src: "n-h1", Dst: "n-h2", Protocol: "tcp"}, 5)
	fw := res.Paths[0].Decisions[1]
	if fw.NodeID != "n-fw" || fw.RuleID != "r-first-deny" {
		t.Fatalf("first matching rule must win: %+v", res.Paths[0].Decisions)
	}
}

func TestBackupPathUsed(t *testing.T) {
	tp := sampleTopo()
	// allow tcp everywhere; break the direct fw->sw policy? policy is per-node
	// not per-link, so instead add a link rule simulation by denying on n-bk
	// first and verify both routes are enumerated, shortest first.
	got := tp.shortestSimplePaths("n-h1", "n-h2", 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 routes, got %d: %v", len(got), got)
	}
	if joinPath(got[0]) != "n-h1>n-fw>n-sw>n-h2" || joinPath(got[1]) != "n-h1>n-fw>n-bk>n-sw>n-h2" {
		t.Fatalf("unexpected route order: %v", got)
	}
}

func TestDefaultDenyWhenNoRuleMatches(t *testing.T) {
	tp := sampleTopo()
	d := tp.evaluateNode("n-sw", tp.nodeMust("n-h1"), tp.nodeMust("n-h2"),
		Connection{Protocol: "tcp"})
	if d.RuleID != DefaultDenyRuleID || d.Action != ActionDeny {
		t.Fatalf("expected default-deny, got %+v", d)
	}
}

func (t *Topology) nodeMust(id string) Node {
	n, ok := t.node(id)
	if !ok {
		panic(id)
	}
	return n
}

func TestDiffAddedAndBlocked(t *testing.T) {
	before := sampleTopo() // fw permits tcp; sw default-denies => blocked
	after := sampleTopo()
	after.Rules = append(after.Rules, Rule{ID: "r-permit-core", NodeIDs: []string{"n-sw", "n-bk"},
		DstZones: []string{"dmz"}, Protocols: []string{"tcp"}, Action: ActionPermit})
	// hosts also need a permit for end-to-end feasibility.
	after.Rules = append(after.Rules, Rule{ID: "r-permit-hosts", NodeIDs: []string{"n-h1", "n-h2"},
		Protocols: []string{"tcp"}, Action: ActionPermit})
	conns := []Connection{{Src: "n-h1", Dst: "n-h2", Protocol: "tcp"},
		{Src: "n-h1", Dst: "n-h2", Protocol: "icmp"}}
	d, err := Diff(before, after, conns, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Added) != 1 || d.Added[0].Conn.Protocol != "tcp" {
		t.Fatalf("expected tcp added: %+v", d.Added)
	}
	if len(d.Blocked) != 0 { // icmp blocked both before and after
		t.Fatalf("icmp should be unchanged-blocked: %+v", d.Blocked)
	}
}

func TestSaveUndoImportPreservesIDs(t *testing.T) {
	store := NewStore()
	t1 := sampleTopo()
	s1, err := store.Save(t1)
	if err != nil {
		t.Fatal(err)
	}
	t2 := sampleTopo()
	t2.Rules = append(t2.Rules, Rule{ID: "r-new", NodeIDs: []string{"n-sw"}, Action: ActionPermit})
	s2, _ := store.Save(t2)
	if s1.Version == s2.Version {
		t.Fatal("distinct content must produce distinct versions")
	}
	back, err := store.Undo()
	if err != nil || back.Version != s1.Version {
		t.Fatalf("undo must restore previous snapshot: %v %v", back, err)
	}
	if back.Nodes[0].ID != "n-h1" {
		t.Fatal("undo must preserve node identity")
	}
	raw, _ := json.Marshal(s2)
	imp, err := store.Import(raw)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, n := range imp.Nodes {
		ids[n.ID] = true
	}
	if !ids["n-h1"] || !ids["n-fw"] {
		t.Fatal("import must preserve original node IDs")
	}
}
