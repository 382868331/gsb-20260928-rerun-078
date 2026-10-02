package topo

import (
	"fmt"
	"sort"
)

// explainFlip attributes a reachability change of the ordered pair
// (src,dst) between two versions to the concrete ordered rule, default
// action or topology edit that flipped a specific directed segment.
//
// It re-evaluates each decisive segment with the policy engine on both
// sides, so the attribution does not depend on two candidate paths
// happening to line up hop for hop (a new deny may move the chosen
// route entirely).
func explainFlip(before, after *Topology, src, dst string) []HopChange {
	return explainFlipState(before, after, src, dst, "")
}

// explainFlipState is explainFlip evaluated for one connection-state
// phase, so attribution matches the traffic phase being compared.
func explainFlipState(before, after *Topology, src, dst, state string) []HopChange {
	bRes, _ := before.FindPathsState(src, dst, 5, state)
	aRes, _ := after.FindPathsState(src, dst, 5, state)
	if bRes == nil || aRes == nil {
		// An endpoint was added or removed between versions: there is
		// no shared path to attribute, say so explicitly.
		return []HopChange{{
			From: src, To: dst,
			BeforeAction: verdictAction(before, bRes),
			AfterAction:  verdictAction(after, aRes),
			Cause:        "unknown",
			Detail:       "端点节点在某一版本中不存在，无法逐跳归因（节点可能被删除或新增）",
		}}
	}
	bEng, _ := BuildEngine(before)
	aEng, _ := BuildEngine(after)

	var changes []HopChange
	reported := map[string]bool{}
	add := func(c HopChange) {
		key := c.From + "\x00" + c.To + "\x00" + c.LinkID + "\x00" + c.Cause
		if !reported[key] {
			reported[key] = true
			changes = append(changes, c)
		}
	}

	if aRes.Reachable {
		// After is reachable: inspect every hop of the chosen allowed
		// paths and see how the same segment was decided before.
		for _, p := range aRes.Paths {
			for _, ah := range p.Hops {
				if c, flipped := segmentDecision(before, after, bEng, aEng, ah, state); flipped {
					add(c)
				}
			}
		}
		// Segments the before routes needed but the after topology lost.
		for _, c := range removedSegments(before, after, bEng, aEng, bRes) {
			add(c)
		}
	} else {
		// After is blocked: the decisive segments are the denied hops on
		// every blocked candidate (a deny on the detour is what actually
		// closes the pair even when the direct link also stays blocked).
		for _, p := range aRes.Blocked {
			for _, ah := range p.Hops {
				if ah.RuleID != "" && !decisionAllows(after, ah) {
					if c, flipped := segmentDecision(before, after, bEng, aEng, ah, state); flipped {
						add(c)
					}
				}
			}
		}
		for _, c := range removedSegments(before, after, bEng, aEng, bRes) {
			add(c)
		}
	}

	if len(changes) == 0 {
		changes = append(changes, HopChange{
			From: src, To: dst,
			BeforeAction: verdictAction(before, bRes),
			AfterAction:  verdictAction(after, aRes),
			Cause:        "unknown",
			Detail:       "未能定位到改变该方向的具体有序规则或链路（端点或整体拓扑可能变化）",
		})
	}
	sortChangesBy(changes, aRes, bRes)
	return changes
}

