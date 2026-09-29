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
	if snap, err := st.Latest(); err == nil {
		srv.draft = snap.Topo
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", srv.health)
	mux.HandleFunc("GET /api/draft", srv.getDraft)
	mux.HandleFunc("PUT /api/draft", srv.putDraft)
	mux.HandleFunc("POST /api/save", srv.save)
	mux.HandleFunc("GET /api/versions", srv.listVersions)
	mux.HandleFunc("GET /api/versions/{v}", srv.getVersion)
	mux.HandleFunc("POST /api/check", srv.check)
	mux.HandleFunc("POST /api/diff", srv.diff)
	mux.HandleFunc("POST /api/import", srv.importTopo)
	if *staticDir != "" {
		mux.Handle("/", spaHandler(*staticDir))
	}
	log.Printf("topoplan listening on %s (data=%s)", *addr, *dataDir)
	log.Fatal(http.ListenAndServe(*addr, mux))
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
	writeJSON(w, 200, &t)
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
	writeJSON(w, 200, &t)
}

func deepClone(t *topo.Topology) *topo.Topology {
	b, _ := json.Marshal(t)
	var c topo.Topology
	_ = json.Unmarshal(b, &c)
	return &c
}

func errMissing(field string) error { return &fieldError{field} }

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
