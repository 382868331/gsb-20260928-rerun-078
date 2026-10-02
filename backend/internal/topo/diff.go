package topo

import "sort"

// PairResult is the reachability explanation for one ordered
// (source, target) pair under a given topology.
type PairResult struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Before bool   `json:"before"`
	After  bool   `json:"after"`
	Change string `json:"change"` // none | now-reachable | now-blocked
	// HopChanges attributes the reachability flip of this pair to the
	// concrete ordered rule / default action / topology change that
	// decided each affected segment. Always a list, never null.
	HopChanges []HopChange `json:"hopChanges"`
	BeforeAt   *PathResult `json:"beforeAt,omitempty"`
	AfterAt    *PathResult `json:"afterAt,omitempty"`
}

// HopChange explains why one directed segment (from -> to over a link)
// flipped between two policy/topology versions.
type HopChange struct {
	From            string `json:"from"`
	To              string `json:"to"`
	LinkID          string `json:"linkId"`
	BeforeRuleID    string `json:"beforeRuleId,omitempty"`
	BeforeRuleOrder int    `json:"beforeRuleOrder,omitempty"`
	BeforeAction    string `json:"beforeAction,omitempty"` // allow | deny | default-allow | default-deny | no-route
	AfterRuleID     string `json:"afterRuleId,omitempty"`
	AfterRuleOrder  int    `json:"afterRuleOrder,omitempty"`
	AfterAction     string `json:"afterAction,omitempty"`
	// Cause: rule-reordered | rule-changed | rule-enabled | rule-disabled |
	// rule-added | rule-removed | default-changed | link-added |
	// link-removed | link-direction-changed | unchanged | unknown
	Cause  string `json:"cause"`
	Detail string `json:"detail,omitempty"`
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
	return ComparePolicyState(before, after, detailed, "")
}

// ComparePolicyState is ComparePolicy evaluated for one connection
// state ("", "new", "established", "return"): rules carrying a
// connState matcher only fire for their own traffic phase, so a diff
// for new connections and one for established ones can differ.
func ComparePolicyState(before, after *Topology, detailed bool, state string) (*DiffReport, error) {
	rep := &DiffReport{
		BeforeVersion: before.Version, AfterVersion: after.Version,
		NewReachable: []PairResult{}, NewBlocked: []PairResult{}, // never null
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
			b, _ := evalQuietState(before, s, d, state)
			a, _ := evalQuietState(after, s, d, state)
			pair := PairResult{Source: s, Target: d, Before: b, After: a, HopChanges: []HopChange{}}
			if detailed {
				br, _ := before.FindPathsState(s, d, 2, state)
				ar, _ := after.FindPathsState(s, d, 2, state)
				pair.BeforeAt = br
				pair.AfterAt = ar
			}
			switch {
			case !b && a:
				pair.Change = "now-reachable"
				pair.HopChanges = explainFlipState(before, after, s, d, state)
				rep.NewReachable = append(rep.NewReachable, pair)
			case b && !a:
				pair.Change = "now-blocked"
				pair.HopChanges = explainFlipState(before, after, s, d, state)
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

// evalQuiet reports reachability without constructing explanations;
// unknown endpoints count as unreachable rather than errors, because
// diffs compare versions where nodes may be added or removed.
func evalQuiet(t *Topology, src, dst string) (bool, error) {
	return evalQuietState(t, src, dst, "")
}

func evalQuietState(t *Topology, src, dst, state string) (bool, error) {
	r, err := t.FindPathsState(src, dst, 1, state)
	if err != nil {
		return false, nil
	}
	return r.Reachable, nil
}