// segmentDecision evaluates the same directed segment (ah) under the
// before policy and returns the change if its effective decision
// flipped; ok=false when the segment is unchanged or uninformative.
func segmentDecision(before, after *Topology, bEng, aEng *engine, ah Hop, state string) (HopChange, bool) {
	bl := linkByIDTopo(before, ah.LinkID)
	al := linkByIDTopo(after, ah.LinkID)
	c := HopChange{
		From: ah.From, To: ah.To, LinkID: ah.LinkID,
		AfterRuleID: ah.RuleID, AfterRuleOrder: ah.RuleOrder,
		AfterAction: decisionLabel(after, ah),
	}

	// Directioned existence of the segment in each topology.
	_, _, beforeHas := bEng.edgeBetween(ah.From, ah.To)
	if al == nil || !beforeHas {
		if bl == nil && al != nil {
			c.Cause = "link-added"
			c.Detail = fmt.Sprintf("新增链路 %s（%s→%s）", ah.LinkID, ah.From, ah.To)
			return c, true
		}
		if bl != nil && al != nil && bl.Kind != al.Kind {
			c.Cause = "link-direction-changed"
			c.BeforeAction = defaultLabel(before)
			c.Detail = fmt.Sprintf("链路 %s 方向属性由 %s 改为 %s", ah.LinkID, bl.Kind, al.Kind)
			return c, true
		}
		return c, false
	}

	bd := before.DecideHopState(ah.From, ah.To, ah.LinkID, state)
	c.BeforeRuleID = bd.RuleID
	c.BeforeRuleOrder = bd.RuleOrder
	c.BeforeAction = decisionLabelD(before, bd)

	if effectiveAllows(before, bd) == effectiveAllows(after, after.DecideHopState(ah.From, ah.To, ah.LinkID, state)) && bd.RuleID == ah.RuleID {
		return c, false
	}
	if effectiveAllows(before, bd) == decisionAllows(after, ah) && bd.RuleID == ah.RuleID {
		return c, false
	}

	br := ruleByIDTopo(before, bd.RuleID)
	ar := ruleByIDTopo(after, ah.RuleID)
	switch {
	case bd.RuleID == "" && ah.RuleID == "":
		c.Cause = "default-changed"
		c.Detail = fmt.Sprintf("默认动作由 %s 改为 %s", c.BeforeAction, c.AfterAction)
	case bd.RuleID == "" && ar != nil:
		c.Cause = classifyRuleAdded(before, ar)
	case ah.RuleID == "" && br != nil:
		c.Cause = classifyRuleRemoved(after, br)
	case bd.RuleID == ah.RuleID:
		switch {
		case br != nil && !br.Enabled && ar != nil && ar.Enabled:
			c.Cause = "rule-enabled"
		case br != nil && br.Enabled && ar != nil && !ar.Enabled:
			c.Cause = "rule-disabled"
		case br != nil && ar != nil && br.Order != ar.Order:
			c.Cause = "rule-reordered"
			c.Detail = fmt.Sprintf("规则 %s 顺序由 %d 调整为 %d", ah.RuleID, br.Order, ar.Order)
		default:
			c.Cause = "rule-changed"
		}
	default:
		// Whether the winning rule identities themselves were edited:
		// new id => added rule now first-matches; vanished id => removed.
		switch {
		case ah.RuleID != "" && ruleByIDTopo(before, ah.RuleID) == nil:
			c.Cause = "rule-added"
		case bd.RuleID != "" && ruleByIDTopo(after, bd.RuleID) == nil:
			c.Cause = "rule-removed"
		case br != nil && ar != nil:
			c.Cause = "rule-reordered"
			c.Detail = fmt.Sprintf("首条命中规则由 %s(顺序%d) 变为 %s(顺序%d)",
				bd.RuleID, bd.RuleOrder, ah.RuleID, ah.RuleOrder)
		case ar != nil:
			c.Cause = classifyRuleAdded(before, ar)
		default:
			c.Cause = classifyRuleRemoved(after, br)
		}
	}
	if c.Detail == "" {
		c.Detail = describeRuleChange(c)
	}
	return c, true
}

func removedSegments(before, after *Topology, bEng, aEng *engine, bRes *PathResult) []HopChange {
	if bRes == nil {
		return nil
	}
	pools := bRes.Paths
	if len(pools) == 0 {
		pools = bRes.Blocked
	}
	var out []HopChange
	seen := map[string]bool{}
	for _, p := range pools {
		for _, bh := range p.Hops {
			key := bh.LinkID + bh.From + bh.To
			if seen[key] {
				continue
			}
			_, _, afterHas := aEng.edgeBetween(bh.From, bh.To)
			al := linkByIDTopo(after, bh.LinkID)
			if afterHas && al != nil {
				continue
			}
			seen[key] = true
			c := HopChange{
				From: bh.From, To: bh.To, LinkID: bh.LinkID,
				BeforeRuleID: bh.RuleID, BeforeRuleOrder: bh.RuleOrder,
				BeforeAction: decisionLabel(before, bh),
			}
			switch {
			case al == nil:
				c.Cause = "link-removed"
				c.Detail = fmt.Sprintf("删除链路 %s（%s→%s）", bh.LinkID, bh.From, bh.To)
			default:
				bl := linkByIDTopo(before, bh.LinkID)
				c.Cause = "link-direction-changed"
				c.Detail = fmt.Sprintf("链路 %s 方向属性由 %s 改为 %s，%s→%s 不再可走", bh.LinkID, safeKind(bl), safeKind(al), bh.From, bh.To)
			}
			out = append(out, c)
		}
	}
	return out
}

func safeKind(l *Link) string {
	if l == nil {
		return "(不存在)"
	}
	return string(l.Kind)
}

func effectiveAllows(t *Topology, d HopDecision) bool {
	if d.RuleID == "" {
		return t.Policy.DefaultAction == ActionAllow
	}
	return d.Allowed
}

func decisionAllows(t *Topology, h Hop) bool {
	if h.RuleID == "" {
		return t.Policy.DefaultAction == ActionAllow
	}
	return h.Action == ActionAllow
}

