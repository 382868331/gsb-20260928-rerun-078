package topo

import (
	"strings"
	"testing"
)

// natChainTopo builds h1 -- nat1 -- nat2 -- h2 with a permit-all
// policy so the translation layer alone decides the verdicts.
func natChainTopo() *Topology {
	return &Topology{
		Nodes: []Node{
			{ID: "h1", Kind: "host", Attrs: map[string]string{"ip": "10.0.0.1"}},
			{ID: "nat1", Kind: "firewall", NAT: &NATConfig{Enabled: true, Address: "198.51.100.1", PortStart: 6000, PortEnd: 6001}},
			{ID: "nat2", Kind: "firewall", NAT: &NATConfig{Enabled: true, Address: "203.0.113.1", PortStart: 7000, PortEnd: 7001}},
			{ID: "h2", Kind: "host", Attrs: map[string]string{"ip": "192.0.2.1"}},
		},
		Links: []Link{
			{ID: "l1", Source: "h1", Target: "nat1", Kind: LinkUndirected},
			{ID: "l2", Source: "nat1", Target: "nat2", Kind: LinkUndirected},
			{ID: "l3", Source: "nat2", Target: "h2", Kind: LinkUndirected},
		},
		Policy: Policy{DefaultAction: ActionAllow},
	}
}

func natEvents(res *ConnResult, direction, decision string) []NATHopEvent {
	var out []NATHopEvent
	for _, ev := range res.NATEvents {
		if ev.Direction == direction && ev.Decision == decision {
			out = append(out, ev)
		}
	}
	return out
}

// Two consecutive translations: each device rewrites the source, the
// return path confirms both mappings in reverse order.
func TestNATTwoConsecutiveTranslations(t *testing.T) {
	topo := natChainTopo()
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{SrcPort: 5000, DstPort: 80})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Bidirectional || res.Asymmetric {
		t.Fatalf("want bidirectional, got %+v (reason=%s)", res, res.Reason)
	}
	if len(res.NewMappings) != 2 {
		t.Fatalf("want 2 new mappings, got %+v", res.NewMappings)
	}
	if res.NewMappings[0].Device != "nat1" || res.NewMappings[0].NATPort != 6000 {
		t.Fatalf("nat1 mapping wrong: %+v", res.NewMappings[0])
	}
	// nat2 sees the already-translated source of nat1.
	if res.NewMappings[1].Device != "nat2" || res.NewMappings[1].InsideAddr != "198.51.100.1" {
		t.Fatalf("nat2 must translate nat1's output address: %+v", res.NewMappings[1])
	}
	if got := len(natEvents(res, "return", "matched")); got != 2 {
		t.Fatalf("want 2 matched return events, got %d: %+v", got, res.NATEvents)
	}
}

// Two hosts contending for one mapping pool: the existing connection
// keeps its original translation, the new connection picks the next
// free port, deterministically.
func TestNATContentionKeepsExistingMapping(t *testing.T) {
	topo := natChainTopo()
	existing := []Connection{{
		ID: "c-old", Source: "h1", Target: "h2",
		Mappings: []NATMapping{{Device: "nat1", InsideAddr: "10.0.0.9", InsidePort: 5000, NATAddr: "198.51.100.1", NATPort: 6000, PeerAddr: "192.0.2.1", PeerPort: 80}},
	}}
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{SrcPort: 5000, DstPort: 80, Existing: existing})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Bidirectional {
		t.Fatalf("want bidirectional, reason=%s", res.Reason)
	}
	if len(res.KeptMappings) != 1 || res.KeptMappings[0].NATPort != 6000 || res.KeptMappings[0].ConnID != "c-old" {
		t.Fatalf("existing mapping must be kept untouched: %+v", res.KeptMappings)
	}
	if res.NewMappings[0].NATPort != 6001 {
		t.Fatalf("new connection must take the next free port 6001, got %+v", res.NewMappings[0])
	}
}

