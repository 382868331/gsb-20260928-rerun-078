package topo

import "sort"

// PairResult is the reachability explanation for one ordered
// (source, target) pair under a given topology.
type PairResult struct {
	Source     string        `json:"source"`
	Target     string        `json:"target"`
	Before     bool          `json:"before"`
	After      bool          `json:"after"`
	Change     string        `json:"change"` // none | now-reachable | now-blocked
	BeforePath []string      `json:"beforePath"`
	AfterPath  []string      `json:"afterPath"`
	BeforeKind string        `json:"beforeKind"` // direct | detour | denied | no-route
	AfterKind  string        `json:"afterKind"`
	Segments   []SegmentDiff `json:"segments"`
	Summary    string        `json:"summary,omitempty"`
	BeforeAt   *PathResult   `json:"beforeAt,omitempty"`
	AfterAt    *PathResult   `json:"afterAt,omitempty"`
}

// SegmentDiff explains one directed segment (hop) of a pair before and
// after the edit, attributing the reachability change to the exact
// ordered rule that decided the segment on each side.
type SegmentDiff struct {
	From         string     `json:"from"`
	To           string     `json:"to"`
	LinkID       string     `json:"linkId"`
	Present      string     `json:"present"` // both | before-only | after-only
	BeforeRuleID string     `json:"beforeRuleId,omitempty"`
	BeforeOrder  int        `json:"beforeOrder"`
	BeforeAction RuleAction `json:"beforeAction,omitempty"`
	AfterRuleID  string     `json:"afterRuleId,omitempty"`
	AfterOrder   int        `json:"afterOrder"`
	AfterAction  RuleAction `json:"afterAction,omitempty"`
	Cause        string     `json:"cause"`
	Detail       string     `json:"detail"`
}

// DiffReport answers: after a policy edit, which ordered connections
// became reachable and which became blocked. Both topology versions
// share node identities; links may also differ. The check runs for
// every ordered pair of nodes present in either version, so direction
// is never collapsed.
type DiffReport struct {
	BeforeVersion int          `json:"beforeVersion"`
	AfterVersion  int          `json:"afterVersion"`
	NewReachable  []PairResult `json:"newReachable"`
	NewBlocked    []PairResult `json:"newBlocked"`
	Unchanged     []PairResult `json:"unchanged,omitempty"`
	TotalPairs    int          `json:"totalPairs"`
}

