package topo

import (
	"fmt"
	"sort"
	"strings"
)

// conn.go: connection-state-aware traffic checks.
//
// A plain reachability check answers "can a packet pass". A
// connection check answers the lifecycle question: can the first
// request establish a connection (state=new), does keep-alive traffic
// of an established connection still pass (state=established), and
// does the return traffic of that connection get back (state=return,
// evaluated dst -> src). A one-way allow never implies the return
// path: asymmetric results are reported explicitly.

// ConnOptions simulates failure/timeout conditions for a what-if
// trial. Trials are pure computations: nothing here mutates the
// topology, the policy, or any saved version.
type ConnOptions struct {
	DownNodes []string     `json:"downNodes,omitempty"` // simulated disconnected middle nodes
	DownLinks []string     `json:"downLinks,omitempty"` // simulated disconnected links
	Timeout   bool         `json:"timeout,omitempty"`   // simulate idle-timeout of the established connection
	Packet    *Packet      `json:"packet,omitempty"`
	Bindings  []NATBinding `json:"bindings,omitempty"`
}

// ConnPhase explains one lifecycle stage with its own hop-level trace.
type ConnPhase struct {
	Phase  string      `json:"phase"` // establish | maintain | return | teardown
	OK     bool        `json:"ok"`
	Reason string      `json:"reason"` // machine readable
	Detail string      `json:"detail"` // human readable basis for the verdict
	Path   *PathResult `json:"path,omitempty"`
}

// ConnResult is the full lifecycle verdict for one ordered pair.
type ConnResult struct {
	Source        string       `json:"source"`
	Target        string       `json:"target"`
	State         string       `json:"state"` // established | expired | broken | blocked
	Establishable bool         `json:"establishable"`
	Bidirectional bool         `json:"bidirectional"` // forward AND return both pass
	Asymmetric    bool         `json:"asymmetric"`    // forward passes, return does not
	Forward       *PathResult  `json:"forward"`       // state=new, src -> dst
	Maintain      *PathResult  `json:"maintain"`      // state=established, src -> dst
	Return        *PathResult  `json:"return"`        // state=return, dst -> src
	Phases        []ConnPhase  `json:"phases"`
	Mappings      []NATBinding `json:"mappings,omitempty"`
	Reason        string       `json:"reason"`
}

