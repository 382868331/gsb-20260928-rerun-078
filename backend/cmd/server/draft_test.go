package main

import (
	"testing"

	"topoplan/internal/store"
	"topoplan/internal/topo"
)

// TestDraftSurvivesRestart is the regression test for "saved,
// refreshed, got the factory sample back": the working draft is
// persisted and a fresh server on the same data dir restores it.
func TestDraftSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(st)

	edited := &topo.Topology{
		Nodes: []topo.Node{{ID: "a"}, {ID: "b"}},
		Links: []topo.Link{{ID: "lab", Source: "a", Target: "b", Kind: topo.LinkDirected}},
		Policy: topo.Policy{
			DefaultAction: topo.ActionDeny,
			Rules: []topo.Rule{
				{ID: "r2", Order: 20, Enabled: true, Action: topo.ActionAllow},
				{ID: "r1", Order: 10, Enabled: true, Action: topo.ActionDeny, LinkID: "lab"},
			},
		},
	}
	if err := st.SaveDraft(edited); err != nil {
		t.Fatal(err)
	}
	if srv.draft.Version != 0 || len(srv.draft.Nodes) != 0 {
		t.Fatalf("fresh server must start empty, got %+v", srv.draft)
	}

	// Simulate a restart: a new server over the same directory must
	// come back with the exact draft, rule order included.
	srv2 := newServer(st)
	if len(srv2.draft.Nodes) != 2 || len(srv2.draft.Policy.Rules) != 2 {
		t.Fatalf("draft not restored: %+v", srv2.draft)
	}
	if srv2.draft.Policy.Rules[0].ID != "r2" || srv2.draft.Policy.Rules[1].ID != "r1" {
		t.Fatalf("rule order must survive restart: %+v", srv2.draft.Policy.Rules)
	}

	// A saved version then becomes the draft, and also survives.
	snap, err := st.Save(edited, "v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveDraft(snap.Topo); err != nil {
		t.Fatal(err)
	}
	srv3 := newServer(st)
	if srv3.draft.Version != 1 {
		t.Fatalf("saved version must be the restored draft, got v%d", srv3.draft.Version)
	}

	// Legacy data dir with snapshots but no persisted draft: the
	// latest snapshot is the draft, never an empty topology.
	dir2 := t.TempDir()
	st2, _ := store.New(dir2)
	if _, err := st2.Save(baseTopo(), "only"); err != nil {
		t.Fatal(err)
	}
	srv4 := newServer(st2)
	if srv4.draft.Version != 1 || len(srv4.draft.Nodes) != 2 {
		t.Fatalf("latest snapshot must seed the draft: %+v", srv4.draft)
	}
}
