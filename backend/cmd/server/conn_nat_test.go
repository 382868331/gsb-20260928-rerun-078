package main

import "testing"

// twoGatewayTopo: h1/h3 are hosts behind two NAT gateways on parallel
// paths to h2; the rules translate into the same address pool and each
// offers exactly one translation port.
func twoGatewayTopoJSON() map[string]any {
	return map[string]any{
		"nodes": []map[string]any{
			{"id": "h1", "kind": "host"}, {"id": "h3", "kind": "host"},
			{"id": "g1", "kind": "nat"}, {"id": "g2", "kind": "nat"},
			{"id": "core", "kind": "switch"}, {"id": "h2", "kind": "host"},
		},
		"links": []map[string]any{
			{"id": "l1", "source": "h1", "target": "g1", "kind": "undirected"},
			{"id": "l2", "source": "h3", "target": "g2", "kind": "undirected"},
			{"id": "l3", "source": "g1", "target": "core", "kind": "undirected"},
			{"id": "l4", "source": "g2", "target": "core", "kind": "undirected"},
			{"id": "l5", "source": "core", "target": "h2", "kind": "undirected"},
		},
		"natRules": []map[string]any{
			{"id": "nat-g1", "device": "g1", "order": 1, "enabled": true,
				"matchSourceIp": "10.1.0.0/24", "translatedIp": "198.51.100.1",
				"portStart": 2000, "portEnd": 2000},
			{"id": "nat-g2", "device": "g2", "order": 2, "enabled": true,
				"matchSourceIp": "10.1.0.0/24", "translatedIp": "198.51.100.1",
				"portStart": 2000, "portEnd": 2000},
		},
		"policy": map[string]any{
			"defaultAction": "deny",
			"rules": []map[string]any{
				{"id": "r-new", "order": 1, "enabled": true, "action": "allow", "connState": "new"},
				{"id": "r-est", "order": 2, "enabled": true, "action": "allow", "connState": "established"},
				{"id": "r-ret", "order": 3, "enabled": true, "action": "allow", "connState": "return"},
			},
		},
	}
}

// TestConnCheckEndpointCarriesPacketAndBindings is the regression for
// the page/HTTP disconnect: the packet addresses/ports and the already
// registered mappings must reach the connection checker. With one
// translation port per gateway, the first host owns it and the second
// host cannot establish; return traffic through the other gateway must
// never be accepted as bidirectional.
func TestConnCheckEndpointCarriesPacketAndBindings(t *testing.T) {
	_, h := newTestServer(t)

	firstPkt := map[string]any{
		"protocol":    "tcp",
		"source":      map[string]any{"ip": "10.1.0.10", "port": 40001},
		"destination": map[string]any{"ip": "203.0.113.20", "port": 80},
	}
	var first map[string]any
	code := do(t, h, "POST", "/api/conncheck", map[string]any{
		"topology": twoGatewayTopoJSON(), "source": "h1", "target": "h2",
		"maxPaths": 3, "packet": firstPkt,
	}, &first)
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if first["establishable"] != true || first["bidirectional"] != true {
		t.Fatalf("first host must establish with return on its own gateway: %v", first)
	}
	mappings, _ := first["mappings"].([]any)
	if len(mappings) != 1 {
		t.Fatalf("first connection must record exactly one mapping, got %v", mappings)
	}
	m := mappings[0].(map[string]any)
	tr := m["translated"].(map[string]any)
	if tr["ip"] != "198.51.100.1" || tr["port"] != float64(2000) || m["device"] != "g1" {
		t.Fatalf("mapping must show actual translated ip/port: %v", m)
	}

	// Second host contends for the same single translation port while
	// the first connection's mapping is registered as occupied.
	secondPkt := map[string]any{
		"protocol":    "tcp",
		"source":      map[string]any{"ip": "10.1.0.11", "port": 40002},
		"destination": map[string]any{"ip": "203.0.113.20", "port": 80},
	}
	var second map[string]any
	do(t, h, "POST", "/api/conncheck", map[string]any{
		"topology": twoGatewayTopoJSON(), "source": "h3", "target": "h2",
		"maxPaths": 3, "packet": secondPkt, "bindings": mappings,
	}, &second)
	if second["establishable"] != false || second["bidirectional"] != false {
		t.Fatalf("second host must not get the occupied single port: %v", second)
	}
	if ms, _ := second["mappings"].([]any); len(ms) != 0 {
		t.Fatalf("no mapping may be recorded for a blocked connection: %v", ms)
	}

	// Return traffic entering through the other gateway cannot be
	// unwound there: g2 owns no mapping for the first connection, so a
	// packet arriving at g2's translated address must be denied as
	// mapping-unconfirmed rather than delivered to anyone.
	asym := map[string]any{
		"nodes": []map[string]any{
			{"id": "h1", "kind": "host"}, {"id": "g1", "kind": "nat"},
			{"id": "g2", "kind": "nat"}, {"id": "h2", "kind": "host"},
		},
		"links": []map[string]any{
			{"id": "a1", "source": "h1", "target": "g1", "kind": "directed"},
			{"id": "a2", "source": "g1", "target": "h2", "kind": "directed"},
			{"id": "a3", "source": "h2", "target": "g2", "kind": "directed"},
			{"id": "a4", "source": "g2", "target": "h1", "kind": "directed"},
		},
		"natRules": []map[string]any{},
		"policy":   twoGatewayTopoJSON()["policy"],
	}
	asym["natRules"] = []map[string]any{
		{"id": "nat-g1", "device": "g1", "egressLinkId": "a2", "order": 1, "enabled": true,
			"matchSourceIp": "10.1.0.0/24", "translatedIp": "198.51.100.1",
			"portStart": 2000, "portEnd": 2000},
	}
	var asymRes map[string]any
	do(t, h, "POST", "/api/conncheck", map[string]any{
		"topology": asym, "source": "h1", "target": "h2",
		"maxPaths": 3, "packet": firstPkt,
	}, &asymRes)
	if asymRes["establishable"] != true || asymRes["asymmetric"] != true || asymRes["bidirectional"] != false {
		t.Fatalf("return through g2 must be unconfirmed even though forward passed via g1: %v", asymRes)
	}
}
