package topo

import "fmt"

// Normalize fills defaults, validates references and guarantees that
// node IDs survive any round trip. It never reassigns IDs.
func (t *Topology) Normalize() error {
	if t.Policy.DefaultAction == "" {
		t.Policy.DefaultAction = ActionDeny
	}
	if t.Policy.DefaultAction != ActionAllow && t.Policy.DefaultAction != ActionDeny {
		return fmt.Errorf("invalid default action %q", t.Policy.DefaultAction)
	}
	seen := map[string]bool{}
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.ID == "" {
			return fmt.Errorf("node missing id")
		}
		if seen[n.ID] {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
		if n.Name == "" {
			n.Name = n.ID
		}
	}
	linkSeen := map[string]bool{}
	for _, l := range t.Links {
		if l.ID == "" {
			return fmt.Errorf("link missing id")
		}
		if linkSeen[l.ID] {
			return fmt.Errorf("duplicate link id %q", l.ID)
		}
		linkSeen[l.ID] = true
		if !seen[l.Source] {
			return fmt.Errorf("link %q references missing source %q", l.ID, l.Source)
		}
		if !seen[l.Target] {
			return fmt.Errorf("link %q references missing target %q", l.ID, l.Target)
		}
		if l.Kind != LinkDirected && l.Kind != LinkUndirected {
			return fmt.Errorf("link %q invalid kind %q", l.ID, l.Kind)
		}
	}
	ruleSeen := map[string]bool{}
	for i := range t.Policy.Rules {
		r := &t.Policy.Rules[i]
		if r.ID == "" {
			return fmt.Errorf("rule %d missing id", i)
		}
		if ruleSeen[r.ID] {
			return fmt.Errorf("duplicate rule id %q", r.ID)
		}
		ruleSeen[r.ID] = true
		if r.Action != ActionAllow && r.Action != ActionDeny {
			return fmt.Errorf("rule %q invalid action %q", r.ID, r.Action)
		}
		if r.ConnState != "" && r.ConnState != ConnNew && r.ConnState != ConnEstablished {
			return fmt.Errorf("rule %q invalid connState %q", r.ID, r.ConnState)
		}
		if r.LinkID != "" && !linkSeen[r.LinkID] {
			return fmt.Errorf("rule %q references missing link %q", r.ID, r.LinkID)
		}
	}
	return nil
}
