package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVersionsNeverEmitsNull is the regression test for the first-load
// white screen: an empty data dir made GET /api/versions serialize a
// nil slice as null, and the frontend mapped over it and crashed.
func TestVersionsNeverEmitsNull(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/versions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty version list must serialize as [], got %s", rec.Body.String())
	}
}

func connTopoJSON() map[string]any {
	return map[string]any{
		"nodes": []map[string]any{
			{"id": "h1", "kind": "host"}, {"id": "s1", "kind": "switch"},
			{"id": "s2", "kind": "switch"}, {"id": "h2", "kind": "host"},
		},
		"links": []map[string]any{
			{"id": "l1", "source": "h1", "target": "s1", "kind": "undirected"},
			{"id": "l2", "source": "s1", "target": "s2", "kind": "undirected"},
			{"id": "l3", "source": "s2", "target": "h2", "kind": "undirected"},
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

func TestConnCheckEndpoint(t *testing.T) {
	_, h := newTestServer(t)
	var res map[string]any
	code := do(t, h, "POST", "/api/conncheck", map[string]any{
		"topology": connTopoJSON(), "source": "h1", "target": "h2",
	}, &res)
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	if res["establishable"] != true || res["bidirectional"] != true || res["state"] != "established" {
		t.Fatalf("unexpected conn result: %v", res)
	}
	phases, _ := res["phases"].([]any)
	if len(phases) != 4 {
		t.Fatalf("want 4 lifecycle phases, got %v", phases)
	}

	// Simulated middle-node disconnect breaks the connection and the
	// trial leaves the server-side draft untouched.
	var broken map[string]any
	do(t, h, "POST", "/api/conncheck", map[string]any{
		"topology": connTopoJSON(), "source": "h1", "target": "h2",
		"downNodes": []string{"s1"},
	}, &broken)
	if broken["state"] != "broken" {
		t.Fatalf("down middle node must break: %v", broken["state"])
	}
	var draft map[string]any
	do(t, h, "GET", "/api/draft", nil, &draft)
	if draft["version"] != 0.0 {
		t.Fatalf("trial must not change the draft: %v", draft)
	}
}

func TestConnDiffEndpointSeparatesNewAndExisting(t *testing.T) {
	_, h := newTestServer(t)
	before := connTopoJSON()
	after := connTopoJSON()
	// Deny only new connections in "after".
	after["policy"] = map[string]any{
		"defaultAction": "deny",
		"rules": []map[string]any{
			{"id": "r-block-new", "order": 0, "enabled": true, "action": "deny", "connState": "new"},
			{"id": "r-new", "order": 1, "enabled": true, "action": "allow", "connState": "new"},
			{"id": "r-est", "order": 2, "enabled": true, "action": "allow", "connState": "established"},
			{"id": "r-ret", "order": 3, "enabled": true, "action": "allow", "connState": "return"},
		},
	}
	var rep map[string]any
	code := do(t, h, "POST", "/api/conndiff", map[string]any{
		"before": before, "after": after, "detailed": false,
		"connections": []map[string]any{
			{"id": "c1", "source": "h1", "target": "h2", "state": "established"},
		},
	}, &rep)
	if code != 200 {
		t.Fatalf("code=%d", code)
	}
	newConns := rep["newConns"].(map[string]any)
	if len(newConns["newBlocked"].([]any)) == 0 {
		t.Fatalf("new connections must be reported blocked: %v", newConns)
	}
	existing := rep["existing"].([]any)
	if len(existing) != 1 || existing[0].(map[string]any)["afterStatus"] != "survives" {
		t.Fatalf("existing connection must survive a new-only deny: %v", existing)
	}
}
