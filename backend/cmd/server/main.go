package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"topoplan/internal/store"
	"topoplan/internal/topo"
)

type server struct {
	mu    sync.Mutex
	store *store.Store
	draft *topo.Topology // working copy; IDs preserved across edits
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", envOr("TOPO_DATA", "./data"), "snapshot directory")
	staticDir := flag.String("static", envOr("TOPO_STATIC", ""), "optional built frontend dir")
	flag.Parse()

	st, err := store.New(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	srv := &server{store: st, draft: &topo.Topology{Policy: topo.Policy{DefaultAction: topo.ActionDeny}}}
	// Restore the user's actual working state: the persisted draft wins
	// (it reflects the last save/edit), then the latest snapshot. Only a
	// brand-new data dir starts empty -- we never substitute a factory
	// sample for a previously saved topology.
	if d, err := st.LoadDraft(); err == nil {
		srv.draft = d
	} else if snap, err := st.Latest(); err == nil {
		srv.draft = snap.Topo
	}

	mux := http.NewServeMux()
	srv.registerAPI(mux)
	if *staticDir != "" {
		mux.Handle("/", spaHandler(*staticDir))
	}
	log.Printf("topoplan listening on %s (data=%s)", *addr, *dataDir)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func (s *server) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/draft", s.getDraft)
	mux.HandleFunc("PUT /api/draft", s.putDraft)
	mux.HandleFunc("POST /api/save", s.save)
	mux.HandleFunc("GET /api/versions", s.listVersions)
	mux.HandleFunc("GET /api/versions/{v}", s.getVersion)
	mux.HandleFunc("POST /api/check", s.check)
	mux.HandleFunc("POST /api/diff", s.diff)
	mux.HandleFunc("POST /api/conncheck", s.connCheck)
	mux.HandleFunc("POST /api/conndiff", s.connDiff)
	mux.HandleFunc("POST /api/import", s.importTopo)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok", "mode": "offline-design"})
}

func (s *server) getDraft(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, 200, s.draft)
}

// putDraft replaces the working draft. Version 0 keeps it unsaved;
// any other version references the snapshot lineage. IDs are untouched.
func (s *server) putDraft(w http.ResponseWriter, r *http.Request) {
	var t topo.Topology
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := t.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.mu.Lock()
	s.draft = &t
	s.mu.Unlock()
	s.persistDraft()
	writeJSON(w, 200, &t)
}

// persistDraft writes the current working draft to disk so a refresh
// or restart restores it verbatim. A failure is logged but does not
// fail the request: the in-memory state is still authoritative.
func (s *server) persistDraft() {
	s.mu.Lock()
	d := s.draft
	s.mu.Unlock()
	if d == nil {
		return
	}
	if err := s.store.SaveDraft(deepClone(d)); err != nil {
		log.Printf("persist draft: %v", err)
	}
}

type saveReq struct {
	Topology *topo.Topology `json:"topology"`
	Label    string         `json:"label"`
}

func (s *server) save(w http.ResponseWriter, r *http.Request) {
	var req saveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	t := req.Topology
	if t == nil {
		s.mu.Lock()
		t = s.draft
		s.mu.Unlock()
	}
	clone := deepClone(t)
	clone.Version = 0 // store assigns the next version
	snap, err := s.store.Save(clone, req.Label)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s.mu.Lock()
	s.draft = snap.Topo
	s.mu.Unlock()
	s.persistDraft()
	writeJSON(w, 200, snap)
}

func (s *server) listVersions(w http.ResponseWriter, r *http.Request) {
	metas, err := s.store.List()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, metas)
}

func (s *server) getVersion(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.Atoi(strings.TrimSpace(r.PathValue("v")))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	snap, err := s.store.Load(v)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, snap)
}

type checkReq struct {
	Topology *topo.Topology `json:"topology,omitempty"`
	Source   string         `json:"source"`
	Target   string         `json:"target"`
	MaxPaths int            `json:"maxPaths"`
}

