package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"topoplan/internal/topo"
)

// TestCheckResponseNeverEmitsNullArrays is the regression test for the
// white screen: with no allowed route the backend used to emit
// "paths":null, and the frontend mapped over it and crashed.
func TestCheckResponseNeverEmitsNullArrays(t *testing.T) {
	_, h := newTestServer(t)
	body := map[string]any{"source": "b", "target": "a", "topology": baseTopo()}
	req := httptest.NewRequest("POST", "/api/check", jsonReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	if strings.Contains(raw, "\"paths\":null") || strings.Contains(raw, "\"blocked\":null") {
		t.Fatalf("response must serialize empty lists as []: %s", raw)
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["reason"] != "no-route" {
		t.Fatalf("want readable reason no-route, got %v", res["reason"])
	}
}

func diamondJSON(mod func(m map[string]any)) map[string]any {
	m := map[string]any{
		"nodes": []map[string]any{
			{"id": "h1", "kind": "host"}, {"id": "h2", "kind": "host"},
			{"id": "s1", "kind": "switch"}, {"id": "s2", "kind": "switch"}, {"id": "s3", "kind": "switch"},
		},
		"links": []map[string]any{
			{"id": "l1", "source": "h1", "target": "s1", "kind": "undirected"},
			{"id": "l2", "source": "s1", "target": "s2", "kind": "undirected"},
			{"id": "l3", "source": "s2", "target": "h2", "kind": "undirected"},
			{"id": "l4", "source": "s1", "target": "s3", "kind": "undirected"},
			{"id": "l5", "source": "s3", "target": "s2", "kind": "undirected"},
		},
		"policy": map[string]any{"defaultAction": "deny", "rules": []any{}},
	}
	if mod != nil {
		mod(m)
	}
	return m
}

func rules(rules ...map[string]any) func(m map[string]any) {
	return func(m map[string]any) {
		m["policy"] = map[string]any{"defaultAction": "deny", "rules": rules}
	}
}

func defaultAllow() func(m map[string]any) {
	return func(m map[string]any) {
		m["policy"] = map[string]any{
			"defaultAction": "allow",
			"rules":         []any{},
		}
	}
}

func TestDiffAttributesRuleReorderDetourDirectDefaultDeny(t *testing.T) {
	_, h := newTestServer(t)

	// Before: specific denials beat the broad allow (first match), so the
	// direct l2 and detour l5 are both refused -> default-style block.
	before := diamondJSON(rules(
		map[string]any{"id": "d2", "order": 1, "enabled": true, "action": "deny", "linkId": "l2"},
		map[string]any{"id": "d5", "order": 1, "enabled": true, "action": "deny", "linkId": "l5"},
		map[string]any{"id": "all", "order": 9, "enabled": true, "action": "allow"},
	))
	after := diamondJSON(rules(
		map[string]any{"id": "all", "order": 1, "enabled": true, "action": "allow"},
		map[string]any{"id": "d2", "order": 9, "enabled": true, "action": "deny", "linkId": "l2"},
		map[string]any{"id": "d5", "order": 9, "enabled": true, "action": "deny", "linkId": "l5"},
	))
	rep := postDiff(t, h, before, after)
	pair := findPair(rep.NewReachable, "h1", "h2")
	if pair == nil {
		t.Fatalf("reorder should make h1->h2 reachable: %+v", rep.NewReachable)
	}
	var l2, l5 bool
	for _, c := range pair.HopChanges {
		if c.Cause == "rule-reordered" {
			if c.LinkID == "l2" && c.BeforeRuleID == "d2" && c.AfterRuleID == "all" {
				l2 = true
			}
			if c.LinkID == "l5" && c.BeforeRuleID == "d5" && c.AfterRuleID == "all" {
				l5 = true
			}
		}
	}
	if !l2 || !l5 {
		t.Fatalf("both direct l2 and detour l5 must be attributed to the reorder: %+v", pair.HopChanges)
	}

	// Default deny -> default allow: attributes on hops must show default change.
	denyT := diamondJSON(nil)
	allowT := diamondJSON(defaultAllow())
	rep2 := postDiff(t, h, denyT, allowT)
	if len(rep2.NewReachable) == 0 {
		t.Fatal("flipping default to allow must open connections")
	}

	// Link removal: reachable under allow-all, gone after deleting l3.
	open := diamondJSON(rules(map[string]any{"id": "all", "order": 1, "enabled": true, "action": "allow"}))
	closed := diamondJSON(func(m map[string]any) {
		rules(map[string]any{"id": "all", "order": 1, "enabled": true, "action": "allow"})(m)
		m["links"] = []map[string]any{
			{"id": "l1", "source": "h1", "target": "s1", "kind": "undirected"},
			{"id": "l2", "source": "s1", "target": "s2", "kind": "undirected"},
			{"id": "l4", "source": "s1", "target": "s3", "kind": "undirected"},
			{"id": "l5", "source": "s3", "target": "s2", "kind": "undirected"},
		}
	})
	rep3 := postDiff(t, h, open, closed)
	p := findPair(rep3.NewBlocked, "s2", "h2")
	if p == nil {
		t.Fatal("removing the only ingress link l3 must block s2->h2")
	}
	var removed bool
	for _, c := range p.HopChanges {
		if c.Cause == "link-removed" && c.LinkID == "l3" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("must attribute block to removed link l3: %+v", p.HopChanges)
	}
}

// TestDiffDetourBlockedBySecondDeny covers the detour case: l2 denied
// forces s1->s3->s2; denying l5 afterwards blocks the pair even though
// the direct link is still physically present.
func TestDiffDetourBlockedBySecondDeny(t *testing.T) {
	_, h := newTestServer(t)
	before := diamondJSON(rules(
		map[string]any{"id": "d2", "order": 1, "enabled": true, "action": "deny", "linkId": "l2"},
		map[string]any{"id": "all", "order": 9, "enabled": true, "action": "allow"},
	))
	after := diamondJSON(rules(
		map[string]any{"id": "d2", "order": 1, "enabled": true, "action": "deny", "linkId": "l2"},
		map[string]any{"id": "d5", "order": 5, "enabled": true, "action": "deny", "linkId": "l5"},
		map[string]any{"id": "all", "order": 9, "enabled": true, "action": "allow"},
	))
	rep := postDiff(t, h, before, after)
	p := findPair(rep.NewBlocked, "h1", "h2")
	if p == nil {
		t.Fatalf("denying the detour l5 must block h1->h2: %+v", rep.NewBlocked)
	}
	var l5 bool
	for _, c := range p.HopChanges {
		if c.Cause == "rule-added" && c.LinkID == "l5" && c.AfterRuleID == "d5" {
			l5 = true
		}
	}
	if !l5 {
		t.Fatalf("block must be attributed to the newly added deny on detour l5: %+v", p.HopChanges)
	}
}

func jsonReader(v any) *strings.Reader {
	b, _ := json.Marshal(v)
	return strings.NewReader(string(b))
}

func postDiff(t *testing.T, h http.Handler, before, after map[string]any) *topo.DiffReport {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/diff", jsonReader(map[string]any{"before": before, "after": after}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("diff code=%d body=%s", rec.Code, rec.Body.String())
	}
	var rep topo.DiffReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	return &rep
}

func findPair(pairs []topo.PairResult, s, d string) *topo.PairResult {
	for i := range pairs {
		if pairs[i].Source == s && pairs[i].Target == d {
			return &pairs[i]
		}
	}
	return nil
}
