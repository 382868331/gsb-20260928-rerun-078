package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"topoplan/internal/store"
	"topoplan/internal/topo"
)

// TestSavedTopologySurvivesRestart is the regression test for "saved
// rules, refreshed the page, got the initial draft back": a brand-new
// server process over the same data dir must hand back the saved
// topology with rule order intact, never a factory default.
func TestSavedTopologySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s1 := &server{store: st, draft: &topo.Topology{Policy: topo.Policy{DefaultAction: topo.ActionDeny}}}
	mux1 := http.NewServeMux()
	s1.registerAPI(mux1)

	topoIn := map[string]any{
		"nodes": []map[string]any{{"id": "a"}, {"id": "b"}},
		"links": []map[string]any{{"id": "lab", "source": "a", "target": "b", "kind": "directed"}},
		"policy": map[string]any{
			"defaultAction": "deny",
			"rules": []map[string]any{
				{"id": "r9", "order": 90, "enabled": true, "action": "deny", "linkId": "lab"},
				{"id": "r1", "order": 10, "enabled": true, "action": "allow"},
			},
		},
	}
	req := httptest.NewRequest("POST", "/api/save", jsonReader(map[string]any{"topology": topoIn, "label": "v1"}))
	rec := httptest.NewRecorder()
	mux1.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("save code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Simulate a restart: new server, same data dir.
	st2, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2 := &server{store: st2, draft: &topo.Topology{Policy: topo.Policy{DefaultAction: topo.ActionDeny}}}
	if d, err := st2.LoadDraft(); err == nil {
		s2.draft = d
	} else if snap, err := st2.Latest(); err == nil {
		s2.draft = snap.Topo
	}
	mux2 := http.NewServeMux()
	s2.registerAPI(mux2)

	req2 := httptest.NewRequest("GET", "/api/draft", nil)
	rec2 := httptest.NewRecorder()
	mux2.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("draft code=%d", rec2.Code)
	}
	var got topo.Topology
	if err := json.Unmarshal(rec2.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Fatalf("restored draft must be saved version 1, got v%d", got.Version)
	}
	if len(got.Policy.Rules) != 2 || got.Policy.Rules[0].ID != "r9" || got.Policy.Rules[1].ID != "r1" {
		t.Fatalf("rule identities/order must survive restart: %+v", got.Policy.Rules)
	}
	if got.Policy.DefaultAction != topo.ActionDeny {
		t.Fatalf("default action must survive restart: %s", got.Policy.DefaultAction)
	}
}

// TestFreshServerDraftIsEmpty ensures a never-saved data dir yields an
// empty v0 draft, so the frontend can show it as an unsaved scratch
// instead of mistaking it for a saved version.
func TestFreshServerDraftIsEmpty(t *testing.T) {
	_, h := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/draft", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var got topo.Topology
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 0 || len(got.Nodes) != 0 || len(got.Policy.Rules) != 0 {
		t.Fatalf("fresh draft must be empty v0, got %+v", got)
	}
}
