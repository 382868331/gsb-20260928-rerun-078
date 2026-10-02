// Package store persists topology versions as immutable snapshots.
// Capacity planning (a later round) will reference these same versions
// rather than any live network state: nothing here ever talks to a
// real network.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"topoplan/internal/topo"
)

type Store struct {
	mu  sync.Mutex
	dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Snapshot metadata kept next to the full topology JSON.
type Meta struct {
	Version int    `json:"version"`
	Label   string `json:"label"`
	Created string `json:"created"`
	Nodes   int    `json:"nodes"`
	Links   int    `json:"links"`
	Rules   int    `json:"rules"`
}

type Snapshot struct {
	Meta Meta           `json:"meta"`
	Topo *topo.Topology `json:"topology"`
}

func (s *Store) path(v int) string {
	return filepath.Join(s.dir, fmt.Sprintf("v%06d.json", v))
}

func (s *Store) draftPath() string {
	return filepath.Join(s.dir, "draft.json")
}

// SaveDraft persists the working draft (unsaved or just-saved) so a
// page refresh or server restart restores exactly what the user last
// worked on, instead of falling back to a factory sample.
func (s *Store) SaveDraft(t *topo.Topology) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.draftPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.draftPath())
}

// LoadDraft returns the persisted working draft, or os.ErrNotExist
// when no draft was ever persisted.
func (s *Store) LoadDraft() (*topo.Topology, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.draftPath())
	if err != nil {
		return nil, err
	}
	var t topo.Topology
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Save validates, stamps the next version and writes an immutable
// snapshot. Node/link/rule IDs inside the payload are preserved.
func (s *Store) Save(t *topo.Topology, label string) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := t.Normalize(); err != nil {
		return nil, err
	}
	next := t.Version
	if next == 0 {
		next = s.latestLocked() + 1
	} else {
		if _, err := os.Stat(s.path(next)); err == nil {
			return nil, fmt.Errorf("version %d already exists", next)
		}
		if next > s.latestLocked()+1 {
			return nil, fmt.Errorf("version %d would skip ahead", next)
		}
	}
	t.Version = next
	snap := &Snapshot{Meta: Meta{
		Version: next, Label: label,
		Created: nowISO(),
		Nodes:   len(t.Nodes), Links: len(t.Links), Rules: len(t.Policy.Rules),
	}, Topo: t}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return nil, err
	}
	tmp := s.path(next) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, s.path(next)); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *Store) Load(version int) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(version)
}

// loadLocked reads a snapshot without taking the mutex, so callers
// that already hold s.mu (Latest/List) cannot deadlock against
// themselves.
func (s *Store) loadLocked(version int) (*Snapshot, error) {
	b, err := os.ReadFile(s.path(version))
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (s *Store) Latest() (*Snapshot, error) {
	s.mu.Lock()
	v := s.latestLocked()
	if v == 0 {
		s.mu.Unlock()
		return nil, os.ErrNotExist
	}
	defer s.mu.Unlock()
	return s.loadLocked(v)
}

func (s *Store) List() ([]Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	metas := []Meta{} // never nil: the API must serialize [] not null
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "v%06d.json", &v); err != nil {
			continue
		}
		if snap, err := s.loadLocked(v); err == nil {
			metas = append(metas, snap.Meta)
		}
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Version < metas[j].Version })
	return metas, nil
}

// latestLocked must be called with mu held.
func (s *Store) latestLocked() int {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0
	}
	latest := 0
	for _, e := range entries {
		var v int
		if _, err := fmt.Sscanf(e.Name(), "v%06d.json", &v); err == nil && v > latest {
			latest = v
		}
	}
	return latest
}
