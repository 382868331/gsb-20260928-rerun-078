// Package topo implements the domain model and the reachability engine of
// the network topology design platform. It never talks to a real network:
// all computations happen against an in-memory, versioned topology snapshot.
package topo

import "time"

// Node is a device in the topology. ID is the stable identity of the node;
// save/undo/import must never rewrite it.
type Node struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Zone string `json:"zone"`
	Kind string `json:"kind"` // host, switch, router, firewall, ...
}

// Link is a directed edge From -> To. A bidirectional connection needs two
// links; direction is never inferred from a single link.
type Link struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

const (
	ActionPermit = "permit"
	ActionDeny   = "deny"
)

// Rule is one ordered policy entry. Rules are evaluated in the exact order
// stored in Topology.Rules: the first matching rule on a node wins
// (first-match-wins). Empty match fields are wildcards.
type Rule struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	NodeIDs    []string `json:"nodeIds"`  // nodes where this rule is installed; empty = every node
	SrcZones   []string `json:"srcZones"` // match source node zone
	DstZones   []string `json:"dstZones"` // match destination node zone
	SrcNodeIDs []string `json:"srcNodeIds"`
	DstNodeIDs []string `json:"dstNodeIds"`
	Protocols  []string `json:"protocols"` // tcp, udp, icmp, ...; empty = any
	Action     string   `json:"action"`    // permit | deny
	Disabled   bool     `json:"disabled"`  // kept in place but skipped during evaluation
}

// Topology is one immutable, versioned snapshot.
type Topology struct {
	Version   string         `json:"version"`
	Nodes     []Node         `json:"nodes"`
	Links     []Link         `json:"links"`
	Rules     []Rule         `json:"rules"`
	CreatedAt time.Time      `json:"createdAt"`
	Meta      map[string]any `json:"meta,omitempty"`
}

// Connection asks: can Src open a session of Protocol toward Dst?
type Connection struct {
	Src      string `json:"src"`
	Dst      string `json:"dst"`
	Protocol string `json:"protocol"`
}

// HopDecision records which rule actually fired on one node of a path.
type HopDecision struct {
	NodeID   string `json:"nodeId"`
	RuleID   string `json:"ruleId"` // empty when DefaultDeny fired
	Action   string `json:"action"`
	RuleName string `json:"ruleName,omitempty"`
}

// DefaultDenyAction is returned when no rule matches on a node.
const DefaultDenyRuleID = "default-deny"

// Path is one candidate route with its per-node rule trace.
type Path struct {
	Nodes     []string      `json:"nodes"`
	Feasible  bool          `json:"feasible"`
	Decisions []HopDecision `json:"decisions"`
	// BlockedAt is the first node that denied, -1 when the path is feasible.
	BlockedAt int `json:"blockedAt"`
}

// ReachResult explains why a connection can or cannot pass. It includes
// infeasible candidate paths so that "we are connected by a wire" is never
// mistaken for "traffic is allowed".
type ReachResult struct {
	Conn       Connection `json:"conn"`
	Reachable  bool       `json:"reachable"`
	ActivePath *Path      `json:"activePath,omitempty"`
	Paths      []Path     `json:"paths"`
	Reason     string     `json:"reason"`
}

// Change describes one ordered connection whose reachability flipped
// between two topology versions.
type Change struct {
	Conn         Connection `json:"conn"`
	Before       bool       `json:"before"`
	After        bool       `json:"after"`
	Kind         string     `json:"kind"` // added | blocked
	BeforeReason string     `json:"beforeReason,omitempty"`
	AfterReason  string     `json:"afterReason,omitempty"`
}

// DiffResult is the policy-change impact report between two versions.
type DiffResult struct {
	FromVersion string   `json:"fromVersion"`
	ToVersion   string   `json:"toVersion"`
	Added       []Change `json:"added"`
	Blocked     []Change `json:"blocked"`
	Unchanged   int      `json:"unchanged"`
}
