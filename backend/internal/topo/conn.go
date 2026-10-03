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
	DownNodes []string `json:"downNodes,omitempty"` // simulated disconnected middle nodes
	DownLinks []string `json:"downLinks,omitempty"` // simulated disconnected links
	Timeout   bool     `json:"timeout,omitempty"`   // simulate idle-timeout of the established connection
	// Address translation trial inputs. SrcPort/DstPort identify the
	// checked connection on the wire (defaults 49152 / 80). Existing
	// carries the registered established connections whose mappings
	// pre-populate the NAT tables; ExpiredConns flags which of them
	// have idle-timed out; NATRanges overrides device port pools to
	// trial a translation-range change. All of it is pure simulation.
	SrcPort      int               `json:"srcPort,omitempty"`
	DstPort      int               `json:"dstPort,omitempty"`
	Existing     []Connection      `json:"existing,omitempty"`
	ExpiredConns []string          `json:"expiredConns,omitempty"`
	NATRanges    map[string][2]int `json:"natRanges,omitempty"`
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
	Source        string      `json:"source"`
	Target        string      `json:"target"`
	State         string      `json:"state"` // established | expired | broken | blocked
	Establishable bool        `json:"establishable"`
	Bidirectional bool        `json:"bidirectional"` // forward AND return both pass
	Asymmetric    bool        `json:"asymmetric"`    // forward passes, return does not
	Forward       *PathResult `json:"forward"`       // state=new, src -> dst
	Maintain      *PathResult `json:"maintain"`      // state=established, src -> dst
	Return        *PathResult `json:"return"`        // state=return, dst -> src
	Phases        []ConnPhase `json:"phases"`
	Reason        string      `json:"reason"`
	// Address-translation trace: every NAT decision along the path
	// (forward allocation, return lookup, late-return attribution),
	// plus the mappings chosen for the checked connection and the
	// existing connections' mappings that were left untouched.
	NATEvents    []NATHopEvent `json:"natEvents"`
	NewMappings  []NATMapping  `json:"newMappings"`
	KeptMappings []NATMapping  `json:"keptMappings"`
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

	fwd, err := now.FindPathsState(src, dst, k, StateNew)
	if err != nil {
		return nil, err
	}
	maintain, err := now.FindPathsState(src, dst, k, StateEstablished)
	if err != nil {
		return nil, err
	}
	ret, err := now.FindPathsState(dst, src, k, StateReturn)
	if err != nil {
		return nil, err
	}
	res.Forward, res.Maintain, res.Return = fwd, maintain, ret
	res.Establishable = fwd.Reachable
	res.Bidirectional = fwd.Reachable && ret.Reachable
	res.Asymmetric = fwd.Reachable && !ret.Reachable

	// Address translation layer: seed the NAT tables from the
	// registered existing connections, allocate a deterministic
	// mapping for the checked connection along its forward path, and
	// require the return traffic to confirm one specific mapping per
	// device. Everything happens on the filtered copy; the draft and
	// saved versions are untouched.
	nat := newNATSim(now, opts)
	srcPort, dstPort := opts.SrcPort, opts.DstPort
	if srcPort == 0 {
		srcPort = 49152
	}
	if dstPort == 0 {
		dstPort = 80
	}
	srcAddr, dstAddr := nodeAddr(now, src), nodeAddr(now, dst)
	outerAddr, outerPort := srcAddr, srcPort
	natFwdOK, natRetOK := true, true
	if res.Establishable && len(fwd.Paths) > 0 {
		var ok bool
		outerAddr, outerPort, ok = nat.translateForward(now, fwd.Paths[0], srcAddr, srcPort, dstAddr, dstPort)
		if !ok {
			natFwdOK = false
			res.Establishable = false
		}
	}
	if opts.Timeout {
		// The checked connection idle-timed out: its mappings expire
		// before any late return packet arrives.
		nat.expireCreated()
	}
	if ret.Reachable && len(ret.Paths) > 0 {
		natRetOK = nat.translateReturn(now, ret.Paths[0], dstAddr, dstPort, outerAddr, outerPort)
	}
	nat.lateReturns()
	res.NATEvents = nat.events
	res.NewMappings = nat.created
	res.KeptMappings = nat.kept

	retOK := ret.Reachable && natRetOK
	res.Bidirectional = res.Establishable && retOK
	res.Asymmetric = res.Establishable && !retOK

	res.Phases = append(res.Phases, ConnPhase{
		Phase: "establish", OK: res.Establishable,
		Reason: establishReason(fwd, natFwdOK),
		Detail: establishDetail(fwd, natFwdOK, nat),
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
	if !natRetOK {
		retDetail += "。地址转换校验未通过：" + natFailDetail(nat)
	}
	if res.Asymmetric {
		retDetail += "。去程放行但回程不通：非对称返回路径，不能凭借一次放行认作全程可达"
	}
	res.Phases = append(res.Phases, ConnPhase{
		Phase: "return", OK: retOK,
		Reason: returnReason(ret, natRetOK, nat),
		Detail: retDetail,
		Path:   ret,
	})

	res.Phases = append(res.Phases, t.teardownPhase(src, dst, opts))

	res.State, res.Reason = res.connState(opts)
	if !natFwdOK && res.State == "blocked" {
		res.Reason = "nat-pool-exhausted"
	}
	return res, nil
}

func establishReason(fwd *PathResult, natOK bool) string {
	if !natOK {
		return "nat-pool-exhausted"
	}
	return pickReason(fwd, "first-request-allowed")
}

func establishDetail(fwd *PathResult, natOK bool, nat *natSim) string {
	d := fmt.Sprintf("首次请求按新连接（state=new）逐跳评估：%s", zhVerdict(fwd))
	if !natOK {
		for _, ev := range nat.events {
			if ev.Decision == "pool-exhausted" {
				d += "。" + ev.Detail
			}
		}
	}
	return d
}

func returnReason(ret *PathResult, natOK bool, nat *natSim) string {
	if !natOK {
		for _, ev := range nat.events {
			if ev.Direction == "return" && ev.Decision != "matched" {
				return "nat-" + ev.Decision
			}
		}
		return "nat-unconfirmed"
	}
	return pickReason(ret, "return-allowed")
}

func natFailDetail(nat *natSim) string {
	for _, ev := range nat.events {
		if ev.Direction == "return" && ev.Decision != "matched" {
			return ev.Detail
		}
	}
	return "回程包无法确认对应连接"
}

// teardownPhase explains why an established connection would break:
// idle timeout, or a middle node/link of its pinned path going down.
// The pinned path is taken from the unfiltered topology (the path the
// connection was established on), so a reroute that helps new
// connections does not mask the break of the existing one.
func (t *Topology) teardownPhase(src, dst string, opts ConnOptions) ConnPhase {
	ph := ConnPhase{Phase: "teardown", OK: true, Reason: "still-up", Detail: "无超时或断开条件，连接保持"}
	var causes []string
	if opts.Timeout {
		causes = append(causes, "连接空闲超时")
	}
	if len(opts.DownNodes) > 0 || len(opts.DownLinks) > 0 {
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
	out := &Topology{Version: t.Version, Policy: t.Policy}
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

// Connection is an established connection the user registered while
// checking; its pinned path is the route it was established on.
type Connection struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	Target      string   `json:"target"`
	PathNodeIDs []string `json:"pathNodeIds,omitempty"`
	State       string   `json:"state"` // established
	// Wire identity and the translation mappings this connection was
	// established with; a change that invalidates them breaks the
	// connection even when the path itself still passes.
	SrcAddr  string       `json:"srcAddr,omitempty"`
	SrcPort  int          `json:"srcPort,omitempty"`
	DstAddr  string       `json:"dstAddr,omitempty"`
	DstPort  int          `json:"dstPort,omitempty"`
	Mappings []NATMapping `json:"mappings,omitempty"`
}

// ConnImpact explains what a policy/route change does to one existing
// connection, separately from what it does to new connections.
type ConnImpact struct {
	ConnID       string `json:"connId"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	BeforeStatus string `json:"beforeStatus"` // established
	AfterStatus  string `json:"afterStatus"`  // survives | broken-path | broken-policy | broken-nat
	Detail       string `json:"detail"`
	AfterHops    []Hop  `json:"afterHops"`
	// MappingStatus reports whether the connection's original NAT
	// mappings still hold under the after topology: kept |
	// invalidated-range | device-removed. Existing connections keep
	// their original translation; only new connections pick fresh ones.
	MappingStatus string `json:"mappingStatus,omitempty"`
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
	imp.MappingStatus, imp.AfterStatus, imp.Detail = mappingImpact(after, c, imp.AfterStatus, imp.Detail)
	return imp
}

// mappingImpact checks every NAT mapping the existing connection was
// established with against the after topology. The original
// translation is the connection's identity: a device that lost its
// NAT config or a pool that no longer covers the mapped port breaks
// the connection's return traffic even when the path still passes.
func mappingImpact(after *Topology, c Connection, status, detail string) (string, string, string) {
	if len(c.Mappings) == 0 {
		return "", status, detail
	}
	for _, m := range c.Mappings {
		n := after.nodeByID(m.Device)
		switch {
		case n == nil || n.NAT == nil || !n.NAT.Enabled:
			return "device-removed", "broken-nat",
				fmt.Sprintf("转换设备 %s 在改动后不再承担地址转换，既有连接的原映射 %s:%d 无法沿用，回程无法确认", m.Device, m.NATAddr, m.NATPort)
		case m.NATPort < n.NAT.PortStart || m.NATPort > n.NAT.PortEnd:
			return "invalidated-range", "broken-nat",
				fmt.Sprintf("设备 %s 的转换范围改为 %d-%d，既有连接的原映射端口 %d 不再可用，回程无法确认", m.Device, n.NAT.PortStart, n.NAT.PortEnd, m.NATPort)
		}
	}
	return "kept", status, detail
}

// SortConnImpacts keeps a stable rendering order for saved reports.
func SortConnImpacts(imps []ConnImpact) {
	sort.SliceStable(imps, func(i, j int) bool { return imps[i].ConnID < imps[j].ConnID })
}
