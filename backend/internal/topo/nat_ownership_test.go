package topo

import "testing"

func twoNATOnePort() *Topology {
	return &Topology{
		Nodes: []Node{
			{ID: "h1", Kind: "host"}, {ID: "h3", Kind: "host"},
			{ID: "n1", Kind: "nat"}, {ID: "n2", Kind: "nat"},
			{ID: "h2", Kind: "host"},
		},
		Links: []Link{
			{ID: "l0", Source: "h3", Target: "n1", Kind: LinkUndirected},
			{ID: "l1", Source: "h1", Target: "n1", Kind: LinkUndirected},
			{ID: "l2", Source: "n1", Target: "n2", Kind: LinkUndirected},
			{ID: "l3", Source: "n2", Target: "h2", Kind: LinkUndirected},
		},
		NATRules: []NATRule{
			{ID: "nat1", Device: "n1", EgressLinkID: "l2", Order: 1, Enabled: true,
				MatchSourceIP: "10.0.0.0/24", TranslatedIP: "198.51.100.1", PortStart: 2000, PortEnd: 2000},
			{ID: "nat2", Device: "n2", EgressLinkID: "l3", Order: 2, Enabled: true,
				MatchSourceIP: "198.51.100.0/24", TranslatedIP: "203.0.113.1", PortStart: 4000, PortEnd: 4000},
		},
		Policy: Policy{DefaultAction: ActionDeny, Rules: []Rule{
			{ID: "new", Order: 1, Enabled: true, Action: ActionAllow, ConnState: StateNew},
			{ID: "est", Order: 2, Enabled: true, Action: ActionAllow, ConnState: StateEstablished},
			{ID: "ret", Order: 3, Enabled: true, Action: ActionAllow, ConnState: StateReturn},
		}},
	}
}

func TestNATSinglePortContentionDenied(t *testing.T) {
	topo := twoNATOnePort()
	res1, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{Packet: &Packet{
		ConnectionID: "c1", Protocol: "tcp",
		Source:       Address{IP: "10.0.0.10", Port: 40001},
		Destination:  Address{IP: "203.0.113.20", Port: 80},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !res1.Bidirectional {
		t.Fatalf("h1 should establish: %+v", res1)
	}
	t.Logf("h1 mappings: %+v", res1.Mappings)

	res2, err := topo.ConnCheck("h3", "h2", 3, ConnOptions{
		Packet: &Packet{
			ConnectionID: "c3", Protocol: "tcp",
			Source:       Address{IP: "10.0.0.11", Port: 40002},
			Destination:  Address{IP: "203.0.113.20", Port: 80},
		},
		Bindings: res1.Mappings,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("h3: establishable=%v bidirectional=%v state=%s reason=%s", res2.Establishable, res2.Bidirectional, res2.State, res2.Reason)
	for _, ph := range res2.Phases {
		t.Logf("phase %s ok=%v reason=%s detail=%s", ph.Phase, ph.OK, ph.Reason, ph.Detail)
	}
	if res2.Bidirectional || res2.Establishable {
		t.Fatalf("BUG: h3 should be denied, got %+v", res2)
	}
}

func TestNATBindingOwnershipNotTransferable(t *testing.T) {
	topo := twoNATOnePort()
	res1, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{Packet: &Packet{
		ConnectionID: "c1", Protocol: "tcp",
		Source:       Address{IP: "10.0.0.10", Port: 40001},
		Destination:  Address{IP: "203.0.113.20", Port: 80},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// h3 submits an "existing" check carrying h1's connection ID and
	// h1's two mappings, but with h3's own source endpoint.
	res2, err := topo.ConnCheck("h3", "h2", 3, ConnOptions{
		Existing: true,
		Packet: &Packet{
			ConnectionID: "c1", Protocol: "tcp",
			Source:       Address{IP: "10.0.0.11", Port: 40002},
			Destination:  Address{IP: "203.0.113.20", Port: 80},
		},
		Bindings: res1.Mappings,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("usurp: establishable=%v bidirectional=%v state=%s reason=%s", res2.Establishable, res2.Bidirectional, res2.State, res2.Reason)
	if res2.Bidirectional || res2.Establishable {
		t.Fatalf("BUG: h3 must not reuse h1's mappings, got %+v", res2)
	}
}