// ConnCheck evaluates the connection lifecycle for src -> dst under
// the simulated conditions in opts. Down nodes/links are removed from
// a copy of the topology; the original is untouched.
func (t *Topology) ConnCheck(src, dst string, k int, opts ConnOptions) (*ConnResult, error) {
	if k <= 0 {
		k = 3
	}
	now := t.filteredCopy(opts.DownNodes, opts.DownLinks)

	res := &ConnResult{Source: src, Target: dst, Phases: []ConnPhase{}}

	var fwdPkt Packet
	var bindings []NATBinding
	if opts.Packet != nil {
		fwdPkt = *opts.Packet
		if fwdPkt.ConnectionID == "" {
			fwdPkt.ConnectionID = src + "->" + dst + ":" + fwdPkt.Source.String() + "->" + fwdPkt.Destination.String()
		}
		bindings = append([]NATBinding{}, opts.Bindings...)
	}
	var fwd *PathResult
	var err error
	if opts.Packet != nil {
		fwd, err = now.FindPathsStatePacket(src, dst, k, StateNew, fwdPkt, bindings)
	} else {
		fwd, err = now.FindPathsState(src, dst, k, StateNew)
	}
	if err != nil {
		return nil, err
	}
	establishedBindings := bindingsForResult(fwd, bindings)
	res.Mappings = append([]NATBinding{}, establishedBindings...)
	maintainBindings := establishedBindings
	if opts.Timeout {
		maintainBindings = expireBindings(establishedBindings)
	}
	var maintain *PathResult
	if opts.Packet != nil {
		maintain, err = now.FindPathsStatePacket(src, dst, k, StateEstablished, fwdPkt, maintainBindings)
	} else {
		maintain, err = now.FindPathsState(src, dst, k, StateEstablished)
	}
	if err != nil {
		return nil, err
	}
	var ret *PathResult
	if opts.Packet != nil {
		retPkt := returnPacket(fwd, fwdPkt)
		ret, err = now.FindPathsStatePacket(dst, src, k, StateReturn, retPkt, maintainBindings)
	} else {
		ret, err = now.FindPathsState(dst, src, k, StateReturn)
	}
	if err != nil {
		return nil, err
	}
	res.Forward, res.Maintain, res.Return = fwd, maintain, ret
	res.Establishable = fwd.Reachable
	res.Bidirectional = fwd.Reachable && ret.Reachable
	res.Asymmetric = fwd.Reachable && !ret.Reachable

	res.Phases = append(res.Phases, ConnPhase{
		Phase: "establish", OK: fwd.Reachable,
		Reason: pickReason(fwd, "first-request-allowed"),
		Detail: fmt.Sprintf("首次请求按新连接（state=new）逐跳评估：%s", zhVerdict(fwd)),
		Path:   fwd,
	})

	switch {
	case opts.Timeout:
		res.Phases = append(res.Phases, ConnPhase{
			Phase: "maintain", OK: false,
			Reason: "idle-timeout",
			Detail: "连接空闲超时已失效：保持阶段不再成立，后续流量须按新连接（state=new）重新建立",
			Path:   maintain,
		})
	case res.Establishable:
		res.Phases = append(res.Phases, ConnPhase{
			Phase: "maintain", OK: maintain.Reachable,
			Reason: pickReason(maintain, "keepalive-allowed"),
			Detail: fmt.Sprintf("已建立连接的保持流量（state=established）逐跳评估：%s", zhVerdict(maintain)),
			Path:   maintain,
		})
	default:
		res.Phases = append(res.Phases, ConnPhase{
			Phase: "maintain", OK: false,
			Reason: "never-established",
			Detail: "连接未能建立，不存在保持阶段",
			Path:   maintain,
		})
	}

	retDetail := fmt.Sprintf("回程流量（state=return，%s → %s）逐跳评估：%s", dst, src, zhVerdict(ret))
	if res.Asymmetric {
		retDetail += "。去程放行但回程不通：非对称返回路径，不能凭借一次放行认作全程可达"
	}
	res.Phases = append(res.Phases, ConnPhase{
		Phase: "return", OK: ret.Reachable,
		Reason: pickReason(ret, "return-allowed"),
		Detail: retDetail,
		Path:   ret,
	})

	res.Phases = append(res.Phases, t.teardownPhase(src, dst, opts, establishedBindings))

	if opts.Packet != nil {
		res.annotateNATPhases(establishedBindings)
	}

	res.State, res.Reason = res.connState(opts)
	return res, nil
}

// teardownPhase explains why an established connection would break:
// idle timeout, or a middle node/link of its pinned path going down.
// The pinned path is taken from the unfiltered topology (the path the
// connection was established on), so a reroute that helps new
// connections does not mask the break of the existing one.
func (t *Topology) teardownPhase(src, dst string, opts ConnOptions, mappings []NATBinding) ConnPhase {
	ph := ConnPhase{Phase: "teardown", OK: true, Reason: "still-up", Detail: "无超时或断开条件，连接保持"}
	var causes []string
	if opts.Timeout {
		causes = append(causes, "连接空闲超时")
	}
	if len(opts.DownNodes) > 0 || len(opts.DownLinks) > 0 {
		for _, b := range mappings {
			for _, n := range opts.DownNodes {
				if b.Device == n {
					causes = append(causes, fmt.Sprintf("连接映射设备 %s 断开（规则 %s，映射 %s→%s）", b.Device, b.RuleID, b.Original, b.Translated))
				}
			}
		}
		pinned, err := t.FindPathsState(src, dst, 1, StateEstablished)
		if err == nil && len(pinned.Paths) > 0 {
			p := pinned.Paths[0]
			downN := map[string]bool{}
			for _, n := range opts.DownNodes {
				downN[n] = true
			}
			downL := map[string]bool{}
			for _, l := range opts.DownLinks {
				downL[l] = true
			}
			for _, n := range p.NodeIDs {
				if downN[n] {
					causes = append(causes, fmt.Sprintf("路径上的节点 %s 断开", n))
				}
			}
			for _, h := range p.Hops {
				if downL[h.LinkID] {
					causes = append(causes, fmt.Sprintf("路径上的链路 %s（%s→%s）断开", h.LinkID, h.From, h.To))
				}
			}
		}
	}
	if len(causes) > 0 {
		ph.OK = false
		ph.Reason = "idle-timeout"
		for _, c := range causes {
			if !strings.Contains(c, "超时") {
				ph.Reason = "broken"
			}
		}
		ph.Detail = "连接失效依据：" + joinZH(causes)
	}
	return ph
}

