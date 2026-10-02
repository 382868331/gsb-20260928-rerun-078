package topo

import (
	"fmt"
	"sort"
)

// Connection lifecycle phases reported by ConnCheck. Every check
// returns all four phases in this order so a saved explanation keeps
// its shape when reopened.
const (
	PhaseEstablish = "establish" // first request of a new connection
	PhaseMaintain  = "maintain"  // subsequent packets of an existing connection
	PhaseReturn    = "return"    // return traffic of an existing connection
	PhaseTeardown  = "teardown"  // timeout / intermediate-node failure
)

// ConnPhase is one traffic phase of a connection check: the paths the
// phase's traffic can take, the rule basis of every hop, and readable
// notes. Paths/Blocked/Notes are always lists, never null.
type ConnPhase struct {
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	State     string   `json:"state"` // traffic state evaluated: new | established
	Reachable bool     `json:"reachable"`
	Paths     []Path   `json:"paths"`
	Blocked   []Path   `json:"blocked"`
	Reason    string   `json:"reason"`
	Notes     []string `json:"notes"`
}

// ConnCheckResult explains a connection across its whole lifecycle.
// Reachable is only true when both the first request (src->dst) and
// the return traffic (dst->src) pass: a single allowed forward path
// never implies the connection works when the return path is
// asymmetric or denied.
type ConnCheckResult struct {
	Source       string      `json:"source"`
	Target       string      `json:"target"`
	AfterTimeout bool        `json:"afterTimeout"`
	Phases       []ConnPhase `json:"phases"`
	Reachable    bool        `json:"reachable"`
	Reason       string      `json:"reason"`
	Asymmetric   bool        `json:"asymmetric"`
	ForwardNodes []string    `json:"forwardNodes"`
	ReturnNodes  []string    `json:"returnNodes"`
}

// ConnCheck evaluates a connection between src and dst phase by phase.
// afterTimeout simulates a connection whose state entry expired:
// maintain/return traffic is then re-judged under new-connection rules.
func (t *Topology) ConnCheck(src, dst string, k int, afterTimeout bool) (*ConnCheckResult, error) {
	res := &ConnCheckResult{
		Source: src, Target: dst, AfterTimeout: afterTimeout,
		Phases: []ConnPhase{}, ForwardNodes: []string{}, ReturnNodes: []string{},
	}
	est, err := t.FindPathsState(src, dst, k, ConnNew)
	if err != nil {
		return nil, err
	}
	res.Phases = append(res.Phases, ConnPhase{
		Name: PhaseEstablish, Title: "建立（首次请求，按新连接规则）",
		State: ConnNew, Reachable: est.Reachable,
		Paths: est.Paths, Blocked: est.Blocked, Reason: est.Reason,
		Notes: []string{},
	})
	if len(est.Paths) > 0 {
		res.ForwardNodes = est.Paths[0].NodeIDs
	}

	// Existing-connection traffic state: normally "established"; after
	// a timeout the state is gone and packets face new-connection rules.
	estState := ConnEstablished
	timeoutNote := ""
	if afterTimeout {
		estState = ConnNew
		timeoutNote = "连接已超时，状态表项失效，后续与返回流量按新连接规则重新评估"
	}

	maint, err := t.FindPathsState(src, dst, k, estState)
	if err != nil {
		return nil, err
	}
	mPhase := ConnPhase{
		Name: PhaseMaintain, Title: "保持（已有连接的去向流量）",
		State: estState, Reachable: maint.Reachable,
		Paths: maint.Paths, Blocked: maint.Blocked, Reason: maint.Reason,
		Notes: []string{},
	}
	if timeoutNote != "" {
		mPhase.Notes = append(mPhase.Notes, timeoutNote)
	}
	if !est.Reachable {
		mPhase.Notes = append(mPhase.Notes, "首次请求无法通过，连接不会建立，本阶段仅作假设性评估")
	}
	res.Phases = append(res.Phases, mPhase)

	ret, err := t.FindPathsState(dst, src, k, estState)
	if err != nil {
		return nil, err
	}
	rPhase := ConnPhase{
		Name: PhaseReturn, Title: "返回流量（宿到源，独立验证）",
		State: estState, Reachable: ret.Reachable,
		Paths: ret.Paths, Blocked: ret.Blocked, Reason: ret.Reason,
		Notes: []string{},
	}
	if timeoutNote != "" {
		rPhase.Notes = append(rPhase.Notes, timeoutNote)
	}
	if len(ret.Paths) > 0 {
		res.ReturnNodes = ret.Paths[0].NodeIDs
	}
	if len(res.ForwardNodes) > 0 && len(res.ReturnNodes) > 0 && !sameSeq(res.ReturnNodes, reversed(res.ForwardNodes)) {
		res.Asymmetric = true
		rPhase.Notes = append(rPhase.Notes,
			"返回路径与去程不对称：去程的一次放行不代表返程可达，返程已按自身逐跳规则独立验证")
	}
	if !ret.Reachable {
		rPhase.Notes = append(rPhase.Notes,
			"返回流量不可达：即使去程放行，连接也无法真正建立（非对称返回不被一次放行覆盖）")
	}
	res.Phases = append(res.Phases, rPhase)

	res.Phases = append(res.Phases, t.teardownPhase(est, estState, afterTimeout))

	switch {
	case !est.Reachable:
		res.Reason = "establish-blocked"
	case !ret.Reachable:
		res.Reason = "return-blocked"
	default:
		res.Reason = "two-way-established"
	}
	res.Reachable = est.Reachable && ret.Reachable
	return res, nil
}

