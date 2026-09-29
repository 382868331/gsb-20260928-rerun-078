package store

import (
	"testing"

	"topoplan/internal/topo"
)

func sample() *topo.Topology {
	return &topo.Topology{
		Nodes: []topo.Node{{ID: "n1"}, {ID: "n2"}},
		Links: []topo.Link{{ID: "l1", Source: "n1", Target: "n2", Kind: topo.LinkDirected}},
		Policy: topo.Policy{
			DefaultAction: topo.ActionDeny,
			Rules:         []topo.Rule{{ID: "r1", Action: topo.ActionAllow, Enabled: true, Order: 1}},
		},
	}
}

func TestSaveKeepsVersionLineageAndIDs(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s1, err := st.Save(sample(), "first")
	if err != nil {
		t.Fatal(err)
	}
	if s1.Meta.Version != 1 || s1.Topo.Nodes[0].ID != "n1" {
		t.Fatalf("unexpected snapshot: %+v", s1.Meta)
	}
	s2, err := st.Save(sample(), "second")
	if err != nil || s2.Meta.Version != 2 {
		t.Fatalf("second save should get version 2: %v %v", s2, err)
	}
	// re-saving the already-stamped v2 topology as-is must be rejected;
	// callers clone and zero the version for a new snapshot.
	if _, err := st.Save(s2.Topo, "dup"); err == nil {
		t.Fatal("overwriting an existing version must fail")
	}
	got, err := st.Load(1)
	if err != nil || got.Topo.Links[0].ID != "l1" || got.Topo.Policy.Rules[0].ID != "r1" {
		t.Fatalf("loaded snapshot lost identities: %+v err=%v", got, err)
	}
	metas, err := st.List()
	if err != nil || len(metas) != 2 {
		t.Fatalf("list=%v err=%v", metas, err)
	}
}
