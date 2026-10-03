package topo

import "fmt"

// nat.go: address-translation-aware connection checks.
//
// A NAT device rewrites the source address/port of a connection as it
// crosses the device, so the same connection appears with different
// addresses on different path segments. The simulation here is a pure
// what-if computation: it builds per-device translation tables from
// the registered existing connections, allocates deterministic ports
// for the checked (new) connection, and verifies that return traffic
// finds the exact mapping it belongs to. A translated port is never
// treated as reusable by every old connection: a late return packet
// that cannot be confirmed against one specific mapping is dropped,
// never delivered to a different later connection.

// NATConfig turns a node into an address-translation device with a
// deterministic port pool. Attached to Node as an optional block.
type NATConfig struct {
	Enabled   bool   `json:"enabled"`
	Address   string `json:"address"`   // translated (outside) source address
	PortStart int    `json:"portStart"` // first translatable port, inclusive
	PortEnd   int    `json:"portEnd"`   // last translatable port, inclusive
}

// NATMapping is one live translation-table entry. The peer tuple is
// part of the key: a return packet is matched against the full tuple
// before any rewrite, so one translated port is never a blanket pass
// for every old connection.
type NATMapping struct {
	Device     string `json:"device"`
	InsideAddr string `json:"insideAddr"`
	InsidePort int    `json:"insidePort"`
	NATAddr    string `json:"natAddr"`
	NATPort    int    `json:"natPort"`
	PeerAddr   string `json:"peerAddr"`
	PeerPort   int    `json:"peerPort"`
	ConnID     string `json:"connId,omitempty"`
	Status     string `json:"status"` // active | expired | invalidated-range
}

// NATHopEvent records one translation decision along the checked
// connection: which device, which addresses went in, which came out,
// and why. Direction is forward | return | late-return.
type NATHopEvent struct {
	Device    string `json:"device"`
	Direction string `json:"direction"`
	PreAddr   string `json:"preAddr"`
	PrePort   int    `json:"prePort"`
	PostAddr  string `json:"postAddr"`
	PostPort  int    `json:"postPort"`
	Decision  string `json:"decision"` // new-mapping | matched | expired | port-reused | no-mapping | pool-exhausted | range-changed
	ConnID    string `json:"connId,omitempty"`
	Detail    string `json:"detail"`
}

// natSim holds the per-device translation state for one trial.
type natSim struct {
	ranges  map[string][2]int // effective port pool per device (what-if overrides applied)
	active  []NATMapping      // mappings return traffic may match
	stale   []NATMapping      // expired / range-invalidated mappings, kept for late-packet attribution
	kept    []NATMapping      // existing connections' mappings left untouched
	created []NATMapping      // mappings allocated for the checked connection
	events  []NATHopEvent
}

// nodeAddr is the address a node presents on the wire: its ip
// attribute when set, otherwise its identity.
func nodeAddr(t *Topology, id string) string {
	if n := t.nodeByID(id); n != nil {
		if ip := n.Attrs["ip"]; ip != "" {
			return ip
		}
	}
	return id
}

func natCfgOf(t *Topology, id string) *NATConfig {
	if n := t.nodeByID(id); n != nil && n.NAT != nil && n.NAT.Enabled {
		return n.NAT
	}
	return nil
}

// newNATSim seeds the translation tables from the registered existing
// connections. Mappings of connections flagged as timed out, and
// mappings falling outside a what-if range override, become stale:
// they no longer confirm return traffic but still explain late
// packets. Existing connections that survive keep their original
// translation untouched (reported separately as kept).
func newNATSim(t *Topology, opts ConnOptions) *natSim {
	s := &natSim{
		ranges:  map[string][2]int{},
		active:  []NATMapping{},
		stale:   []NATMapping{},
		kept:    []NATMapping{},
		created: []NATMapping{},
		events:  []NATHopEvent{},
	}
	for _, n := range t.Nodes {
		if n.NAT != nil && n.NAT.Enabled {
			s.ranges[n.ID] = [2]int{n.NAT.PortStart, n.NAT.PortEnd}
		}
	}
	for dev, r := range opts.NATRanges {
		if _, ok := s.ranges[dev]; ok {
			s.ranges[dev] = r
		}
	}
	expired := map[string]bool{}
	for _, id := range opts.ExpiredConns {
		expired[id] = true
	}
	for _, c := range opts.Existing {
		for _, m := range c.Mappings {
			m.ConnID = c.ID
			switch {
			case expired[c.ID]:
				m.Status = "expired"
				s.stale = append(s.stale, m)
			case s.outOfRange(m):
				m.Status = "invalidated-range"
				s.stale = append(s.stale, m)
			default:
				m.Status = "active"
				s.active = append(s.active, m)
				s.kept = append(s.kept, m)
			}
		}
	}
	return s
}

