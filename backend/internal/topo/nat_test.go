package topo

import (
	"strings"
	"testing"
)

func natTopo() *Topology {
	return &Topology{
		Nodes: []Node{
			{ID: "h1", Kind: "host"}, {ID: "n1", Kind: "nat"},
			{ID: "s1", Kind: "switch"}, {ID: "h2", Kind: "host"},
		},
		Links: []Link{
			{ID: "l1", Source: "h1", Target: "n1", Kind: LinkUndirected},
			{ID: "l2", Source: "n1", Target: "s1", Kind: LinkUndirected},
			{ID: "l3", Source: "s1", Target: "h2", Kind: LinkUndirected},
		},
		NATRules: []NATRule{{
			ID: "nat1", Device: "n1", EgressLinkID: "l2", Order: 1, Enabled: true,
			MatchSourceIP: "10.0.0.0/24", TranslatedIP: "198.51.100.1", PortStart: 2000, PortEnd: 2009,
		}},
		Policy: Policy{DefaultAction: ActionDeny, Rules: []Rule{
			{ID: "new", Order: 1, Enabled: true, Action: ActionAllow, ConnState: StateNew},
			{ID: "est", Order: 2, Enabled: true, Action: ActionAllow, ConnState: StateEstablished},
			{ID: "ret", Order: 3, Enabled: true, Action: ActionAllow, ConnState: StateReturn},
		}},
	}
}

func natPacket(connID string, srcPort int) Packet {
	return Packet{
		ConnectionID: connID,
		Protocol:     "tcp",
		Source:       Address{IP: "10.0.0.10", Port: srcPort},
		Destination:  Address{IP: "203.0.113.20", Port: 80},
	}
}

func TestNATCreatesTrackedMappingAndConfirmsReturn(t *testing.T) {
	topo := natTopo()
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{Packet: packetPtr(natPacket("c1", 40001))})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Establishable || !res.Bidirectional {
		t.Fatalf("forward and translated return must pass: %+v", res)
	}
	if len(res.Mappings) != 1 {
		t.Fatalf("want one tracked mapping, got %+v", res.Mappings)
	}
	m := res.Mappings[0]
	if m.Translated != (Address{IP: "198.51.100.1", Port: 2000}) || m.ConnectionID != "c1" {
		t.Fatalf("unexpected mapping: %+v", m)
	}
	fwdHop := res.Forward.Paths[0].Hops[1]
	if fwdHop.Flow.Source != m.Translated || fwdHop.NAT == nil || fwdHop.NAT.Decision != "created" {
		t.Fatalf("forward policy must see post-NAT address and decision: %+v", fwdHop)
	}
	retHops := res.Return.Paths[0].Hops
	last := retHops[len(retHops)-1]
	if last.NAT == nil || last.NAT.Decision != "confirmed" || last.Flow.Destination != m.Original {
		t.Fatalf("return must un-SNAT by connection binding: %+v", last)
	}
}

func TestNATTwoConsecutiveTranslationsTrackedIndependently(t *testing.T) {
	topo := natTopo()
	topo.Nodes = append(topo.Nodes, Node{ID: "n2", Kind: "nat"})
	topo.Links = []Link{
		{ID: "l1", Source: "h1", Target: "n1", Kind: LinkUndirected},
		{ID: "l2", Source: "n1", Target: "n2", Kind: LinkUndirected},
		{ID: "l3", Source: "n2", Target: "s1", Kind: LinkUndirected},
		{ID: "l4", Source: "s1", Target: "h2", Kind: LinkUndirected},
	}
	topo.NATRules[0].EgressLinkID = "l2"
	topo.NATRules = append(topo.NATRules, NATRule{
		ID: "nat2", Device: "n2", EgressLinkID: "l3", Order: 2, Enabled: true,
		MatchSourceIP: "198.51.100.0/24", TranslatedIP: "203.0.113.1", PortStart: 4000, PortEnd: 4009,
	})
	res, err := topo.ConnCheck("h1", "h2", 1, ConnOptions{Packet: packetPtr(natPacket("c1", 40001))})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Bidirectional || len(res.Mappings) != 2 {
		t.Fatalf("want two independently tracked mappings and valid return: %+v", res)
	}
	if res.Mappings[0].Translated.IP != "198.51.100.1" || res.Mappings[1].Translated.IP != "203.0.113.1" {
		t.Fatalf("consecutive mappings changed addresses in order: %+v", res.Mappings)
	}
	lastForward := res.Forward.Paths[0].Hops[2]
	if lastForward.Flow.Source.IP != "203.0.113.1" {
		t.Fatalf("second device rule must see twice-translated source: %+v", lastForward)
	}
	ret := res.Return.Paths[0]
	var n1Unsnatted, n2Confirmed bool
	for _, h := range ret.Hops {
		if h.NAT == nil {
			continue
		}
		if h.NAT.Device == "n2" && h.NAT.Decision == "confirmed" {
			n2Confirmed = true
		}
		if h.NAT.Device == "n1" && h.Flow.Destination.IP == "10.0.0.10" {
			n1Unsnatted = true
		}
	}
	if !n2Confirmed || !n1Unsnatted {
		t.Fatalf("return must unwind n2 then n1 using each binding: %+v", ret.Hops)
	}
}

