package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVersionsListNeverNull is the regression test for the first-load
// white screen: an empty data dir made GET /api/versions serialize
// null, and the frontend crashed mapping over it.
func TestVersionsListNeverNull(t *testing.T) {
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

// TestDiffListsNeverNull: a diff with zero changes must still emit
// empty arrays, not null.
func TestDiffListsNeverNull(t *testing.T) {
	_, h := newTestServer(t)
	body := map[string]any{"before": baseTopo(), "after": baseTopo()}
	req := httptest.NewRequest("POST", "/api/diff", jsonReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	raw := rec.Body.String()
	if strings.Contains(raw, "\"newReachable\":null") || strings.Contains(raw, "\"newBlocked\":null") {
		t.Fatalf("diff lists must serialize as []: %s", raw)
	}
}

// TestConnCheckEndpoint: phases always present, arrays never null,
// and a one-way permit is not reported as full reachability.
func TestConnCheckEndpoint(t *testing.T) {
	_, h := newTestServer(t)
	topo := map[string]any{
		"nodes": []map[string]any{{"id": "a"}, {"id": "b"}},
		"links": []map[string]any{
			{"id": "l1", "source": "a", "target": "b", "kind": "directed"},
		},
		"policy": map[string]any{
			"defaultAction": "deny",
			"rules": []map[string]any{
				{"id": "r1", "order": 1, "enabled": true, "action": "allow"},
			},
		},
	}
	body := map[string]any{"topology": topo, "source": "a", "target": "b"}
	req := httptest.NewRequest("POST", "/api/conncheck", jsonReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, bad := range []string{"\"paths\":null", "\"blocked\":null", "\"notes\":null", "\"phases\":null"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("conncheck must not emit %s: %s", bad, raw)
		}
	}
	var res struct {
		Reachable bool `json:"reachable"`
		Reason    string `json:"reason"`
		Phases    []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"phases"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Phases) != 4 {
		t.Fatalf("want 4 phases, got %d", len(res.Phases))
	}
	if res.Reachable {
		t.Fatal("directed one-way link: return traffic has no route, connection must not be reachable")
	}
	if res.Reason != "return-blocked" {
		t.Fatalf("want return-blocked, got %s", res.Reason)
	}
}

// TestConnDiffEndpointSeparatesStates: removing the new-connection
// allow must show up as new-connection impact plus a timeout risk,
// while established connections stay up.
func TestConnDiffEndpointSeparatesStates(t *testing.T) {
	_, h := newTestServer(t)
	mk := func(rules []map[string]any) map[string]any {
		return map[string]any{
			"nodes": []map[string]any{{"id": "a"}, {"id": "b"}},
			"links": []map[string]any{
				{"id": "l1", "source": "a", "target": "b", "kind": "undirected"},
			},
			"policy": map[string]any{"defaultAction": "deny", "rules": rules},
		}
	}
	before := mk([]map[string]any{
		{"id": "rn", "order": 1, "enabled": true, "action": "allow", "connState": "new"},
		{"id": "re", "order": 2, "enabled": true, "action": "allow", "connState": "established"},
	})
	after := mk([]map[string]any{
		{"id": "re", "order": 2, "enabled": true, "action": "allow", "connState": "established"},
	})
	req := httptest.NewRequest("POST", "/api/conndiff", jsonReader(map[string]any{"before": before, "after": after}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, bad := range []string{"\"newConnImpact\":null", "\"estConnImpact\":null", "\"timeoutRisks\":null"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("conndiff must not emit %s: %s", bad, raw)
		}
	}
	var rep struct {
		NewConnImpact []struct{ Source string `json:"source"` } `json:"newConnImpact"`
		EstConnImpact []struct{ Source string `json:"source"` } `json:"estConnImpact"`
		TimeoutRisks  []struct{ Source string `json:"source"` } `json:"timeoutRisks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.NewConnImpact) == 0 {
		t.Fatal("removing the new-connection allow must impact new connections")
	}
	if len(rep.EstConnImpact) != 0 {
		t.Fatalf("established connections must be unaffected: %+v", rep.EstConnImpact)
	}
	if len(rep.TimeoutRisks) == 0 {
		t.Fatal("connections surviving only until timeout must be listed")
	}
}

// TestConnStateValidation: an unknown connState is rejected, not
// silently treated as matching everything.
func TestConnStateValidation(t *testing.T) {
	_, h := newTestServer(t)
	topo := map[string]any{
		"nodes": []map[string]any{{"id": "a"}},
		"links": []map[string]any{},
		"policy": map[string]any{
			"defaultAction": "deny",
			"rules": []map[string]any{
				{"id": "r1", "order": 1, "enabled": true, "action": "allow", "connState": "weird"},
			},
		},
	}
	req := httptest.NewRequest("PUT", "/api/draft", jsonReader(topo))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid connState must be rejected, code=%d", rec.Code)
	}
}

