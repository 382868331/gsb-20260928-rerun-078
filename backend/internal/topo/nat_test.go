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

func TestEstablishedConnectionCannotRerouteAroundOriginalNATDevice(t *testing.T) {
	topology := natTopo()
	topology.Links = append(topology.Links, Link{ID: "bypass", Source: "h1", Target: "s1", Kind: LinkUndirected})
	topology = topology.filteredCopy([]string{"n1"}, nil)
	binding := NATBinding{
		ConnectionID: "c1", Device: "n1", RuleID: "nat1", RuleOrder: 1, Protocol: "tcp",
		Original:    Address{IP: "10.0.0.10", Port: 40001},
		Translated:  Address{IP: "198.51.100.1", Port: 2000},
		Destination: Address{IP: "203.0.113.20", Port: 80},
		State:       bindingEstablished,
	}
	pkt := natPacket("c1", 40001)
	res, err := topology.FindPathsStatePacket("h1", "h2", 3, StateEstablished, pkt, []NATBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reachable {
		t.Fatalf("existing connection cannot take a bypass that loses its mapping: %+v", res.Paths)
	}
	if !strings.Contains(res.Blocked[0].Detail, "n1") {
		t.Fatalf("blocked explanation must name the original NAT device: %+v", res.Blocked)
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

func TestConnTimeoutExpiresOnlyCurrentConnectionAndKeepsOtherMappingOccupied(t *testing.T) {
	topology := natTopo()
	current := NATBinding{
		ConnectionID: "current", Device: "n1", RuleID: "nat1", RuleOrder: 1, Protocol: "tcp",
		Original:    Address{IP: "10.0.0.10", Port: 40001},
		Translated:  Address{IP: "198.51.100.1", Port: 2000},
		Destination: Address{IP: "203.0.113.20", Port: 80},
		State:       bindingEstablished,
	}
	other := NATBinding{
		ConnectionID: "other", Device: "n1", RuleID: "nat1", RuleOrder: 1, Protocol: "tcp",
		Original:    Address{IP: "10.0.0.20", Port: 40002},
		Translated:  Address{IP: "198.51.100.1", Port: 2001},
		Destination: Address{IP: "203.0.113.20", Port: 80},
		State:       bindingEstablished,
	}
	pkt := natPacket("current", 40001)
	res, err := topology.ConnCheck("h1", "h2", 1, ConnOptions{
		Packet:   &pkt,
		Bindings: []NATBinding{current, other},
		Timeout:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Maintain.Reachable || res.Return.Reachable || res.State != "expired" {
		t.Fatalf("timeout must invalidate the current connection only: %+v", res)
	}
	if len(res.Mappings) != 1 || res.Mappings[0].ConnectionID != "current" || res.Mappings[0].State != bindingEstablished {
		t.Fatalf("result should preserve the current established evidence, got %+v", res.Mappings)
	}
	newPkt := natPacket("replacement", 40003)
	newRes, err := topology.FindPathsStatePacket("h1", "h2", 1, StateNew, newPkt, []NATBinding{
		{ConnectionID: "current", Device: "n1", Translated: Address{IP: "198.51.100.1", Port: 2000}, State: bindingExpired},
		other,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !newRes.Reachable || newRes.Paths[0].Mappings[0].Translated.Port != 2000 {
		t.Fatalf("expired current port may be reallocated while other mapping stays occupied: %+v", newRes)
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

// twoNATSinglePortTopo chains two NAT gateways that each expose exactly
// one translation port, so any second connection must contend for the
// port already occupied by the registered h1 connection.
func twoNATSinglePortTopo() *Topology {
	topo := natTopo()
	topo.Nodes = append(topo.Nodes,
		Node{ID: "h3", Kind: "host"},
		Node{ID: "n2", Kind: "nat"},
	)
	topo.Links = []Link{
		{ID: "l0", Source: "h3", Target: "n1", Kind: LinkUndirected},
		{ID: "l1", Source: "h1", Target: "n1", Kind: LinkUndirected},
		{ID: "l2", Source: "n1", Target: "n2", Kind: LinkUndirected},
		{ID: "l3", Source: "n2", Target: "s1", Kind: LinkUndirected},
		{ID: "l4", Source: "s1", Target: "h2", Kind: LinkUndirected},
	}
	topo.NATRules[0].EgressLinkID = "l2"
	topo.NATRules[0].PortStart, topo.NATRules[0].PortEnd = 2000, 2000
	topo.NATRules = append(topo.NATRules, NATRule{
		ID: "nat2", Device: "n2", EgressLinkID: "l3", Order: 2, Enabled: true,
		MatchSourceIP: "198.51.100.0/24", TranslatedIP: "203.0.113.1", PortStart: 4000, PortEnd: 4000,
	})
	return topo
}

func h1RegisteredBindings() []NATBinding {
	return []NATBinding{
		{ConnectionID: "c1", Device: "n1", RuleID: "nat1", Protocol: "tcp",
			Original: Address{IP: "10.0.0.10", Port: 40001}, Translated: Address{IP: "198.51.100.1", Port: 2000},
			Destination: Address{IP: "203.0.113.20", Port: 80}, State: bindingEstablished},
		{ConnectionID: "c1", Device: "n2", RuleID: "nat2", Protocol: "tcp",
			Original: Address{IP: "198.51.100.1", Port: 2000}, Translated: Address{IP: "203.0.113.1", Port: 4000},
			Destination: Address{IP: "203.0.113.20", Port: 80}, State: bindingEstablished},
	}
}

func h3Packet(connID string) Packet {
	return Packet{ConnectionID: connID, Protocol: "tcp",
		Source:      Address{IP: "10.0.0.30", Port: 40001},
		Destination: Address{IP: "203.0.113.20", Port: 80},
	}
}

func TestNATSinglePortContentionNewConnectionDenied(t *testing.T) {
	topo := twoNATSinglePortTopo()
	pkt := h3Packet("c3")
	res, err := topo.ConnCheck("h3", "h2", 1, ConnOptions{Packet: &pkt, Bindings: h1RegisteredBindings()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Establishable || res.Bidirectional {
		t.Fatalf("single port occupied by h1: h3 must not establish: %+v", res)
	}
	var denied *NATDecision
	for _, p := range res.Forward.Blocked {
		for i := range p.Hops {
			if h := p.Hops[i]; h.NAT != nil && h.NAT.Decision == "denied" && h.NAT.Reason == "port-exhausted" {
				denied = h.NAT
			}
		}
	}
	if denied == nil || denied.Device != "n1" {
		t.Fatalf("denial must cite port exhaustion on the occupied gateway: %+v", denied)
	}
}

func TestNATForeignConnectionIDCannotReuseMapping(t *testing.T) {
	topo := twoNATSinglePortTopo()
	pkt := h3Packet("c1") // h3 claims the connection ID registered to h1
	res, err := topo.ConnCheck("h3", "h2", 1, ConnOptions{Packet: &pkt, Bindings: h1RegisteredBindings(), Existing: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Establishable || res.Bidirectional {
		t.Fatalf("h3 must not reuse h1's translated address/port: %+v", res)
	}
	var denied *NATDecision
	for _, p := range res.Forward.Blocked {
		for i := range p.Hops {
			if h := p.Hops[i]; h.NAT != nil && h.NAT.Decision == "denied" && h.NAT.Reason == "mapping-ownership" {
				denied = h.NAT
			}
		}
	}
	if denied == nil || !strings.Contains(denied.Detail, "其他主机") {
		t.Fatalf("denial must explain the mapping belongs to another host: %+v", denied)
	}
}

func TestNATRegisteredConnectionKeepsOwnMapping(t *testing.T) {
	topo := twoNATSinglePortTopo()
	pkt := natPacket("c1", 40001)
	res, err := topo.ConnCheck("h1", "h2", 1, ConnOptions{Packet: &pkt, Bindings: h1RegisteredBindings(), Existing: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Establishable || !res.Bidirectional {
		t.Fatalf("the registered owner must keep its own mapping on both gateways: %+v", res)
	}
}
