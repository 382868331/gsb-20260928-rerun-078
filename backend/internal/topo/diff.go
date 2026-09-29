package topo

import "sort"

// PairResult is the reachability explanation for one ordered
// (source, target) pair under a given topology.
type PairResult struct {
	Source   string      `json:"source"`
	Target   string      `json:"target"`
	Before   bool        `json:"before"`
	After    bool        `json:"after"`
	Change   string      `json:"change"` // none | now-reachable | now-blocked
	BeforeAt *PathResult `json:"beforeAt,omitempty"`
	AfterAt  *PathResult `json:"afterAt,omitempty"`
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
	rep := &DiffReport{BeforeVersion: before.Version, AfterVersion: after.Version}
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
			b, _ := evalQuiet(before, s, d)
			a, _ := evalQuiet(after, s, d)
			pair := PairResult{Source: s, Target: d, Before: b, After: a}
			if detailed {
				br, _ := before.FindPaths(s, d, 2)
				ar, _ := after.FindPaths(s, d, 2)
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

// evalQuiet reports reachability without constructing explanations;
// unknown endpoints count as unreachable rather than errors, because
// diffs compare versions where nodes may be added or removed.
func evalQuiet(t *Topology, src, dst string) (bool, error) {
	r, err := t.FindPaths(src, dst, 1)
	if err != nil {
		return false, nil
	}
	return r.Reachable, nil
}
