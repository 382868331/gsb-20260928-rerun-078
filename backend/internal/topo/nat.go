package topo

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

const (
	bindingEstablished = "established"
	bindingExpired     = "expired"
)

// FindPathsStatePacket evaluates a concrete packet and its current
// connection state. Policy rules see the addresses present after any
// translation on that hop, and each hop records both the rule and the
// translation decision.
func (t *Topology) FindPathsStatePacket(src, dst string, k int, state string, pkt Packet, bindings []NATBinding) (*PathResult, error) {
	res := &PathResult{Source: src, Target: dst, Paths: []Path{}, Blocked: []Path{}}
	eng, err := BuildEngine(t)
	if err != nil {
		return nil, err
	}
	if _, ok := eng.nodes[src]; !ok {
		return nil, fmt.Errorf("unknown source node %q", src)
	}
	if _, ok := eng.nodes[dst]; !ok {
		return nil, fmt.Errorf("unknown target node %q", dst)
	}
	if src == dst {
		res.Reachable = true
		res.Reason = "same-node"
		res.Paths = []Path{{Hops: []Hop{}, NodeIDs: []string{src}, Reachable: true, Length: 0}}
		return res, nil
	}
	if k <= 0 {
		k = 3
	}
	for _, cand := range yen(eng, src, dst, k*4) {
		p := t.evaluatePathPacket(eng, cand, state, pkt, bindings)
		if state == StateReturn {
			p.Detail = requireReturnBindings(&p, pkt.ConnectionID, bindings)
		}
		if p.Reachable {
			res.Paths = append(res.Paths, p)
			if len(res.Paths) >= k {
				break
			}
		} else if len(res.Blocked) < k {
			res.Blocked = append(res.Blocked, p)
		}
	}
	res.Reachable = len(res.Paths) > 0
	switch {
	case res.Reachable:
		res.Reason = "allowed"
	case len(res.Blocked) > 0:
		res.Reason = "all-paths-denied"
	default:
		res.Reason = "no-route"
	}
	return res, nil
}

func (t *Topology) evaluatePathPacket(eng *engine, nodes []string, state string, initial Packet, external []NATBinding) Path {
	p := Path{Hops: []Hop{}, NodeIDs: nodes, Reachable: len(nodes) >= 1, Mappings: []NATBinding{}}
	pkt := initial
	created := map[string]NATBinding{}
	all := append(append([]NATBinding{}, external...), []NATBinding{}...)
	for i := 0; i+1 < len(nodes); i++ {
		u, v := nodes[i], nodes[i+1]
		linkID, reverse, ok := eng.edgeBetween(u, v)
		if !ok {
			p.Reachable = false
			break
		}
		hopFlow := pkt
		var nat *NATDecision
		if state == StateReturn {
			nat = t.applyReturnBinding(u, &hopFlow, pkt.ConnectionID, all, created)
		} else {
			nat = t.applyForwardBinding(u, linkID, state, &hopFlow, all, created)
		}
		d := t.DecideHopStateFlow(u, v, linkID, state, hopFlow)
		action := t.Policy.DefaultAction
		if d.RuleID != "" {
			if d.Allowed {
				action = ActionAllow
			} else {
				action = ActionDeny
			}
		}
		hop := Hop{From: u, To: v, LinkID: linkID, Reverse: reverse, RuleID: d.RuleID, RuleOrder: d.RuleOrder, Action: action, Flow: &hopFlow, NAT: nat}
		if nat != nil && nat.Decision == "denied" {
			hop.Action = ActionDeny
			d.Allowed = false
		}
		if !decisionAllows(t, hop) {
			p.Reachable = false
		}
		p.Hops = append(p.Hops, hop)
		if nat != nil && nat.Binding != nil {
			if _, ok := created[nat.Device]; !ok {
				created[nat.Device] = *nat.Binding
				p.Mappings = append(p.Mappings, *nat.Binding)
			}
		}
		pkt = hopFlow
	}
	p.Length = len(p.Hops)
	return p
}

func (t *Topology) applyForwardBinding(device, linkID, state string, pkt *Packet, external []NATBinding, created map[string]NATBinding) *NATDecision {
	if !t.isNATDevice(device) {
		return nil
	}
	if b, ok := created[device]; ok {
		return bindingDecision(device, "snat", "reused", pkt, b)
	}
	for _, b := range external {
		if b.Device != device || b.ConnectionID != pkt.ConnectionID {
			continue
		}
		if b.State == bindingExpired || state == StateNew {
			if b.State == bindingExpired {
				return denyBinding(device, "snat", b, "mapping-expired", "旧连接映射已超时，不能复用转换端口", pkt)
			}
			break
		}
		return bindingDecision(device, "snat", "reused", pkt, b)
	}
	if state == StateEstablished {
		return &NATDecision{Device: device, Action: "snat", Decision: "denied", Reason: "mapping-missing", Detail: "已有连接在该设备找不到原转换依据，不能临时借用其他连接的映射", Before: pkt.Source}
	}
	rule, ok := t.natRuleFor(device, linkID, pkt.Source.IP)
	if !ok {
		return &NATDecision{Device: device, Action: "snat", Decision: "denied", Reason: "no-nat-rule", Detail: "新连接没有匹配的地址转换规则，不能猜测端口映射", Before: pkt.Source}
	}
	port, err := t.allocatePort(rule, pkt, external, created)
	if err != nil {
		return &NATDecision{Device: device, RuleID: rule.ID, Action: "snat", Decision: "denied", Reason: "port-exhausted", Detail: err.Error(), Before: pkt.Source}
	}
	b := NATBinding{ConnectionID: pkt.ConnectionID, Device: device, RuleID: rule.ID, Protocol: pkt.Protocol, Original: pkt.Source, Translated: Address{IP: rule.TranslatedIP, Port: port}, Destination: pkt.Destination, State: bindingEstablished}
	pkt.Source = b.Translated
	return &NATDecision{Device: device, RuleID: rule.ID, Action: "snat", Before: b.Original, After: b.Translated, Decision: "created", Detail: fmt.Sprintf("规则 %s 建立新映射：%s → %s", rule.ID, b.Original, b.Translated), Binding: &b}
}

