package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func natTopoJSON() map[string]any {
	return map[string]any{
		"nodes": []map[string]any{
			{"id": "h1", "kind": "host", "attrs": map[string]string{"ip": "10.0.0.1"}},
			{"id": "nat1", "kind": "firewall", "nat": map[string]any{"enabled": true, "address": "198.51.100.1", "portStart": 6000, "portEnd": 6000}},
			{"id": "h2", "kind": "host", "attrs": map[string]string{"ip": "192.0.2.1"}},
		},
		"links": []map[string]any{
			{"id": "l1", "source": "h1", "target": "nat1", "kind": "undirected"},
			{"id": "l2", "source": "nat1", "target": "h2", "kind": "undirected"},
		},
		"policy": map[string]any{"defaultAction": "allow", "rules": []any{}},
	}
}

// The conncheck endpoint accepts NAT trial inputs and always
// serializes the translation trace as arrays, never null.
func TestConnCheckNATEndpoint(t *testing.T) {
	_, h := newTestServer(t)
	body := map[string]any{
		"topology": natTopoJSON(), "source": "h1", "target": "h2",
		"srcPort": 5000, "dstPort": 80,
		"existing": []map[string]any{{
			"id": "c-old", "source": "h1", "target": "h2",
			"mappings": []map[string]any{{
				"device": "nat1", "insideAddr": "10.0.0.9", "insidePort": 5000,
				"natAddr": "198.51.100.1", "natPort": 6000,
				"peerAddr": "192.0.2.1", "peerPort": 80,
			}},
		}},
		"expiredConns": []string{"c-old"},
	}
	var res map[string]any
	code := do(t, h, "POST", "/api/conncheck", body, &res)
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if res["bidirectional"] != true {
		t.Fatalf("new connection should reuse the freed port and pass: %v", res)
	}
	raw := rawPost(t, h, body)
	for _, k := range []string{"natEvents", "newMappings", "keptMappings"} {
		if strings.Contains(raw, `"`+k+`":null`) {
			t.Fatalf("%s must serialize as [], got %s", k, raw)
		}
	}
	events, _ := res["natEvents"].([]any)
	foundReuse := false
	for _, e := range events {
		ev, _ := e.(map[string]any)
		if ev["direction"] == "late-return" && ev["decision"] == "port-reused" {
			foundReuse = true
		}
	}
	if !foundReuse {
		t.Fatalf("want a port-reused late-return event, got %v", events)
	}
}

// rawPost re-posts the body and returns the raw response text so the
// serialization guarantees can be checked on the wire format.
func rawPost(t *testing.T, h http.Handler, body map[string]any) string {
	t.Helper()
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/api/conncheck", &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}