// ComparePolicy evaluates reachability for all ordered node pairs under
// before and after. detailed=true keeps the full path explanation on
// every changed pair; unchanged pairs only carry their boolean state.
func ComparePolicy(before, after *Topology, detailed bool) (*DiffReport, error) {
	rep := &DiffReport{
		BeforeVersion: before.Version,
		AfterVersion:  after.Version,
		NewReachable:  []PairResult{},
		NewBlocked:    []PairResult{},
	}
	idSet := map[string]struct{}{}
	for _, n := range before.Nodes {
		idSet[n.ID] = struct{}{}
	}
	for _, n := range after.Nodes {
		idSet[n.ID] = struct{}{}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, s := range ids {
		for _, d := range ids {
			if s == d {
				continue
			}
			rep.TotalPairs++
			br, berr := before.FindPaths(s, d, 3)
			ar, aerr := after.FindPaths(s, d, 3)
			// A pair involving a node missing from one side is "not routed"
			// there, not an error that aborts the whole comparison.
			if berr != nil {
				br = emptyResult(s, d)
			}
			if aerr != nil {
				ar = emptyResult(s, d)
			}
			b, a := br.Reachable, ar.Reachable
			pair := PairResult{Source: s, Target: d, Before: b, After: a}
			bp, bkind := chosenPath(before, br)
			ap, akind := chosenPath(after, ar)
			pair.BeforePath = bp
			pair.AfterPath = ap
			pair.BeforeKind = bkind
			pair.AfterKind = akind
			pair.Segments = diffSegments(before, after, bp, ap)
			pair.Summary = summarizePair(b, a, bkind, akind, pair.Segments)
			if detailed {
				pair.BeforeAt = br
				pair.AfterAt = ar
			}
			switch {
			case !b && a:
				pair.Change = "now-reachable"
				rep.NewReachable = append(rep.NewReachable, pair)
			case b && !a:
				pair.Change = "now-blocked"
				rep.NewBlocked = append(rep.NewBlocked, pair)
			default:
				pair.Change = "none"
				if detailed {
					rep.Unchanged = append(rep.Unchanged, pair)
				}
			}
		}
	}
	return rep, nil
}

func emptyResult(src, dst string) *PathResult {
	return &PathResult{Source: src, Target: dst, Paths: []Path{}, Blocked: []Path{}, Reason: "no-route"}
}

// chosenPath returns the node sequence that best represents the
// explanation for a pair (first allowed path; else first blocked
// candidate; else nothing) and classifies it as direct (physical
// shortest length), detour, denied or no-route.
func chosenPath(t *Topology, r *PathResult) ([]string, string) {
	if len(r.Paths) > 0 {
		return r.Paths[0].NodeIDs, classifyPath(t, r.Paths[0])
	}
	if len(r.Blocked) > 0 {
		return r.Blocked[0].NodeIDs, "denied"
	}
	return []string{}, "no-route"
}

// classifyPath compares hop count against the policy-free shortest one.
func classifyPath(t *Topology, p Path) string {
	if !p.Reachable || len(p.NodeIDs) == 0 {
		return "denied"
	}
	if len(p.NodeIDs) <= 1 {
		return "direct"
	}
	eng, err := BuildEngine(t)
	if err != nil {
		return "direct"
	}
	_, dist := dijkstra(eng, p.NodeIDs[0], nil, nil)
	if shortest, ok := dist[p.NodeIDs[len(p.NodeIDs)-1]]; ok && len(p.Hops) > shortest {
		return "detour"
	}
	return "direct"
}

type hopKey struct{ from, to string }

func diffSegments(before, after *Topology, bp, ap []string) []SegmentDiff {
	bHop := hopIndex(before, bp)
	aHop := hopIndex(after, ap)
	order := []hopKey{}
	seen := map[hopKey]bool{}
	add := func(k hopKey) {
		if !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	for i := 0; i+1 < len(bp); i++ {
		add(hopKey{bp[i], bp[i+1]})
	}
	for i := 0; i+1 < len(ap); i++ {
		add(hopKey{ap[i], ap[i+1]})
	}

	out := []SegmentDiff{}
	for _, k := range order {
		hb, inB := bHop[k]
		ha, inA := aHop[k]
		sg := SegmentDiff{From: k.from, To: k.to, Present: "both"}
		switch {
		case inB && !inA:
			sg.Present = "before-only"
			sg.LinkID = hb.LinkID
			sg.BeforeRuleID, sg.BeforeOrder, sg.BeforeAction = hb.RuleID, hb.RuleOrder, hb.Action
			sg.Cause, sg.Detail = "route-removed", "该段在修订后路径中消失（链路删除或改走绕行）"
		case !inB && inA:
			sg.Present = "after-only"
			sg.AfterRuleID, sg.AfterOrder, sg.AfterAction = ha.RuleID, ha.RuleOrder, ha.Action
			sg.LinkID = ha.LinkID
			sg.Cause, sg.Detail = "route-added", "该段在修订后路径中新出现（新增链路或改走绕行）"
		default:
			sg.LinkID = ha.LinkID
			sg.BeforeRuleID, sg.BeforeOrder, sg.BeforeAction = hb.RuleID, hb.RuleOrder, hb.Action
			sg.AfterRuleID, sg.AfterOrder, sg.AfterAction = ha.RuleID, ha.RuleOrder, ha.Action
			sg.Cause, sg.Detail = classifySegmentChange(before, after, hb, ha)
		}
		out = append(out, sg)
	}
	return out
}

// hopIndex replays ordered-rule decisions for the chosen node sequence
// so diffs never depend on Yen enumeration order.
func hopIndex(t *Topology, nodes []string) map[hopKey]Hop {
	m := map[hopKey]Hop{}
	if len(nodes) == 0 {
		return m
	}
	eng, err := BuildEngine(t)
	if err != nil {
		return m
	}
	for i := 0; i+1 < len(nodes); i++ {
		linkID, _, ok := eng.edgeBetween(nodes[i], nodes[i+1])
		if !ok {
			continue
		}
		d := t.DecideHop(nodes[i], nodes[i+1], linkID)
		action := t.Policy.DefaultAction
		if d.RuleID != "" {
			if d.Allowed {
				action = ActionAllow
			} else {
				action = ActionDeny
			}
		}
		m[hopKey{nodes[i], nodes[i+1]}] = Hop{
			From: nodes[i], To: nodes[i+1], LinkID: linkID,
			RuleID: d.RuleID, RuleOrder: d.RuleOrder, Action: action,
		}
	}
	return m
}

// classifySegmentChange attributes one segment decision change to the
// concrete ordered-rule edit: action/edit, reorder, default change, etc.
func classifySegmentChange(before, after *Topology, hb, ha Hop) (string, string) {
	switch {
	case hb.Action == ha.Action && hb.RuleID == ha.RuleID && hb.RuleOrder == ha.RuleOrder:
		return "none", "该段判定不变"
	case hb.RuleID == "" && ha.RuleID == "":
		if before.Policy.DefaultAction != after.Policy.DefaultAction {
			return "default-changed",
				"默认动作由 " + string(before.Policy.DefaultAction) + " 改为 " + string(after.Policy.DefaultAction)
		}
		return "none", "该段仍由默认动作决定"
	case hb.RuleID == ha.RuleID && hb.RuleID != "":
		if hb.RuleOrder != ha.RuleOrder {
			if hb.Action != ha.Action {
				return "rule-reordered",
					"规则 " + ha.RuleID + " 顺序由 " + itoa(hb.RuleOrder) + " 重排为 " + itoa(ha.RuleOrder) +
						"，动作由 " + string(hb.Action) + " 改为 " + string(ha.Action)
			}
			return "rule-reordered",
				"规则 " + ha.RuleID + " 顺序由 " + itoa(hb.RuleOrder) + " 重排为 " + itoa(ha.RuleOrder)
		}
		if hb.Action != ha.Action {
			return "rule-edited",
				"规则 " + ha.RuleID + " 动作由 " + string(hb.Action) + " 改为 " + string(ha.Action)
		}
		return "rule-edited", "规则 " + ha.RuleID + " 被修改"
	case hb.RuleID != ha.RuleID && hb.RuleID != "" && ha.RuleID != "":
		// A different first-match rule on the same segment: the ordered
		// list was rearranged (or a matcher changed) so precedence flipped.
		return "rule-swap",
			"首条命中规则由 " + hb.RuleID + "（顺序 " + itoa(hb.RuleOrder) + "，" + string(hb.Action) +
				"）变为 " + ha.RuleID + "（顺序 " + itoa(ha.RuleOrder) + "，" + string(ha.Action) + "），规则重排导致改判"
	case hb.RuleID == "":
		verb := "显式放行"
		if ha.Action == ActionDeny {
			verb = "显式拒绝"
		}
		return "rule-added",
			"新增/提前的规则 " + ha.RuleID + "（顺序 " + itoa(ha.RuleOrder) + "）" + verb +
				"，取代默认 " + string(before.Policy.DefaultAction)
	default:
		if ha.Action == ActionAllow {
			return "rule-removed",
				"规则 " + hb.RuleID + " 不再命中，默认动作 " + string(after.Policy.DefaultAction) + " 放行"
		}
		return "rule-removed", "规则 " + hb.RuleID + " 不再命中，落入默认拒绝"
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

func summarizePair(beforeOK, afterOK bool, bkind, akind string, segs []SegmentDiff) string {
	labels := map[string]string{
		"direct": "直达", "detour": "绕行", "denied": "被规则/默认拒绝", "no-route": "无物理路由",
	}
	head := labels[bkind] + " → " + labels[akind]
	for _, s := range segs {
		if s.Cause != "none" {
			return head + "；" + s.From + "→" + s.To + "：" + s.Detail
		}
	}
	return head
}
