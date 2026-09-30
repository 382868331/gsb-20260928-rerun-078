package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"topoplan/internal/store"
	"topoplan/internal/topo"
)

func newTestServer(t *testing.T) (*server, http.Handler) {
	t.Helper()
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &server{store: st, draft: &topo.Topology{Policy: topo.Policy{DefaultAction: topo.ActionDeny}}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/save", s.save)
	mux.HandleFunc("POST /api/check", s.check)
	mux.HandleFunc("POST /api/diff", s.diff)
	mux.HandleFunc("POST /api/import", s.importTopo)
	mux.HandleFunc("GET /api/versions", s.listVersions)
	return s, mux
}

func do(t *testing.T, h http.Handler, method, path string, body any, into any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if into != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("decode %s: %v body=%s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func baseTopo() *topo.Topology {
	return &topo.Topology{
		Nodes:  []topo.Node{{ID: "a"}, {ID: "b"}},
		Links:  []topo.Link{{ID: "lab", Source: "a", Target: "b", Kind: topo.LinkDirected}},
		Policy: topo.Policy{DefaultAction: topo.ActionDeny},
	}
}

// Regression for the white screen: no-route responses must serialize
// paths/blocked as empty arrays, never JSON null (null made the React
// panel throw and blank the page).
func TestCheckNoRouteNeverReturnsNullArrays(t *testing.T) {
	_, h := newTestServer(t)
	body := map[string]any{
		"source": "a",
		"target": "c",
		"topology": &topo.Topology{
			Nodes:  []topo.Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
			Links:  []topo.Link{{ID: "lab", Source: "a", Target: "b", Kind: topo.LinkDirected}},
			Policy: topo.Policy{DefaultAction: topo.ActionDeny},
		},
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/api/check", &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "null") {
		t.Fatalf("response must not contain null arrays: %s", rec.Body.String())
	}
}

func TestAPISaveCheckDiffImport(t *testing.T) {
	_, h := newTestServer(t)

	// 1) deny by default: cable alone does not pass
	var check topo.PathResult
	code := do(t, h, "POST", "/api/check", map[string]any{"source": "a", "target": "b", "topology": baseTopo()}, &check)
	if code != 200 || check.Reachable {
		t.Fatalf("default deny check: code=%d reachable=%v reason=%s", code, check.Reachable, check.Reason)
	}

	// 2) save the deny version
	var snap store.Snapshot
	if code := do(t, h, "POST", "/api/save", map[string]any{"topology": baseTopo(), "label": "deny"}, &snap); code != 200 {
		t.Fatalf("save denied version code=%d", code)
	}
	if snap.Meta.Version != 1 {
		t.Fatalf("want version 1, got %d", snap.Meta.Version)
	}

	// 3) after: add an allow rule
	after := baseTopo()
	after.Policy.Rules = []topo.Rule{{ID: "r-allow", Order: 1, Enabled: true, Action: topo.ActionAllow}}
	var check2 topo.PathResult
	do(t, h, "POST", "/api/check", map[string]any{"source": "a", "target": "b", "topology": after}, &check2)
	if !check2.Reachable || len(check2.Paths[0].Hops) != 1 || check2.Paths[0].Hops[0].RuleID != "r-allow" {
		t.Fatalf("allow check wrong: %+v", check2)
	}

	// 4) diff against saved v1: a->b now reachable, b->a never routed
	var rep topo.DiffReport
	code = do(t, h, "POST", "/api/diff", map[string]any{"after": after, "beforeVersion": 1}, &rep)
	if code != 200 || rep.TotalPairs != 2 {
		t.Fatalf("diff code=%d pairs=%d", code, rep.TotalPairs)
	}
	if len(rep.NewReachable) != 1 || rep.NewReachable[0].Source != "a" || rep.NewReachable[0].Target != "b" {
		t.Fatalf("unexpected diff: %+v", rep.NewReachable)
	}
	if len(rep.NewBlocked) != 0 {
		t.Fatalf("nothing should be blocked: %+v", rep.NewBlocked)
	}

	// 5) import preserves identities and resets lineage (unsaved draft)
	var imported topo.Topology
	do(t, h, "POST", "/api/import", after, &imported)
	if imported.Version != 0 || imported.Nodes[0].ID != "a" || imported.Links[0].ID != "lab" || imported.Policy.Rules[0].ID != "r-allow" {
		t.Fatalf("import rewrote identity: %+v", imported)
	}
}
