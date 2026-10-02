package topo

import "fmt"

// LinkKind controls how a link may be traversed.
type LinkKind string

const (
	LinkDirected   LinkKind = "directed"   // source -> target only
	LinkUndirected LinkKind = "undirected" // traversable in both directions
)

type Node struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Kind  string            `json:"kind,omitempty"` // host, switch, router, firewall ...
	X     int               `json:"x"`
	Y     int               `json:"y"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

type Link struct {
	ID     string            `json:"id"`
	Source string            `json:"source"`
	Target string            `json:"target"`
	Kind   LinkKind          `json:"kind"`
	Attrs  map[string]string `json:"attrs,omitempty"`
}

type RuleAction string

const (
	ActionAllow RuleAction = "allow"
	ActionDeny  RuleAction = "deny"
)

// Connection traffic states. A rule may restrict itself to one state;
// an empty ConnState matches every state. "new" is the first request
// of a connection, "established" is subsequent/return traffic of a
// connection that already exists.
const (
	ConnNew         = "new"
	ConnEstablished = "established"
)

// Rule is a single ordered policy entry.
// A rule matches a hop when every non-empty matcher equals the value
// present on the hop or its endpoints. Rules are evaluated in Order
// ascending; the first matching rule decides. If none matches the
// policy default applies (deny unless changed).
type Rule struct {
	ID            string     `json:"id"`
	Order         int        `json:"order"`
	Enabled       bool       `json:"enabled"`
	Action        RuleAction `json:"action"`
	SrcNode       string     `json:"srcNode,omitempty"`
	DstNode       string     `json:"dstNode,omitempty"`
	SrcKind       string     `json:"srcKind,omitempty"`
	DstKind       string     `json:"dstKind,omitempty"`
	LinkID        string     `json:"linkId,omitempty"`
	LinkAttrName  string     `json:"linkAttrName,omitempty"`
	LinkAttrValue string     `json:"linkAttrValue,omitempty"`
	SrcAttrName   string     `json:"srcAttrName,omitempty"`
	SrcAttrValue  string     `json:"srcAttrValue,omitempty"`
	DstAttrName   string     `json:"dstAttrName,omitempty"`
	DstAttrValue  string     `json:"dstAttrValue,omitempty"`
	Protocol      string     `json:"protocol,omitempty"` // carried on link attrs, matched when set
	ConnState     string     `json:"connState,omitempty"` // "" | new | established
	Note          string     `json:"note,omitempty"`
}

type Policy struct {
	DefaultAction RuleAction `json:"defaultAction"` // applied when no rule matches
	Rules         []Rule     `json:"rules"`
}

type Topology struct {
	Version int    `json:"version"` // incremented on every persisted save
	Nodes   []Node `json:"nodes"`
	Links   []Link `json:"links"`
	Policy  Policy `json:"policy"`
}

// Hop describes one directed traversal in an evaluated path.
type Hop struct {
	From      string     `json:"from"`
	To        string     `json:"to"`
	LinkID    string     `json:"linkId"`
	Reverse   bool       `json:"reverse"` // undirected link walked target -> source
	RuleID    string     `json:"ruleId"`
	RuleOrder int        `json:"ruleOrder"`
	Action    RuleAction `json:"action"`
}

type Path struct {
	Hops      []Hop    `json:"hops"`
	NodeIDs   []string `json:"nodeIds"`
	Reachable bool     `json:"reachable"`
	Length    int      `json:"length"`
}

// HopDecision is the outcome of evaluating the ordered rule list for one hop.
type HopDecision struct {
	Allowed   bool
	RuleID    string
	RuleOrder int
}

type engine struct {
	nodes map[string]*Node
	// outgoing adjacency: nodeID -> list of (linkID, neighbor, reverse)
	out map[string][][3]string
}

func BuildEngine(t *Topology) (*engine, error) {
	e := &engine{nodes: map[string]*Node{}, out: map[string][][3]string{}}
	for i := range t.Nodes {
		n := &t.Nodes[i]
		if n.ID == "" {
			return nil, fmt.Errorf("node with empty id")
		}
		if _, dup := e.nodes[n.ID]; dup {
			return nil, fmt.Errorf("duplicate node id %q", n.ID)
		}
		e.nodes[n.ID] = n
	}
	for i := range t.Links {
		l := &t.Links[i]
		if l.ID == "" {
			return nil, fmt.Errorf("link with empty id")
		}
		if _, ok := e.nodes[l.Source]; !ok {
			return nil, fmt.Errorf("link %q source %q missing", l.ID, l.Source)
		}
		if _, ok := e.nodes[l.Target]; !ok {
			return nil, fmt.Errorf("link %q target %q missing", l.ID, l.Target)
		}
		switch l.Kind {
		case LinkDirected:
			e.out[l.Source] = append(e.out[l.Source], [3]string{l.ID, l.Target, ""})
		case LinkUndirected:
			e.out[l.Source] = append(e.out[l.Source], [3]string{l.ID, l.Target, ""})
			e.out[l.Target] = append(e.out[l.Target], [3]string{l.ID, l.Source, "1"})
		default:
			return nil, fmt.Errorf("link %q has invalid kind %q", l.ID, l.Kind)
		}
	}
	return e, nil
}

func (t *Topology) linkByID(id string) *Link {
	for i := range t.Links {
		if t.Links[i].ID == id {
			return &t.Links[i]
		}
	}
	return nil
}

func attrEqual(m map[string]string, name, want string) bool {
	if name == "" {
		return true
	}
	return m[name] == want
}

// DecideHop evaluates the ordered rule list for a directed hop of a
// new connection's first request.
func (t *Topology) DecideHop(from, to, linkID string) HopDecision {
	return t.DecideHopState(from, to, linkID, ConnNew)
}

// DecideHopState evaluates the ordered rule list for a directed hop
// carrying traffic in the given connection state. A rule with an empty
// ConnState matches every state; otherwise the rule only matches when
// its ConnState equals the traffic state, so first requests and the
// return traffic of established connections can follow different rules.
//
// Order is significant: enabled rules are sorted by Order (stable on
// insertion position) and the first match wins. With no match the
// policy default action applies.
func (t *Topology) DecideHopState(from, to, linkID, state string) HopDecision {
	fromNode := t.nodeByID(from)
	toNode := t.nodeByID(to)
	link := t.linkByID(linkID)

	var order []int
	for i := range t.Policy.Rules {
		order = append(order, i)
	}
	// insertion sort by Order, keeping insertion order on ties
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && t.Policy.Rules[order[j]].Order < t.Policy.Rules[order[j-1]].Order; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	for _, i := range order {
		r := t.Policy.Rules[i]
		if !r.Enabled {
			continue
		}
		if r.ConnState != "" && r.ConnState != state {
			continue
		}
		if r.SrcNode != "" && r.SrcNode != from {
			continue
		}
		if r.DstNode != "" && r.DstNode != to {
			continue
		}
		if r.SrcKind != "" && (fromNode == nil || fromNode.Kind != r.SrcKind) {
			continue
		}
		if r.DstKind != "" && (toNode == nil || toNode.Kind != r.DstKind) {
			continue
		}
		if r.LinkID != "" && r.LinkID != linkID {
			continue
		}
		if !attrEqual(linkAttrs(link), r.LinkAttrName, r.LinkAttrValue) {
			continue
		}
		if !attrEqual(nodeAttrs(fromNode), r.SrcAttrName, r.SrcAttrValue) {
			continue
		}
		if !attrEqual(nodeAttrs(toNode), r.DstAttrName, r.DstAttrValue) {
			continue
		}
		if r.Protocol != "" {
			var proto string
			if link != nil {
				proto = link.Attrs["protocol"]
			}
			if proto != r.Protocol {
				continue
			}
		}
		return HopDecision{Allowed: r.Action == ActionAllow, RuleID: r.ID, RuleOrder: r.Order}
	}
	return HopDecision{Allowed: t.Policy.DefaultAction == ActionAllow}
}

func nodeAttrs(n *Node) map[string]string {
	if n == nil {
		return nil
	}
	return n.Attrs
}

func linkAttrs(l *Link) map[string]string {
	if l == nil {
		return nil
	}
	return l.Attrs
}

func (t *Topology) nodeByID(id string) *Node {
	for i := range t.Nodes {
		if t.Nodes[i].ID == id {
			return &t.Nodes[i]
		}
	}
	return nil
}
