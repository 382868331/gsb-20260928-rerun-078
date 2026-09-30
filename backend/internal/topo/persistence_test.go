package topo

import (
	"encoding/json"
	"testing"
)

// TestCheckMarshalsEmptyListsAsArrays guards the wire contract: the
// React UI maps over paths/blocked, so JSON null (nil slices) used to
// white-screen the page.
func TestCheckMarshalsEmptyListsAsArrays(t *testing.T) {
	top := &Topology{
		Nodes:  []Node{{ID: "a"}, {ID: "b"}},
		Links:  []Link{{ID: "x", Source: "a", Target: "b", Kind: LinkDirected}},
		Policy: Policy{DefaultAction: ActionDeny},
	}
	res, err := top.FindPaths("b", "a", 3) // no route in that direction
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !contains(s, `"paths":[]`) || !contains(s, `"blocked":[]`) {
		t.Fatalf("empty lists must serialize as []: %s", s)
	}
	if res.Reason == "" {
		t.Fatal("reason must be present even with no result")
	}
}

// TestReopenedVersionProducesIdenticalExplanation: a saved immutable
// snapshot must explain the same pair identically after a full
// marshal/unmarshal round trip (save -> reopen), with no timestamp or
// map-iteration input. Deterministic path ordering is asserted by
// TestPathExplanationIsDeterministic; here we assert persistence keeps
// every deciding rule/order/link intact.
func TestReopenedVersionProducesIdenticalExplanation(t *testing.T) {
	top := diamondTopo()
	top.Version = 7
	top.Policy.Rules = []Rule{
		{ID: "d2", Order: 1, Enabled: true, Action: ActionDeny, LinkID: "l2"},
		{ID: "all", Order: 2, Enabled: true, Action: ActionAllow},
	}
	orig, err := top.FindPaths("h1", "h2", 3)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Topology
	if err := json.Unmarshal(b, &reopened); err != nil {
		t.Fatal(err)
	}
	again, err := reopened.FindPaths("h1", "h2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !sameResult(orig, again) {
		t.Fatalf("reopened snapshot explains differently:\n%+v\n%+v", orig, again)
	}
	if reopened.Version != 7 {
		t.Fatalf("version lineage lost: %d", reopened.Version)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