func TestNATAsymmetricReturnThroughDifferentDeviceIsUnconfirmed(t *testing.T) {
	topo := natTopo()
	topo.Nodes = append(topo.Nodes, Node{ID: "n3", Kind: "nat"})
	topo.Links = []Link{
		{ID: "l1", Source: "h1", Target: "n1", Kind: LinkDirected},
		{ID: "l2", Source: "n1", Target: "h2", Kind: LinkDirected},
		{ID: "l3", Source: "h2", Target: "n3", Kind: LinkDirected},
		{ID: "l4", Source: "n3", Target: "h1", Kind: LinkDirected},
	}
	topo.NATRules[0].EgressLinkID = "l2"
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{Packet: packetPtr(natPacket("c1", 40001))})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Establishable || !res.Asymmetric || res.Bidirectional {
		t.Fatalf("forward must not validate an unrelated return device: %+v", res)
	}
	if !strings.Contains(res.Return.Blocked[0].Detail, "n1") {
		t.Fatalf("blocked return must name the original mapping device: %+v", res.Return.Blocked)
	}
}

func TestNATPortContentionDoesNotReuseAnotherConnection(t *testing.T) {
	topo := natTopo()
	active := NATBinding{
		ConnectionID: "old", Device: "n1", RuleID: "nat1", Protocol: "tcp",
		Original:   Address{IP: "10.0.0.11", Port: 40001},
		Translated: Address{IP: "198.51.100.1", Port: 2000},
		State:      bindingEstablished,
	}
	res, err := topo.FindPathsStatePacket("h1", "h2", 1, StateNew, natPacket("new", 40001), []NATBinding{active})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reachable || res.Paths[0].Mappings[0].Translated.Port != 2001 {
		t.Fatalf("new connection must allocate another port: %+v", res)
	}
}

func TestNATExpiredOrReusedPortLateReturnIsDenied(t *testing.T) {
	topo := natTopo()
	old := NATBinding{
		ConnectionID: "old", Device: "n1", RuleID: "nat1", Protocol: "tcp",
		Original:   Address{IP: "10.0.0.10", Port: 40001},
		Translated: Address{IP: "198.51.100.1", Port: 2000},
		State:      bindingExpired,
	}
	newBinding := NATBinding{
		ConnectionID: "new", Device: "n1", RuleID: "nat1", Protocol: "tcp",
		Original:   Address{IP: "10.0.0.20", Port: 50002},
		Translated: Address{IP: "198.51.100.1", Port: 2000},
		State:      bindingEstablished,
	}
	pkt := Packet{
		ConnectionID: "old", Protocol: "tcp",
		Source:      Address{IP: "203.0.113.20", Port: 80},
		Destination: Address{IP: "198.51.100.1", Port: 2000},
	}
	res, err := topo.FindPathsStatePacket("h2", "h1", 1, StateReturn, pkt, []NATBinding{old, newBinding})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reachable {
		t.Fatalf("late return for expired binding must not pass to port's new owner: %+v", res)
	}
	var denied *NATDecision
	for _, p := range res.Blocked {
		for i := range p.Hops {
			if h := p.Hops[i]; h.NAT != nil && h.NAT.Decision == "denied" {
				denied = h.NAT
			}
		}
	}
	if denied == nil || denied.Reason != "mapping-expired" || !strings.Contains(denied.Detail, "新连接") {
		t.Fatalf("must explain expired mapping and reused port risk: %+v", denied)
	}
}

func TestCompareConnExistingMappingIgnoresCurrentNATRange(t *testing.T) {
	before := natTopo()
	after := natTopo()
	after.NATRules[0].PortStart = 3000
	after.NATRules[0].PortEnd = 3009
	binding := NATBinding{
		ConnectionID: "c1", Device: "n1", RuleID: "nat1",
		Original:   Address{IP: "10.0.0.10", Port: 40001},
		Translated: Address{IP: "198.51.100.1", Port: 2000},
		State:      bindingEstablished,
	}
	packet := natPacket("c1", 40001)
	conns := []Connection{{
		ID: "c1", Source: "h1", Target: "h2", State: StateEstablished,
		PathNodeIDs: []string{"h1", "n1", "s1", "h2"},
		Mappings:    []NATBinding{binding}, Packet: &packet,
	}}
	rep, err := CompareConn(before, after, conns, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Existing[0].AfterStatus != "survives" {
		t.Fatalf("existing connection keeps its original mapping basis: %+v", rep.Existing[0])
	}
	newRes, err := after.FindPathsStatePacket("h1", "h2", 1, StateNew, packet, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !newRes.Reachable || newRes.Paths[0].Mappings[0].Translated.Port != 3000 {
		t.Fatalf("new connections must select from current range: %+v", newRes)
	}
}

func packetPtr(p Packet) *Packet { return &p }
