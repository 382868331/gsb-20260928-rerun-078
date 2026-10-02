package topo

import (
	"strings"
	"testing"
)

// connTopo: h1 - s1 - s2 - h2, all undirected, default deny.
func connTopo() *Topology {
	return &Topology{
		Nodes: []Node{
			{ID: "h1", Kind: "host"}, {ID: "s1", Kind: "switch"},
			{ID: "s2", Kind: "switch"}, {ID: "h2", Kind: "host"},
		},
		Links: []Link{
			{ID: "l1", Source: "h1", Target: "s1", Kind: LinkUndirected},
			{ID: "l2", Source: "s1", Target: "s2", Kind: LinkUndirected},
			{ID: "l3", Source: "s2", Target: "h2", Kind: LinkUndirected},
		},
		Policy: Policy{DefaultAction: ActionDeny},
	}
}

func allowAll(t *Topology, state string) {
	t.Policy.Rules = append(t.Policy.Rules, Rule{
		ID: "allow-" + state, Order: 1, Enabled: true, Action: ActionAllow, ConnState: state,
	})
}

func TestConnStateRulesSeparatePhases(t *testing.T) {
	topo := connTopo()
	// First request allowed, keep-alive of established denied.
	topo.Policy.Rules = []Rule{
		{ID: "r-new", Order: 1, Enabled: true, Action: ActionAllow, ConnState: StateNew},
		{ID: "r-est", Order: 2, Enabled: true, Action: ActionDeny, ConnState: StateEstablished},
		{ID: "r-ret", Order: 3, Enabled: true, Action: ActionAllow, ConnState: StateReturn},
	}
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Establishable {
		t.Fatalf("first request must establish: %+v", res.Forward)
	}
	if res.Maintain.Reachable {
		t.Fatalf("established keep-alive must be denied by r-est")
	}
	if !res.Return.Reachable {
		t.Fatalf("return traffic must pass under r-ret")
	}
	var maintain ConnPhase
	for _, ph := range res.Phases {
		if ph.Phase == "maintain" {
			maintain = ph
		}
	}
	if maintain.OK || maintain.Reason == "" {
		t.Fatalf("maintain phase must report failure with a reason: %+v", maintain)
	}
}

func TestConnCheckAsymmetricReturn(t *testing.T) {
	topo := connTopo()
	allowAll(topo, StateNew)
	allowAll(topo, StateEstablished)
	// return traffic denied on l2 only
	topo.Policy.Rules = append(topo.Policy.Rules,
		Rule{ID: "r-ret-deny", Order: 1, Enabled: true, Action: ActionDeny, ConnState: StateReturn, LinkID: "l2"},
		Rule{ID: "r-ret-allow", Order: 2, Enabled: true, Action: ActionAllow, ConnState: StateReturn},
	)
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Establishable || !res.Asymmetric || res.Bidirectional {
		t.Fatalf("want established+asymmetric, got %+v", res)
	}
	if res.State != "established" || res.Reason != "asymmetric-return" {
		t.Fatalf("one-way allow must not count as fully reachable: %+v", res)
	}
	var ret ConnPhase
	for _, ph := range res.Phases {
		if ph.Phase == "return" {
			ret = ph
		}
	}
	if ret.OK || !strings.Contains(ret.Detail, "非对称") {
		t.Fatalf("return phase must explain asymmetry: %+v", ret)
	}
}

