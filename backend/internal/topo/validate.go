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
		if r.LinkID != "" && !linkSeen[r.LinkID] {
			return fmt.Errorf("rule %q references missing link %q", r.ID, r.LinkID)
		}
	}
	natSeen := map[string]bool{}
	for i := range t.NATRules {
		r := &t.NATRules[i]
		if r.ID == "" {
			return fmt.Errorf("nat rule %d missing id", i)
		}
		if natSeen[r.ID] {
			return fmt.Errorf("duplicate nat rule id %q", r.ID)
		}
		natSeen[r.ID] = true
		if !seen[r.Device] {
			return fmt.Errorf("nat rule %q references missing device %q", r.ID, r.Device)
		}
		if r.EgressLinkID != "" && !linkSeen[r.EgressLinkID] {
			return fmt.Errorf("nat rule %q references missing link %q", r.ID, r.EgressLinkID)
		}
		if r.TranslatedIP == "" {
			return fmt.Errorf("nat rule %q missing translatedIp", r.ID)
		}
		if r.PortStart <= 0 || r.PortEnd < r.PortStart {
			return fmt.Errorf("nat rule %q has invalid port range", r.ID)
		}
	}
	return nil
}