func (r *ConnResult) connState(opts ConnOptions) (string, string) {
	for _, ph := range r.Phases {
		if ph.Phase == "teardown" && !ph.OK {
			if ph.Reason == "broken" {
				return "broken", "path-element-down"
			}
		}
	}
	if opts.Timeout {
		return "expired", "idle-timeout"
	}
	if !r.Establishable {
		return "blocked", "first-request-denied"
	}
	if r.Asymmetric {
		return "established", "asymmetric-return"
	}
	return "established", "established"
}

// filteredCopy returns a copy of the topology without the given
// nodes/links. The receiver is never modified.
func (t *Topology) filteredCopy(downNodes, downLinks []string) *Topology {
	dn := map[string]bool{}
	for _, n := range downNodes {
		dn[n] = true
	}
	dl := map[string]bool{}
	for _, l := range downLinks {
		dl[l] = true
	}
	out := &Topology{Version: t.Version, Policy: t.Policy, NATRules: t.NATRules}
	for _, n := range t.Nodes {
		if !dn[n.ID] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, l := range t.Links {
		if dl[l.ID] || dn[l.Source] || dn[l.Target] {
			continue
		}
		out.Links = append(out.Links, l)
	}
	return out
}

func pickReason(r *PathResult, ok string) string {
	if r.Reachable {
		return ok
	}
	return r.Reason
}

func zhVerdict(r *PathResult) string {
	switch {
	case r.Reachable:
		return "放行"
	case r.Reason == "no-route":
		return "无路由"
	default:
		return "被规则拒绝"
	}
}

func joinZH(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "；"
		}
		out += p
	}
	return out
}

func bindingsForResult(r *PathResult, supplied []NATBinding) []NATBinding {
	if r == nil || !r.Reachable || len(r.Paths) == 0 {
		return append([]NATBinding{}, supplied...)
	}
	return append([]NATBinding{}, r.Paths[0].Mappings...)
}

func expireBindings(in []NATBinding) []NATBinding {
	out := make([]NATBinding, len(in))
	for i, b := range in {
		b.State = bindingExpired
		out[i] = b
	}
	return out
}

func returnPacket(fwd *PathResult, initial Packet) Packet {
	pkt := Packet{ConnectionID: initial.ConnectionID, Protocol: initial.Protocol}
	if fwd != nil && fwd.Reachable && len(fwd.Paths) > 0 && len(fwd.Paths[0].Hops) > 0 {
		last := fwd.Paths[0].Hops[len(fwd.Paths[0].Hops)-1].Flow
		if last != nil {
			pkt.Source = last.Destination
			pkt.Destination = last.Source
			return pkt
		}
	}
	pkt.Source = initial.Destination
	pkt.Destination = initial.Source
	return pkt
}