// Pool exhausted: the new connection is refused even though every
// policy rule allows it.
func TestNATPoolExhaustedBlocksNewConnection(t *testing.T) {
	topo := natChainTopo()
	topo.Nodes[1].NAT.PortEnd = 6000 // single port
	existing := []Connection{{
		ID: "c-old", Source: "h1", Target: "h2",
		Mappings: []NATMapping{{Device: "nat1", InsideAddr: "10.0.0.9", InsidePort: 5000, NATAddr: "198.51.100.1", NATPort: 6000, PeerAddr: "192.0.2.1", PeerPort: 80}},
	}}
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{SrcPort: 5000, DstPort: 80, Existing: existing})
	if err != nil {
		t.Fatal(err)
	}
	if res.Establishable || res.State != "blocked" || res.Reason != "nat-pool-exhausted" {
		t.Fatalf("want blocked/nat-pool-exhausted, got state=%s reason=%s", res.State, res.Reason)
	}
	if got := natEvents(res, "forward", "pool-exhausted"); len(got) != 1 {
		t.Fatalf("want one pool-exhausted event, got %+v", res.NATEvents)
	}
}

// Idle timeout: the late return packet finds only an expired mapping;
// it is explained and dropped, never confirmed.
func TestNATTimeoutLateReturnDropped(t *testing.T) {
	topo := natChainTopo()
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{SrcPort: 5000, DstPort: 80, Timeout: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "expired" {
		t.Fatalf("want expired, got %s", res.State)
	}
	evs := natEvents(res, "return", "expired")
	if len(evs) == 0 {
		t.Fatalf("want expired return events, got %+v", res.NATEvents)
	}
	if !strings.Contains(evs[0].Detail, "超时") {
		t.Fatalf("expired event must explain the timeout: %s", evs[0].Detail)
	}
	if res.Bidirectional {
		t.Fatal("expired connection must not be bidirectional")
	}
}

// Port reused by a newer connection: the old connection's late return
// is reported as unconfirmable and is never delivered to the new
// connection now owning the port.
func TestNATPortReusedLateReturnNotDelivered(t *testing.T) {
	topo := natChainTopo()
	topo.Nodes[1].NAT.PortEnd = 6000 // single port: reuse is forced
	existing := []Connection{{
		ID: "c-old", Source: "h1", Target: "h2",
		Mappings: []NATMapping{{Device: "nat1", InsideAddr: "10.0.0.9", InsidePort: 5000, NATAddr: "198.51.100.1", NATPort: 6000, PeerAddr: "192.0.2.1", PeerPort: 80}},
	}}
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{
		SrcPort: 5000, DstPort: 80,
		Existing: existing, ExpiredConns: []string{"c-old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The new connection reuses the freed port and passes.
	if !res.Bidirectional || res.NewMappings[0].NATPort != 6000 {
		t.Fatalf("new connection should reuse port 6000, got %+v bidir=%v", res.NewMappings, res.Bidirectional)
	}
	evs := natEvents(res, "late-return", "port-reused")
	if len(evs) != 1 || evs[0].ConnID != "c-old" {
		t.Fatalf("want one port-reused late-return for c-old, got %+v", res.NATEvents)
	}
	if !strings.Contains(evs[0].Detail, "不放行") {
		t.Fatalf("must state the packet is not delivered to the new connection: %s", evs[0].Detail)
	}
}

// Translation range change: the old mapping falls outside the new
// pool, so the late return is dropped as unconfirmable.
func TestNATRangeChangeInvalidatesOldMapping(t *testing.T) {
	topo := natChainTopo()
	existing := []Connection{{
		ID: "c-old", Source: "h1", Target: "h2",
		Mappings: []NATMapping{{Device: "nat1", InsideAddr: "10.0.0.9", InsidePort: 5000, NATAddr: "198.51.100.1", NATPort: 6000, PeerAddr: "192.0.2.1", PeerPort: 80}},
	}}
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{
		SrcPort: 5000, DstPort: 80,
		Existing:  existing,
		NATRanges: map[string][2]int{"nat1": {7000, 7001}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.KeptMappings) != 0 {
		t.Fatalf("range change must not keep the old mapping: %+v", res.KeptMappings)
	}
	if res.NewMappings[0].NATPort != 7000 {
		t.Fatalf("new connection must allocate inside the new range, got %+v", res.NewMappings[0])
	}
	if evs := natEvents(res, "late-return", "range-changed"); len(evs) != 1 {
		t.Fatalf("want one range-changed late-return, got %+v", res.NATEvents)
	}
}

// Asymmetric return through a different device: the forward path was
// allowed and translated, but the return crosses a NAT device that
// never saw the connection. Forward allow never validates the return.
func TestNATAsymmetricReturnDifferentDevice(t *testing.T) {
	topo := &Topology{
		Nodes: []Node{
			{ID: "h1", Kind: "host", Attrs: map[string]string{"ip": "10.0.0.1"}},
			{ID: "nat1", Kind: "firewall", NAT: &NATConfig{Enabled: true, Address: "198.51.100.1", PortStart: 6000, PortEnd: 6001}},
			{ID: "nat3", Kind: "firewall", NAT: &NATConfig{Enabled: true, Address: "203.0.113.9", PortStart: 8000, PortEnd: 8001}},
			{ID: "h2", Kind: "host", Attrs: map[string]string{"ip": "192.0.2.1"}},
		},
		Links: []Link{
			{ID: "f1", Source: "h1", Target: "nat1", Kind: LinkDirected},
			{ID: "f2", Source: "nat1", Target: "h2", Kind: LinkDirected},
			{ID: "r1", Source: "h2", Target: "nat3", Kind: LinkDirected},
			{ID: "r2", Source: "nat3", Target: "h1", Kind: LinkDirected},
		},
		Policy: Policy{DefaultAction: ActionAllow},
	}
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{SrcPort: 5000, DstPort: 80})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Asymmetric || res.Bidirectional {
		t.Fatalf("want asymmetric, got %+v", res)
	}
	evs := natEvents(res, "return", "no-mapping")
	if len(evs) != 1 || evs[0].Device != "nat3" {
		t.Fatalf("want no-mapping at nat3, got %+v", res.NATEvents)
	}
	if !strings.Contains(evs[0].Detail, "不能凭去程已放行") {
		t.Fatalf("must state forward allow does not validate the return: %s", evs[0].Detail)
	}
}

// conndiff: a range change or a removed NAT config breaks the existing
// connection's original translation even when the path still passes.
func TestConnDiffNATMappingImpact(t *testing.T) {
	before := natChainTopo()
	conn := Connection{
		ID: "c1", Source: "h1", Target: "h2",
		PathNodeIDs: []string{"h1", "nat1", "nat2", "h2"},
		Mappings: []NATMapping{
			{Device: "nat1", InsideAddr: "10.0.0.1", InsidePort: 5000, NATAddr: "198.51.100.1", NATPort: 6000, PeerAddr: "192.0.2.1", PeerPort: 80},
			{Device: "nat2", InsideAddr: "198.51.100.1", InsidePort: 6000, NATAddr: "203.0.113.1", NATPort: 7000, PeerAddr: "192.0.2.1", PeerPort: 80},
		},
	}

	// Unchanged topology: the original translation is kept.
	same := natChainTopo()
	rep, err := CompareConn(before, same, []Connection{conn}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Existing[0].AfterStatus != "survives" || rep.Existing[0].MappingStatus != "kept" {
		t.Fatalf("want survives/kept, got %+v", rep.Existing[0])
	}

	// Range change on nat1 invalidates the original mapping.
	ranged := natChainTopo()
	ranged.Nodes[1].NAT.PortStart, ranged.Nodes[1].NAT.PortEnd = 9000, 9010
	rep, err = CompareConn(before, ranged, []Connection{conn}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Existing[0].AfterStatus != "broken-nat" || rep.Existing[0].MappingStatus != "invalidated-range" {
		t.Fatalf("want broken-nat/invalidated-range, got %+v", rep.Existing[0])
	}

	// NAT config removed from nat2.
	stripped := natChainTopo()
	stripped.Nodes[2].NAT = nil
	rep, err = CompareConn(before, stripped, []Connection{conn}, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Existing[0].AfterStatus != "broken-nat" || rep.Existing[0].MappingStatus != "device-removed" {
		t.Fatalf("want broken-nat/device-removed, got %+v", rep.Existing[0])
	}
}

// NAT trial results must serialize as [] so the frontend never maps
// over null.
func TestNATResultArraysNeverNil(t *testing.T) {
	topo := natChainTopo()
	res, err := topo.ConnCheck("h1", "h2", 3, ConnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.NATEvents == nil || res.NewMappings == nil || res.KeptMappings == nil {
		t.Fatal("NAT result slices must be initialized")
	}
}