func (s *server) check(w http.ResponseWriter, r *http.Request) {
	var req checkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	t := req.Topology
	if t == nil {
		s.mu.Lock()
		t = s.draft
		s.mu.Unlock()
	}
	if err := t.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	res, err := t.FindPaths(req.Source, req.Target, req.MaxPaths)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}

type diffReq struct {
	Before   *topo.Topology `json:"before,omitempty"`
	After    *topo.Topology `json:"after"`
	BeforeV  int            `json:"beforeVersion,omitempty"`
	Detailed bool           `json:"detailed"`
}

func (s *server) diff(w http.ResponseWriter, r *http.Request) {
	var req diffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	before := req.Before
	if before == nil && req.BeforeV > 0 {
		snap, err := s.store.Load(req.BeforeV)
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		before = snap.Topo
	}
	if before == nil {
		s.mu.Lock()
		before = s.draft
		s.mu.Unlock()
	}
	if req.After == nil {
		writeErr(w, 400, errMissing("after"))
		return
	}
	if err := before.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := req.After.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	rep, err := topo.ComparePolicy(before, req.After, req.Detailed)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, rep)
}

// importTopo accepts an externally produced topology JSON and returns
// a normalized copy. Importing never mints new IDs: the identity set
// of the file is preserved verbatim so undo/save line up.
func (s *server) importTopo(w http.ResponseWriter, r *http.Request) {
	var t topo.Topology
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeErr(w, 400, err)
		return
	}
	t.Version = 0
	if err := t.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.mu.Lock()
	s.draft = &t
	s.mu.Unlock()
	s.persistDraft()
	writeJSON(w, 200, &t)
}

func deepClone(t *topo.Topology) *topo.Topology {
	b, _ := json.Marshal(t)
	var c topo.Topology
	_ = json.Unmarshal(b, &c)
	return &c
}

func errMissing(field string) error { return &fieldError{field} }

type connCheckReq struct {
	Topology     *topo.Topology `json:"topology,omitempty"`
	Source       string         `json:"source"`
	Target       string         `json:"target"`
	MaxPaths     int            `json:"maxPaths"`
	AfterTimeout bool           `json:"afterTimeout"`
}

// connCheck explains one connection across its lifecycle phases
// (establish / maintain / return / teardown). The topology in the
// body is used as-is, so what-if trials (node removed, rules
// reordered) never disturb the stored draft or other scenarios.
func (s *server) connCheck(w http.ResponseWriter, r *http.Request) {
	var req connCheckReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	t := req.Topology
	if t == nil {
		s.mu.Lock()
		t = s.draft
		s.mu.Unlock()
	}
	if err := t.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	res, err := t.ConnCheck(req.Source, req.Target, req.MaxPaths, req.AfterTimeout)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}

type connDiffReq struct {
	Before   *topo.Topology `json:"before,omitempty"`
	After    *topo.Topology `json:"after"`
	BeforeV  int            `json:"beforeVersion,omitempty"`
	Detailed bool           `json:"detailed"`
}

// connDiff reports the impact of an edit split by connection state:
// new connections, already-established connections, and connections
// that only survive until their next timeout.
func (s *server) connDiff(w http.ResponseWriter, r *http.Request) {
	var req connDiffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	before := req.Before
	if before == nil && req.BeforeV > 0 {
		snap, err := s.store.Load(req.BeforeV)
		if err != nil {
			writeErr(w, 404, err)
			return
		}
		before = snap.Topo
	}
	if before == nil {
		s.mu.Lock()
		before = s.draft
		s.mu.Unlock()
	}
	if req.After == nil {
		writeErr(w, 400, errMissing("after"))
		return
	}
	if err := before.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := req.After.Normalize(); err != nil {
		writeErr(w, 400, err)
		return
	}
	rep, err := topo.ConnDiff(before, req.After, req.Detailed)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, rep)
}

type fieldError struct{ f string }

func (e *fieldError) Error() string { return "missing required field: " + e.f }
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if _, err := os.Stat(p); os.IsNotExist(err) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fs.ServeHTTP(w, r2)
			return
		}
		fs.ServeHTTP(w, r)
	})
}