// teardownPhase explains how the connection ends: what a timeout
// re-evaluation would do hop by hop, and which intermediate node or
// link failure breaks the chosen path (plus whether a detour exists).
func (t *Topology) teardownPhase(est *PathResult, estState string, afterTimeout bool) ConnPhase {
	p := ConnPhase{
		Name: PhaseTeardown, Title: "失效（超时与中间节点断开）",
		State: estState, Paths: []Path{}, Blocked: []Path{},
		Reason: "explained", Notes: []string{},
	}
	if len(est.Paths) == 0 {
		p.Reachable = false
		p.Reason = "no-established-path"
		p.Notes = append(p.Notes, "没有已建立的连接路径，无从失效")
		return p
	}
	path := est.Paths[0]
	p.Paths = []Path{path}
	p.Reachable = est.Reachable // re-establishment after timeout possible?

	// Timeout: every hop falls back to the new-connection rule set.
	for _, h := range path.Hops {
		d := t.DecideHopState(h.From, h.To, h.LinkID, ConnNew)
		p.Notes = append(p.Notes, fmt.Sprintf(
			"超时后 %s→%s（链路 %s）按新连接规则重审：%s",
			h.From, h.To, h.LinkID, hopBasisText(t, d)))
	}
	if est.Reachable {
		p.Notes = append(p.Notes, "超时后连接可按新连接规则重建")
	} else {
		p.Notes = append(p.Notes, "超时后连接无法按当前新连接规则重建")
	}

	// Intermediate node / link failure along the chosen path.
	for i, nid := range path.NodeIDs {
		if i == 0 || i == len(path.NodeIDs)-1 {
			continue
		}
		detour := detourAvoiding(t, path.NodeIDs[0], path.NodeIDs[len(path.NodeIDs)-1], nid)
		if detour {
			p.Notes = append(p.Notes, fmt.Sprintf("中间节点 %s 断开：当前路径在此中断，但存在绕过它的可行路径", nid))
		} else {
			p.Notes = append(p.Notes, fmt.Sprintf("中间节点 %s 断开：当前路径在此中断，且无绕过它的可行路径，连接失效", nid))
		}
	}
	for _, h := range path.Hops {
		detour := detourAvoidingLink(t, path.NodeIDs[0], path.NodeIDs[len(path.NodeIDs)-1], h.LinkID)
		if !detour {
			p.Notes = append(p.Notes, fmt.Sprintf("链路 %s（%s→%s）断开：无不含该链路的可行路径，连接失效", h.LinkID, h.From, h.To))
		}
	}
	if afterTimeout {
		p.Notes = append(p.Notes, "本次检查已按“连接已超时”模拟：保持与返回阶段均按新连接规则评估")
	}
	return p
}

func hopBasisText(t *Topology, d HopDecision) string {
	if d.RuleID == "" {
		if t.Policy.DefaultAction == ActionAllow {
			return "无规则命中，默认放行"
		}
		return "无规则命中，默认拒绝"
	}
	if d.Allowed {
		return fmt.Sprintf("命中规则 %s（顺序 %d）放行", d.RuleID, d.RuleOrder)
	}
	return fmt.Sprintf("命中规则 %s（顺序 %d）拒绝", d.RuleID, d.RuleOrder)
}

// detourAvoiding reports whether a policy-allowed new-connection path
// exists that skips the given node.
func detourAvoiding(t *Topology, src, dst, avoid string) bool {
	clone := *t
	nodes := make([]Node, 0, len(t.Nodes))
	for _, n := range t.Nodes {
		if n.ID != avoid {
			nodes = append(nodes, n)
		}
	}
	links := make([]Link, 0, len(t.Links))
	for _, l := range t.Links {
		if l.Source != avoid && l.Target != avoid {
			links = append(links, l)
		}
	}
	clone.Nodes, clone.Links = nodes, links
	r, err := clone.FindPaths(src, dst, 1)
	return err == nil && r.Reachable
}