func (r *ConnResult) annotateNATPhases(bindings []NATBinding) {
	for i := range r.Phases {
		switch r.Phases[i].Phase {
		case "maintain":
			if len(bindings) > 0 {
				if r.Phases[i].OK {
					r.Phases[i].Detail += "。已有连接沿用建立时记录的转换规则和地址范围，不重新选择新连接的映射"
				} else {
					r.Phases[i].Detail += "。原映射与新连接的可用映射分开判断"
				}
			}
		case "return":
			if len(bindings) > 0 {
				r.Phases[i].Detail += "。回程只能按连接 ID 反查建立时映射，不凭转换端口猜测当前占用者"
			}
		}
	}
}

// Connection is an established connection the user registered while
// checking; its pinned path is the route it was established on.
type Connection struct {
	ID          string       `json:"id"`
	Source      string       `json:"source"`
	Target      string       `json:"target"`
	PathNodeIDs []string     `json:"pathNodeIds,omitempty"`
	Mappings    []NATBinding `json:"mappings,omitempty"`
	Packet      *Packet      `json:"packet,omitempty"`
	State       string       `json:"state"` // established
}

// ConnImpact explains what a policy/route change does to one existing
// connection, separately from what it does to new connections.
type ConnImpact struct {
	ConnID       string       `json:"connId"`
	Source       string       `json:"source"`
	Target       string       `json:"target"`
	BeforeStatus string       `json:"beforeStatus"` // established
	AfterStatus  string       `json:"afterStatus"`  // survives | broken-path | broken-policy
	Detail       string       `json:"detail"`
	AfterHops    []Hop        `json:"afterHops"`
	Mappings     []NATBinding `json:"mappings,omitempty"`
}

// ConnDiffReport separates the impact of a change: NewConns covers
// first-request (state=new) reachability for every ordered pair;
// Existing covers each registered established connection on its
// pinned path.
type ConnDiffReport struct {
	NewConns *DiffReport  `json:"newConns"`
	Existing []ConnImpact `json:"existing"`
}

// CompareConn evaluates a policy/route change for new connections
// (state=new over all ordered pairs) and for the given existing
// connections (state=established on their pinned paths).
func CompareConn(before, after *Topology, conns []Connection, detailed bool) (*ConnDiffReport, error) {
	newRep, err := ComparePolicyState(before, after, detailed, StateNew)
	if err != nil {
		return nil, err
	}
	rep := &ConnDiffReport{NewConns: newRep, Existing: []ConnImpact{}}
	aEng, err := BuildEngine(after)
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		rep.Existing = append(rep.Existing, connImpact(before, after, aEng, c))
	}
	return rep, nil
}

func connImpact(before, after *Topology, aEng *engine, c Connection) ConnImpact {
	imp := ConnImpact{
		ConnID: c.ID, Source: c.Source, Target: c.Target,
		BeforeStatus: StateEstablished,
		AfterHops:    []Hop{},
	}
	path := c.PathNodeIDs
	if len(path) < 2 {
		// No pinned path recorded: derive the route the connection
		// would have been established on under the before topology.
		if res, err := before.FindPathsState(c.Source, c.Target, 1, StateEstablished); err == nil && len(res.Paths) > 0 {
			path = res.Paths[0].NodeIDs
		}
	}
	if len(path) < 2 {
		imp.AfterStatus = "broken-path"
		imp.Detail = "改动前已无可用路径，连接无法定位原路径"
		return imp
	}
	if c.Packet != nil && len(c.Mappings) > 0 {
		return connImpactPacket(before, after, aEng, c, path)
	}
	for i := 0; i+1 < len(path); i++ {
		u, v := path[i], path[i+1]
		linkID, reverse, ok := aEng.edgeBetween(u, v)
		if !ok {
			imp.AfterStatus = "broken-path"
			imp.Detail = fmt.Sprintf("原路径第 %d 段 %s→%s 在改动后的拓扑中不存在（节点/链路被删或方向改变），连接断开", i+1, u, v)
			return imp
		}
		d := after.DecideHopState(u, v, linkID, StateEstablished)
		action := after.Policy.DefaultAction
		if d.RuleID != "" {
			if d.Allowed {
				action = ActionAllow
			} else {
				action = ActionDeny
			}
		}
		imp.AfterHops = append(imp.AfterHops, Hop{
			From: u, To: v, LinkID: linkID, Reverse: reverse,
			RuleID: d.RuleID, RuleOrder: d.RuleOrder, Action: action,
		})
		if !d.Allowed {
			imp.AfterStatus = "broken-policy"
			if d.RuleID != "" {
				imp.Detail = fmt.Sprintf("改动后第 %d 段 %s→%s 被规则 %s（顺序 %d）拒绝（state=established），既有连接断开", i+1, u, v, d.RuleID, d.RuleOrder)
			} else {
				imp.Detail = fmt.Sprintf("改动后第 %d 段 %s→%s 无规则命中，按默认动作拒绝（state=established），既有连接断开", i+1, u, v)
			}
			return imp
		}
	}
	imp.AfterStatus = "survives"
	imp.Detail = "原路径各段在改动后仍放行（state=established），既有连接保持"
	return imp
}