func (t *Topology) applyReturnBinding(device string, pkt *Packet, connID string, external []NATBinding, created map[string]NATBinding) *NATDecision {
	if !t.isNATDevice(device) {
		return nil
	}
	b, ok := created[device]
	if !ok {
		b, ok = findBinding(external, device, connID, pkt.Destination)
	}
	if !ok {
		return &NATDecision{Device: device, Action: "un-snat", Decision: "denied", Reason: "mapping-unconfirmed", Detail: "回程报文的目标端口无法确认属于该连接，不能放行到转换端口的当前占用者", After: pkt.Destination}
	}
	if b.State == bindingExpired {
		return denyBinding(device, "un-snat", b, "mapping-expired", "迟到回程命中已超时的旧连接映射；端口若已被新连接复用，也不能转给后来的连接", pkt)
	}
	before := pkt.Destination
	pkt.Destination = b.Original
	return &NATDecision{Device: device, RuleID: b.RuleID, Action: "un-snat", Before: before, After: b.Original, Decision: "confirmed", Detail: fmt.Sprintf("按连接 %s 的原映射反向还原：%s → %s", connID, before, b.Original), Binding: &b}
}

func bindingDecision(device, action, decision string, pkt *Packet, b NATBinding) *NATDecision {
	before := pkt.Source
	pkt.Source = b.Translated
	return &NATDecision{Device: device, RuleID: b.RuleID, Action: action, Before: before, After: b.Translated, Decision: decision, Detail: fmt.Sprintf("沿用连接 %s 建立时的原映射：%s → %s", b.ConnectionID, before, b.Translated), Binding: &b}
}

func denyBinding(device, action string, b NATBinding, reason, detail string, pkt *Packet) *NATDecision {
	return &NATDecision{Device: device, RuleID: b.RuleID, Action: action, Before: pkt.Source, Decision: "denied", Reason: reason, Detail: detail, Binding: &b}
}

func findBinding(bindings []NATBinding, device, connID string, translated Address) (NATBinding, bool) {
	for _, b := range bindings {
		if b.Device == device && b.ConnectionID == connID && b.Translated == translated {
			return b, true
		}
	}
	return NATBinding{}, false
}

func requireReturnBindings(p *Path, connID string, bindings []NATBinding) string {
	nodes := map[string]bool{}
	for _, id := range p.NodeIDs {
		nodes[id] = true
	}
	var missing []string
	for _, b := range bindings {
		if b.ConnectionID != connID || b.State == bindingExpired {
			continue
		}
		if !nodes[b.Device] {
			p.Reachable = false
			missing = append(missing, b.Device)
		}
	}
	if len(missing) > 0 {
		p.Reachable = false
		return "回程路径未经过原连接的转换设备 " + strings.Join(missing, "、") + "，不能凭借去程已放行认定该非对称回程有效"
	}
	return ""
}

func (t *Topology) isNATDevice(id string) bool {
	n := t.nodeByID(id)
	if n != nil && n.Kind == "nat" {
		return true
	}
	for _, r := range t.NATRules {
		if r.Device == id {
			return true
		}
	}
	return false
}

func (t *Topology) natRuleFor(device, linkID, sourceIP string) (NATRule, bool) {
	rules := append([]NATRule{}, t.NATRules...)
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].Order < rules[j].Order })
	for _, r := range rules {
		if !r.Enabled || r.Device != device {
			continue
		}
		if r.EgressLinkID != "" && r.EgressLinkID != linkID {
			continue
		}
		if r.MatchSourceIP != "" && !ipMatch(r.MatchSourceIP, sourceIP) {
			continue
		}
		return r, true
	}
	return NATRule{}, false
}

func (t *Topology) allocatePort(rule NATRule, pkt *Packet, external []NATBinding, created map[string]NATBinding) (int, error) {
	// Translation ports are occupied on the translated address itself:
	// when two gateway devices share the same external IP pool (e.g.
	// consecutive SNAT ranges), a port taken by one gateway is not
	// available to another host behind the other gateway either.
	usedShared := map[string]bool{}
	for _, b := range external {
		if b.State == bindingExpired {
			continue
		}
		if b.Translated.IP == rule.TranslatedIP {
			usedShared[b.Translated.IP+":"+strconv.Itoa(b.Translated.Port)] = true
		}
	}
	for _, b := range created {
		if b.Translated.IP == rule.TranslatedIP {
			usedShared[b.Translated.IP+":"+strconv.Itoa(b.Translated.Port)] = true
		}
	}
	portTaken := func(port int) bool {
		return usedShared[rule.TranslatedIP+":"+strconv.Itoa(port)]
	}
	if rule.PreservePort && pkt.Source.Port >= rule.PortStart && pkt.Source.Port <= rule.PortEnd && !portTaken(pkt.Source.Port) {
		return pkt.Source.Port, nil
	}
	for port := rule.PortStart; port <= rule.PortEnd; port++ {
		if !portTaken(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("转换端口范围 %d-%d 已被其他活动连接占满", rule.PortStart, rule.PortEnd)
}

func ipMatch(want, got string) bool {
	if want == got {
		return true
	}
	prefix, err := netip.ParsePrefix(want)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(got)
	if err != nil {
		return false
	}
	return prefix.Contains(ip)
}