// detourAvoidingLink reports whether a policy-allowed new-connection
// path exists that does not use the given link.
func detourAvoidingLink(t *Topology, src, dst, linkID string) bool {
	clone := *t
	links := make([]Link, 0, len(t.Links))
	for _, l := range t.Links {
		if l.ID != linkID {
			links = append(links, l)
		}
	}
	clone.Links = links
	r, err := clone.FindPaths(src, dst, 1)
	return err == nil && r.Reachable
}

func reversed(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

// ConnPairResult compares one ordered pair's connection viability
// between two versions, split by connection state.
type ConnPairResult struct {
	Source    string           `json:"source"`
	Target    string           `json:"target"`
	NewBefore bool             `json:"newBefore"`
	NewAfter  bool             `json:"newAfter"`
	EstBefore bool             `json:"estBefore"`
	EstAfter  bool             `json:"estAfter"`
	NewChange string           `json:"newChange"` // none | now-reachable | now-blocked
	EstChange string           `json:"estChange"`
	// TimeoutRisk: the existing connection survives, but a timeout
	// would drop it onto new-connection rules that refuse it.
	TimeoutRisk bool             `json:"timeoutRisk"`
	Notes       []string         `json:"notes"`
	AfterDetail *ConnCheckResult `json:"afterDetail,omitempty"`
}

// ConnDiffReport separates the impact of a policy/route edit on new
// connections from its impact on already-established ones, and lists
// connections that only survive until their next timeout.
type ConnDiffReport struct {
	BeforeVersion int              `json:"beforeVersion"`
	AfterVersion  int              `json:"afterVersion"`
	TotalPairs    int              `json:"totalPairs"`
	NewConnImpact []ConnPairResult `json:"newConnImpact"`
	EstConnImpact []ConnPairResult `json:"estConnImpact"`
	TimeoutRisks  []ConnPairResult `json:"timeoutRisks"`
}

// ConnDiff compares two topology/policy versions connection-state by
// connection-state over every ordered node pair.
func ConnDiff(before, after *Topology, detailed bool) (*ConnDiffReport, error) {
	rep := &ConnDiffReport{
		BeforeVersion: before.Version, AfterVersion: after.Version,
		NewConnImpact: []ConnPairResult{}, EstConnImpact: []ConnPairResult{},
		TimeoutRisks: []ConnPairResult{},
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
			nb, eb := connViability(before, s, d)
			na, ea := connViability(after, s, d)
			pair := ConnPairResult{
				Source: s, Target: d,
				NewBefore: nb, NewAfter: na, EstBefore: eb, EstAfter: ea,
				NewChange: changeLabel(nb, na), EstChange: changeLabel(eb, ea),
				TimeoutRisk: ea && !na,
				Notes:       []string{},
			}
			if pair.NewChange != "none" {
				pair.Notes = append(pair.Notes, fmt.Sprintf("新连接：%s", changeText(nb, na)))
			}
			if pair.EstChange != "none" {
				pair.Notes = append(pair.Notes, fmt.Sprintf("已有连接：%s", changeText(eb, ea)))
			}
			if pair.TimeoutRisk {
				pair.Notes = append(pair.Notes, "连接超时风险：已有连接当前可保持，但一旦超时将按新连接规则被拒绝，无法重建")
			}
			if pair.NewChange == "none" && pair.EstChange == "none" && !pair.TimeoutRisk {
				continue
			}
			if detailed {
				if res, err := after.ConnCheck(s, d, 2, false); err == nil {
					pair.AfterDetail = res
				}
			}
			if pair.NewChange != "none" {
				rep.NewConnImpact = append(rep.NewConnImpact, pair)
			}
			if pair.EstChange != "none" {
				rep.EstConnImpact = append(rep.EstConnImpact, pair)
			}
			if pair.TimeoutRisk {
				rep.TimeoutRisks = append(rep.TimeoutRisks, pair)
			}
		}
	}
	return rep, nil
}

// connViability reports (new-connection reachable, existing-connection
// viable). Existing-connection viability requires both the forward
// established traffic and the return traffic to pass: an asymmetric or
// denied return path means the connection is not truly up.
func connViability(t *Topology, src, dst string) (newOK, estOK bool) {
	fwd, err := t.FindPathsState(src, dst, 1, ConnNew)
	if err != nil {
		return false, false
	}
	newOK = fwd.Reachable
	m, err1 := t.FindPathsState(src, dst, 1, ConnEstablished)
	r, err2 := t.FindPathsState(dst, src, 1, ConnEstablished)
	if err1 != nil || err2 != nil {
		return newOK, false
	}
	return newOK, m.Reachable && r.Reachable
}

func changeLabel(before, after bool) string {
	switch {
	case !before && after:
		return "now-reachable"
	case before && !after:
		return "now-blocked"
	default:
		return "none"
	}
}

func changeText(before, after bool) string {
	switch {
	case !before && after:
		return "由不可达变为可达"
	case before && !after:
		return "由可达变为不可达"
	default:
		return "无变化"
	}
}