func connImpactPacket(before, after *Topology, aEng *engine, c Connection, path []string) ConnImpact {
	imp := ConnImpact{ConnID: c.ID, Source: c.Source, Target: c.Target, BeforeStatus: StateEstablished, AfterHops: []Hop{}, Mappings: append([]NATBinding{}, c.Mappings...)}
	pkt := *c.Packet
	for i := 0; i+1 < len(path); i++ {
		u, v := path[i], path[i+1]
		linkID, reverse, ok := aEng.edgeBetween(u, v)
		if !ok {
			imp.AfterStatus = "broken-path"
			imp.Detail = fmt.Sprintf("原路径第 %d 段 %s→%s 在改动后的拓扑中不存在，连接断开", i+1, u, v)
			return imp
		}
		hopFlow := pkt
		nat := after.applyForwardBinding(u, linkID, StateEstablished, &hopFlow, c.Mappings, map[string]NATBinding{})
		d := after.DecideHopStateFlow(u, v, linkID, StateEstablished, hopFlow)
		hop := Hop{From: u, To: v, LinkID: linkID, Reverse: reverse, RuleID: d.RuleID, RuleOrder: d.RuleOrder, Action: ActionAllow, Flow: &hopFlow, NAT: nat}
		if d.RuleID == "" && after.Policy.DefaultAction == ActionDeny {
			hop.Action = ActionDeny
		} else if d.RuleID != "" && !d.Allowed {
			hop.Action = ActionDeny
		}
		if nat != nil && nat.Decision == "denied" {
			hop.Action = ActionDeny
		}
		imp.AfterHops = append(imp.AfterHops, hop)
		if hop.Action == ActionDeny {
			imp.AfterStatus = "broken-policy"
			if nat != nil && nat.Decision == "denied" {
				imp.Detail = fmt.Sprintf("改动后第 %d 段的连接追踪拒绝：%s", i+1, nat.Detail)
			} else if d.RuleID != "" {
				imp.Detail = fmt.Sprintf("改动后第 %d 段按转换后地址命中规则 %s（顺序 %d）被拒绝", i+1, d.RuleID, d.RuleOrder)
			} else {
				imp.Detail = fmt.Sprintf("改动后第 %d 段按转换后地址无规则命中，默认拒绝", i+1)
			}
			return imp
		}
		pkt = hopFlow
	}
	imp.AfterStatus = "survives"
	imp.Detail = "原路径、原连接映射及 state=established 策略均继续成立；新连接若转换范围改变会另行选择，不影响该既有连接"
	return imp
}

// SortConnImpacts keeps a stable rendering order for saved reports.
func SortConnImpacts(imps []ConnImpact) {
	sort.SliceStable(imps, func(i, j int) bool { return imps[i].ConnID < imps[j].ConnID })
}