func (s *natSim) outOfRange(m NATMapping) bool {
	r, ok := s.ranges[m.Device]
	if !ok {
		return false
	}
	return m.NATPort < r[0] || m.NATPort > r[1]
}

func (s *natSim) portInUse(device string, port int) bool {
	for _, m := range s.active {
		if m.Device == device && m.NATPort == port {
			return true
		}
	}
	return false
}

// allocate picks the lowest free port of the device's pool. The
// choice is deterministic so a saved trial explains itself identically
// after a reload. ok=false means the pool is exhausted.
func (s *natSim) allocate(device, natAddr, insideAddr string, insidePort int, peerAddr string, peerPort int) (NATMapping, bool) {
	r := s.ranges[device]
	for p := r[0]; p <= r[1]; p++ {
		if !s.portInUse(device, p) {
			m := NATMapping{
				Device: device, InsideAddr: insideAddr, InsidePort: insidePort,
				NATAddr: natAddr, NATPort: p,
				PeerAddr: peerAddr, PeerPort: peerPort,
				Status: "active",
			}
			s.active = append(s.active, m)
			return m, true
		}
	}
	return NATMapping{}, false
}

// lookup resolves a return-side packet (dst = translated addr/port,
// src = peer) against one device's table. An exact active mapping
// confirms the packet; anything else explains why it cannot be
// confirmed and must be dropped.
func (s *natSim) lookup(device, natAddr string, natPort int, peerAddr string, peerPort int) (*NATMapping, string) {
	for i := range s.active {
		m := &s.active[i]
		if m.Device == device && m.NATAddr == natAddr && m.NATPort == natPort &&
			m.PeerAddr == peerAddr && m.PeerPort == peerPort {
			return m, "matched"
		}
	}
	for i := range s.active {
		m := &s.active[i]
		if m.Device == device && m.NATAddr == natAddr && m.NATPort == natPort {
			return m, "port-reused"
		}
	}
	for i := range s.stale {
		m := &s.stale[i]
		if m.Device == device && m.NATAddr == natAddr && m.NATPort == natPort {
			if m.Status == "invalidated-range" {
				return m, "range-changed"
			}
			return m, "expired"
		}
	}
	return nil, "no-mapping"
}

// translateForward walks the chosen forward path and rewrites the
// connection's source at every NAT device, allocating a fresh mapping
// for the checked connection. ok=false (pool exhausted) blocks the
// connection even when every policy rule allowed it.
func (s *natSim) translateForward(t *Topology, p Path, srcAddr string, srcPort int, dstAddr string, dstPort int) (string, int, bool) {
	curAddr, curPort := srcAddr, srcPort
	for _, id := range p.NodeIDs[1:] {
		cfg := natCfgOf(t, id)
		if cfg == nil {
			continue
		}
		m, ok := s.allocate(id, cfg.Address, curAddr, curPort, dstAddr, dstPort)
		if !ok {
			r := s.ranges[id]
			s.events = append(s.events, NATHopEvent{
				Device: id, Direction: "forward",
				PreAddr: curAddr, PrePort: curPort,
				PostAddr: cfg.Address,
				Decision: "pool-exhausted",
				Detail:   fmt.Sprintf("设备 %s 转换池 %d-%d 的端口全部被占用，无法为 %s:%d 建立新映射，连接被拒绝", id, r[0], r[1], curAddr, curPort),
			})
			return curAddr, curPort, false
		}
		s.created = append(s.created, m)
		s.events = append(s.events, NATHopEvent{
			Device: id, Direction: "forward",
			PreAddr: curAddr, PrePort: curPort,
			PostAddr: m.NATAddr, PostPort: m.NATPort,
			Decision: "new-mapping",
			Detail:   fmt.Sprintf("设备 %s 新建映射 %s:%d → %s:%d（对端 %s:%d），该端口仅此连接可用", id, curAddr, curPort, m.NATAddr, m.NATPort, dstAddr, dstPort),
		})
		curAddr, curPort = m.NATAddr, m.NATPort
	}
	return curAddr, curPort, true
}