func TestConnCheckTimeout(t *testing.T) {
	topo := connTopo()
	allowAll(topo, StateNew)
	allowAll(topo, StateEstablished)
	allowAll(topo, StateReturn)
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{Timeout: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "expired" {
		t.Fatalf("timeout must expire the connection: %+v", res)
	}
	var maintain, teardown ConnPhase
	for _, ph := range res.Phases {
		switch ph.Phase {
		case "maintain":
			maintain = ph
		case "teardown":
			teardown = ph
		}
	}
	if maintain.OK || maintain.Reason != "idle-timeout" {
		t.Fatalf("maintain must fail on timeout: %+v", maintain)
	}
	if teardown.OK || !strings.Contains(teardown.Detail, "超时") {
		t.Fatalf("teardown must cite the timeout: %+v", teardown)
	}
}

func TestConnCheckMiddleNodeDown(t *testing.T) {
	topo := connTopo()
	allowAll(topo, StateNew)
	allowAll(topo, StateEstablished)
	allowAll(topo, StateReturn)
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{DownNodes: []string{"s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "broken" {
		t.Fatalf("middle node down must break the connection: %+v", res)
	}
	var teardown ConnPhase
	for _, ph := range res.Phases {
		if ph.Phase == "teardown" {
			teardown = ph
		}
	}
	if teardown.OK || !strings.Contains(teardown.Detail, "s1") {
		t.Fatalf("teardown must name the failed node: %+v", teardown)
	}
	// The trial must not mutate the original topology.
	if len(topo.Nodes) != 4 || len(topo.Links) != 3 {
		t.Fatalf("trial mutated the topology: %+v", topo)
	}
}

func TestCompareConnSeparatesNewAndExisting(t *testing.T) {
	before := connTopo()
	allowAll(before, StateNew)
	allowAll(before, StateEstablished)
	allowAll(before, StateReturn)

	// Change 1: deny only new connections. Existing ones must survive.
	after := connTopo()
	allowAll(after, StateNew)
	allowAll(after, StateEstablished)
	allowAll(after, StateReturn)
	after.Policy.Rules = append([]Rule{
		{ID: "r-block-new", Order: 0, Enabled: true, Action: ActionDeny, ConnState: StateNew},
	}, after.Policy.Rules...)

	conns := []Connection{{ID: "c1", Source: "h1", Target: "h2", State: StateEstablished}}
	rep, err := CompareConn(before, after, conns, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.NewConns.NewBlocked) == 0 {
		t.Fatalf("new connections must be reported blocked")
	}
	if rep.Existing[0].AfterStatus != "survives" {
		t.Fatalf("existing connection must survive a new-only deny: %+v", rep.Existing[0])
	}

	// Change 2: deny established traffic -> existing connection breaks on policy.
	after2 := connTopo()
	allowAll(after2, StateNew)
	allowAll(after2, StateEstablished)
	after2.Policy.Rules = append(after2.Policy.Rules,
		Rule{ID: "r-est-deny", Order: 0, Enabled: true, Action: ActionDeny, ConnState: StateEstablished, LinkID: "l2"})
	rep2, err := CompareConn(before, after2, conns, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Existing[0].AfterStatus != "broken-policy" || !strings.Contains(rep2.Existing[0].Detail, "r-est-deny") {
		t.Fatalf("established deny must break the connection with attribution: %+v", rep2.Existing[0])
	}

	// Change 3: remove the middle node -> broken-path.
	after3 := connTopo()
	allowAll(after3, StateNew)
	allowAll(after3, StateEstablished)
	after3.Nodes = after3.Nodes[:1] // keep h1 only? no: drop s1 properly
	after3.Nodes = []Node{{ID: "h1", Kind: "host"}, {ID: "s2", Kind: "switch"}, {ID: "h2", Kind: "host"}}
	after3.Links = []Link{{ID: "l3", Source: "s2", Target: "h2", Kind: LinkUndirected}}
	rep3, err := CompareConn(before, after3, conns, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep3.Existing[0].AfterStatus != "broken-path" {
		t.Fatalf("removed middle node must break the path: %+v", rep3.Existing[0])
	}
}

func TestConnCheckNeverEmitsNullPhases(t *testing.T) {
	topo := connTopo() // default deny, no rules
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Establishable || res.State != "blocked" {
		t.Fatalf("default deny must block establishment: %+v", res)
	}
	if res.Phases == nil || len(res.Phases) != 4 {
		t.Fatalf("phases must always be a populated list: %+v", res.Phases)
	}
	if res.Forward.Paths == nil || res.Forward.Blocked == nil {
		t.Fatalf("phase path lists must never be nil")
	}
}