func defaultLabel(t *Topology) string {
	if t.Policy.DefaultAction == ActionAllow {
		return "default-allow"
	}
	return "default-deny"
}

func decisionLabel(t *Topology, h Hop) string {
	if h.RuleID == "" {
		return defaultLabel(t)
	}
	return string(h.Action)
}

func decisionLabelD(t *Topology, d HopDecision) string {
	if d.RuleID == "" {
		return defaultLabel(t)
	}
	if d.Allowed {
		return string(ActionAllow)
	}
	return string(ActionDeny)
}

func verdictAction(t *Topology, r *PathResult) string {
	if r == nil {
		return "no-route"
	}
	if r.Reachable {
		return "reachable"
	}
	if len(r.Blocked) > 0 {
		return defaultLabel(t)
	}
	return "no-route"
}

func linkByIDTopo(t *Topology, id string) *Link {
	for i := range t.Links {
		if t.Links[i].ID == id {
			return &t.Links[i]
		}
	}
	return nil
}

func ruleByIDTopo(t *Topology, id string) *Rule {
	if id == "" {
		return nil
	}
	for i := range t.Policy.Rules {
		if t.Policy.Rules[i].ID == id {
			return &t.Policy.Rules[i]
		}
	}
	return nil
}

func classifyRuleAdded(before *Topology, ar *Rule) string {
	if br := ruleByIDTopo(before, ar.ID); br != nil {
		if !br.Enabled && ar.Enabled {
			return "rule-enabled"
		}
		return "rule-changed"
	}
	return "rule-added"
}

func classifyRuleRemoved(after *Topology, br *Rule) string {
	if ar := ruleByIDTopo(after, br.ID); ar != nil {
		if br.Enabled && !ar.Enabled {
			return "rule-disabled"
		}
		return "rule-changed"
	}
	return "rule-removed"
}

func describeRuleChange(c HopChange) string {
	switch c.Cause {
	case "rule-added":
		return fmt.Sprintf("新增规则 %s(顺序%d) 对该段%s", c.AfterRuleID, c.AfterRuleOrder, zhDecision(c.AfterAction))
	case "rule-removed":
		return fmt.Sprintf("删除规则 %s，该段回落到 %s", c.BeforeRuleID, zhDecision(c.AfterAction))
	case "rule-enabled":
		return fmt.Sprintf("启用规则 %s(顺序%d)", c.AfterRuleID, c.AfterRuleOrder)
	case "rule-disabled":
		return fmt.Sprintf("停用规则 %s(顺序%d)，回落到默认动作", c.BeforeRuleID, c.BeforeRuleOrder)
	case "rule-reordered":
		if c.Detail != "" {
			return c.Detail
		}
		return "规则重排改变首条命中规则"
	case "rule-changed":
		return fmt.Sprintf("规则 %s 被修改", c.AfterRuleID)
	case "default-changed":
		return fmt.Sprintf("默认动作：%s → %s", zhDecision(c.BeforeAction), zhDecision(c.AfterAction))
	default:
		return c.Cause
	}
}

func zhDecision(a string) string {
	switch a {
	case "allow":
		return "放行"
	case "deny":
		return "拒绝"
	case "default-allow":
		return "默认放行"
	case "default-deny":
		return "默认拒绝"
	default:
		return a
	}
}

// sortChangesBy orders attributions by their position on the after
// paths (traffic path first), then before paths, giving a saved version
// one stable rendering every time it is reopened.
func sortChangesBy(changes []HopChange, aRes, bRes *PathResult) {
	positions := func(res *PathResult) map[string]int {
		m := map[string]int{}
		if res == nil {
			return m
		}
		walk := func(paths []Path, offset int) {
			for pi, p := range paths {
				for hi, h := range p.Hops {
					key := h.From + "\x00" + h.To + "\x00" + h.LinkID
					if _, ok := m[key]; !ok {
						m[key] = offset + pi*1000 + hi
					}
				}
			}
		}
		walk(res.Paths, 0)
		walk(res.Blocked, 100000)
		return m
	}
	pa := positions(aRes)
	pb := positions(bRes)
	rank := func(c HopChange) int {
		key := c.From + "\x00" + c.To + "\x00" + c.LinkID
		if i, ok := pa[key]; ok {
			return i
		}
		if i, ok := pb[key]; ok {
			return 500000 + i
		}
		return 1000000
	}
	sort.SliceStable(changes, func(i, j int) bool {
		ri, rj := rank(changes[i]), rank(changes[j])
		if ri != rj {
			return ri < rj
		}
		if changes[i].From != changes[j].From {
			return changes[i].From < changes[j].From
		}
		if changes[i].To != changes[j].To {
			return changes[i].To < changes[j].To
		}
		return changes[i].Cause < changes[j].Cause
	})
}
