package topo

import (
	"encoding/json"
	"strings"
	"testing"
)

// Regression: a no-route result must serialize paths/blocked as [],
// never null (null crashed the React page into a white screen).
func TestFindPathsNeverEmitsNullArrays(t *testing.T) {
	top := &Topology{
		Nodes:  []Node{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		Links:  []Link{{ID: "lab", Source: "a", Target: "b", Kind: LinkDirected}},
		Policy: Policy{DefaultAction: ActionDeny},
	}
	res, err := top.FindPaths("a", "c", 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Reachable || res.Reason != "no-route" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Paths == nil || res.Blocked == nil {
		t.Fatal("slices must be non-nil")
	}
	b, _ := json.Marshal(res)
	s := string(b)
	if strings.Contains(s, `"paths":null`) || strings.Contains(s, `"blocked":null`) {
		t.Fatalf("null arrays in JSON: %s", s)
	}
	same, _ := top.FindPaths("a", "a", 3)
	b2, _ := json.Marshal(same)
	if strings.Contains(string(b2), "null") {
		t.Fatalf("same-node JSON contains null: %s", b2)
	}
}

func detourTopo() *Topology {
	top := diamondTopo()
	top.Policy.Rules = []Rule{
		{ID: "deny-l2", Order: 1, Enabled: true, Action: ActionDeny, LinkID: "l2"},
		{ID: "allow", Order: 2, Enabled: true, Action: ActionAllow},
	}
	return top
}

// 直达：放行走最短链路时分类必须是 direct。
func TestSegmentDiffDirect(t *testing.T) {
	direct := diamondTopo()
	direct.Policy.Rules = []Rule{
		{ID: "allow", Order: 1, Enabled: true, Action: ActionAllow},
	}
	rep, err := ComparePolicy(diamondTopo(), direct, false)
	if err != nil {
		t.Fatal(err)
	}
	var pair *PairResult
	for i := range rep.NewReachable {
		if rep.NewReachable[i].Source == "h1" && rep.NewReachable[i].Target == "h2" {
			pair = &rep.NewReachable[i]
		}
	}
	if pair == nil {
		t.Fatal("h1->h2 should be newly reachable")
	}
	if pair.AfterKind != "direct" || len(pair.AfterPath) != 4 {
		t.Fatalf("want direct 3-hop path, got kind=%s path=%v", pair.AfterKind, pair.AfterPath)
	}
}

// 绕行：直达链路 l2 被有序规则拒绝后必须改走 s3。
func TestSegmentDiffDetourAttributesRule(t *testing.T) {
	before := diamondTopo()
	before.Policy.Rules = []Rule{
		{ID: "allow", Order: 1, Enabled: true, Action: ActionAllow},
	}
	after := detourTopo()
	rep, err := ComparePolicy(before, after, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.NewBlocked) != 0 {
		t.Fatalf("detour keeps pairs reachable, but got blocked=%d", len(rep.NewBlocked))
	}
	ar, _ := after.FindPaths("h1", "h2", 3)
	if !ar.Reachable || len(ar.Paths[0].NodeIDs) != 5 || ar.Paths[0].NodeIDs[2] != "s3" {
		t.Fatalf("expected detour through s3: %+v", ar.Paths)
	}
	segs := diffSegments(before, after, []string{"h1", "s1", "s2", "h2"}, ar.Paths[0].NodeIDs)
	var sawRemoval bool
	for _, sg := range segs {
		if sg.LinkID == "l2" && sg.Present == "before-only" && sg.BeforeRuleID == "allow" {
			sawRemoval = true
		}
	}
	if !sawRemoval {
		t.Fatalf("expected the denied direct segment l2 to disappear from the after path: %+v", segs)
	}
	// And the same segment, evaluated in place, must attribute the denial
	// to the ordered deny-l2 rule rather than being silently passed.
	hb := hopIndex(before, []string{"h1", "s1", "s2", "h2"})
	ha := hopIndex(after, []string{"h1", "s1", "s2", "h2"})
	k := hopKey{"s1", "s2"}
	if hb[k].RuleID != "allow" || ha[k].RuleID != "deny-l2" || ha[k].Action != ActionDeny {
		t.Fatalf("l2 decision attribution wrong: before=%+v after=%+v", hb[k], ha[k])
	}
}

// 默认拒绝：删掉唯一放行后逐段报告 rule-removed -> 默认拒绝，
// 绝不能把"未放行/未完成"算作可达。
func TestSegmentDiffDefaultDeny(t *testing.T) {
	before := diamondTopo()
	before.Policy.Rules = []Rule{{ID: "allow", Order: 1, Enabled: true, Action: ActionAllow}}
	after := diamondTopo() // default deny, no rules
	rep, err := ComparePolicy(before, after, false)
	if err != nil {
		t.Fatal(err)
	}
	var pair *PairResult
	for i := range rep.NewBlocked {
		if rep.NewBlocked[i].Source == "h1" && rep.NewBlocked[i].Target == "h2" {
			pair = &rep.NewBlocked[i]
		}
	}
	if pair == nil || pair.After || pair.AfterKind != "denied" {
		t.Fatalf("missing allow must not count as pass: %+v", pair)
	}
	var defaultHits int
	for _, sg := range pair.Segments {
		if sg.BeforeRuleID == "allow" && sg.AfterRuleID == "" && sg.AfterAction == ActionDeny {
			if sg.Cause != "rule-removed" {
				t.Fatalf("cause=%s want rule-removed", sg.Cause)
			}
			defaultHits++
		}
	}
	if defaultHits != 3 {
		t.Fatalf("all 3 direct segments should hit default deny, got %d", defaultHits)
	}
}

// 规则重排：交换两条规则顺序，首条命中翻转，段差异点名顺序变化。
func TestSegmentDiffRuleReorder(t *testing.T) {
	mk := func(orderDeny, orderAllow int) *Topology {
		return &Topology{
			Nodes: []Node{{ID: "a"}, {ID: "b"}},
			Links: []Link{{ID: "l", Source: "a", Target: "b", Kind: LinkUndirected}},
			Policy: Policy{
				DefaultAction: ActionDeny,
				Rules: []Rule{
					{ID: "specific-deny", Order: orderDeny, Enabled: true, Action: ActionDeny, LinkID: "l"},
					{ID: "broad-allow", Order: orderAllow, Enabled: true, Action: ActionAllow},
				},
			},
		}
	}
	before, after := mk(1, 2), mk(2, 1)
	if r, _ := before.FindPaths("a", "b", 1); r.Reachable {
		t.Fatal("deny-first must block")
	}
	if r, _ := after.FindPaths("a", "b", 1); !r.Reachable {
		t.Fatal("allow-first must pass")
	}
	rep, err := ComparePolicy(before, after, false)
	if err != nil {
		t.Fatal(err)
	}
	// Undirected link: both ordered directions a->b and b->a flip.
	if len(rep.NewReachable) != 2 {
		t.Fatalf("want both ordered directions newly reachable: %+v", rep.NewReachable)
	}
	pair := rep.NewReachable[0]
	var swap *SegmentDiff
	for i := range pair.Segments {
		if pair.Segments[i].Cause == "rule-swap" {
			swap = &pair.Segments[i]
		}
	}
	if swap == nil {
		t.Fatalf("expected rule-swap segment: %+v", pair.Segments)
	}
	if swap.BeforeRuleID != "specific-deny" || swap.AfterRuleID != "broad-allow" {
		t.Fatalf("wrong rules on swap: %+v", swap)
	}
	if swap.BeforeOrder != 1 || swap.AfterOrder != 1 {
		t.Fatalf("both should cite the precedence slot 1: %+v", swap)
	}
}

// Reopening the same saved version explains identically every run.
func TestDeterministicExplanationAcrossRuns(t *testing.T) {
	first, _ := json.Marshal(mustFind(detourTopo(), "h1", "h2"))
	for i := 0; i < 20; i++ {
		got, _ := json.Marshal(mustFind(detourTopo(), "h1", "h2"))
		if string(got) != string(first) {
			t.Fatalf("non-deterministic explanation on run %d", i)
		}
	}
}

func mustFind(t *Topology, src, dst string) *PathResult {
	r, err := t.FindPaths(src, dst, 3)
	if err != nil {
		panic(err)
	}
	return r
}
