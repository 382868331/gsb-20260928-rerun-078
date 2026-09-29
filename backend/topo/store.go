package topo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Store keeps immutable topology versions plus an undo stack. Versions
// are content addressed; importing a snapshot preserves every node ID.
type Store struct {
	mu       sync.RWMutex
	versions map[string]*Topology
	order    []string
	undo     []string // stack of previous draft version ids
	draft    string
}

func NewStore() *Store {
	return &Store{versions: map[string]*Topology{}}
}

// ContentHash identifies a topology by its stable content (IDs included).
func ContentHash(t *Topology) string {
	nodes := append([]Node{}, t.Nodes...)
	links := append([]Link{}, t.Links...)
	rules := append([]Rule{}, t.Rules...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(links, func(i, j int) bool { return links[i].ID < links[j].ID })
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	b, _ := json.Marshal(struct {
		Nodes []Node `json:"nodes"`
		Links []Link `json:"links"`
		Rules []Rule `json:"rules"`
	}{nodes, links, rules})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// Save stores a new draft version and pushes the previous draft onto the
// undo stack. The caller supplies node/link/rule IDs; they are preserved.
func (s *Store) Save(t *Topology) (*Topology, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.draft
	snap := *t
	// Version is always derived from content so the same topology (same node
	// identities, links and ordered rules) maps to the same version on save
	// and on a later re-import.
	snap.Version = ContentHash(t)
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now().UTC()
	}
	if _, ok := s.versions[snap.Version]; !ok {
		s.versions[snap.Version] = &snap
		s.order = append(s.order, snap.Version)
	}
	if prev != "" && prev != snap.Version {
		s.undo = append(s.undo, prev)
	}
	s.draft = snap.Version
	return s.versions[snap.Version], nil
}

// Undo restores the previous draft version. Node IDs are untouched because
// undo only swaps which immutable snapshot is current.
func (s *Store) Undo() (*Topology, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.undo) == 0 {
		return nil, fmt.Errorf("nothing to undo")
	}
	prev := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	s.draft = prev
	return s.versions[prev], nil
}

// Import parses an exported topology. IDs are kept exactly as supplied;
// the importer never regenerates them. A fresh content version is assigned
// only when the incoming version field is empty.
func (s *Store) Import(raw json.RawMessage) (*Topology, error) {
	var t Topology
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("invalid topology json: %w", err)
	}
	return s.Save(&t)
}

func (s *Store) Draft() (*Topology, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.draft == "" {
		return nil, fmt.Errorf("no topology loaded")
	}
	return s.versions[s.draft], nil
}

func (s *Store) Get(version string) (*Topology, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.versions[version]
	if !ok {
		return nil, fmt.Errorf("unknown version %q", version)
	}
	return t, nil
}

// Versions lists saved version ids newest-last.
func (s *Store) Versions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string{}, s.order...)
}

func (s *Store) CanUndo() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.undo) > 0
}