// translateReturn walks the chosen return path. The packet's
// destination starts as the outermost translated address the peer
// replied to; every NAT device on the way must confirm one specific
// mapping before rewriting it back. ok=false means the packet is
// dropped as unconfirmed.
func (s *natSim) translateReturn(t *Topology, p Path, dstAddr string, dstPort int, toAddr string, toPort int) bool {
	curDstAddr, curDstPort := toAddr, toPort
	ok := true
	for _, id := range p.NodeIDs[1:] {
		cfg := natCfgOf(t, id)
		if cfg == nil {
			continue
		}
		m, decision := s.lookup(id, curDstAddr, curDstPort, dstAddr, dstPort)
		ev := NATHopEvent{
			Device: id, Direction: "return",
			PreAddr: curDstAddr, PrePort: curDstPort,
			Decision: decision,
		}
		switch decision {
		case "matched":
			ev.PostAddr, ev.PostPort = m.InsideAddr, m.InsidePort
			ev.ConnID = m.ConnID
			ev.Detail = fmt.Sprintf("设备 %s 命中映射：回程目的 %s:%d 还原为 %s:%d（对端 %s:%d 一致）", id, curDstAddr, curDstPort, m.InsideAddr, m.InsidePort, dstAddr, dstPort)
			curDstAddr, curDstPort = m.InsideAddr, m.InsidePort
		case "port-reused":
			ev.PostAddr, ev.PostPort = m.NATAddr, m.NATPort
			ev.ConnID = m.ConnID
			ev.Detail = fmt.Sprintf("设备 %s 的 %s:%d 现属于另一条连接（对端 %s:%d），本包无法确认归属，丢弃，绝不放行到该连接", id, curDstAddr, curDstPort, m.PeerAddr, m.PeerPort)
			ok = false
		case "expired":
			ev.ConnID = m.ConnID
			ev.Detail = fmt.Sprintf("设备 %s 上 %s:%d 的映射已因空闲超时失效：迟到回程包属于旧连接但无法确认，丢弃", id, curDstAddr, curDstPort)
			ok = false
		case "range-changed":
			ev.ConnID = m.ConnID
			ev.Detail = fmt.Sprintf("设备 %s 的转换范围已调整，%s:%d 不再在池内：旧映射失效，回程包无法确认，丢弃", id, curDstAddr, curDstPort)
			ok = false
		default:
			ev.Decision = "no-mapping"
			ev.Detail = fmt.Sprintf("设备 %s 上没有 %s:%d 的映射：回程经过未参与去程转换的设备，不能凭去程已放行认定回程有效，丢弃", id, curDstAddr, curDstPort)
			ok = false
		}
		s.events = append(s.events, ev)
	}
	return ok
}

// expireCreated moves the checked connection's own mappings to the
// stale set, simulating idle timeout before a late return arrives.
func (s *natSim) expireCreated() {
	if len(s.created) == 0 {
		return
	}
	created := map[string]bool{}
	for _, m := range s.created {
		created[fmt.Sprintf("%s\x00%d", m.Device, m.NATPort)] = true
	}
	var keep []NATMapping
	for _, m := range s.active {
		if created[fmt.Sprintf("%s\x00%d", m.Device, m.NATPort)] {
			m.Status = "expired"
			s.stale = append(s.stale, m)
			continue
		}
		keep = append(keep, m)
	}
	s.active = keep
}

// lateReturns explains, for every stale mapping inherited from an
// existing connection, what a late return packet would hit now: the
// port may have been reallocated to a newer connection, but the packet
// is never handed to it.
func (s *natSim) lateReturns() {
	for _, m := range s.stale {
		if m.ConnID == "" {
			continue // the checked connection's own expiry is covered by the return phase
		}
		ev := NATHopEvent{
			Device: m.Device, Direction: "late-return",
			PreAddr: m.NATAddr, PrePort: m.NATPort,
			ConnID: m.ConnID,
		}
		cur, dec := s.lookup(m.Device, m.NATAddr, m.NATPort, m.PeerAddr, m.PeerPort)
		switch {
		case dec == "matched" || dec == "port-reused":
			ev.Decision = "port-reused"
			if cur != nil {
				ev.PostAddr, ev.PostPort = cur.NATAddr, cur.NATPort
			}
			ev.Detail = fmt.Sprintf("连接 %s 的旧映射端口 %s:%d 已被新连接占用：迟到回程包无法确认属于旧连接，丢弃，不放行到占用该端口的新连接", m.ConnID, m.NATAddr, m.NATPort)
		case m.Status == "invalidated-range":
			ev.Decision = "range-changed"
			ev.Detail = fmt.Sprintf("连接 %s 的映射 %s:%d 因转换范围调整失效：迟到回程包属于旧连接但无法确认，丢弃", m.ConnID, m.NATAddr, m.NATPort)
		default:
			ev.Decision = "expired"
			ev.Detail = fmt.Sprintf("连接 %s 的映射 %s:%d 已超时失效：迟到回程包属于旧连接但无法确认，丢弃", m.ConnID, m.NATAddr, m.NATPort)
		}
		s.events = append(s.events, ev)
	}
}
