package topo

import (
	"encoding/json"
	"net/http"
)

type Server struct {
	Store *Store
}

func NewServer() *Server {
	return &Server{Store: NewStore()}
}

type evaluateReq struct {
	Version  string       `json:"version"` // empty = current draft
	Conn     Connection   `json:"conn"`
	Conns    []Connection `json:"conns"`
	MaxPaths int          `json:"maxPaths"`
}

type diffReq struct {
	FromVersion string       `json:"fromVersion"`
	ToVersion   string       `json:"toVersion"` // empty = current draft
	Conns       []Connection `json:"conns"`
	MaxPaths    int          `json:"maxPaths"`
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/versions", s.handleVersions)
	mux.HandleFunc("GET /api/topology", s.handleGetDraft)
	mux.HandleFunc("POST /api/topology", s.handleSave)
	mux.HandleFunc("POST /api/undo", s.handleUndo)
	mux.HandleFunc("POST /api/import", s.handleImport)
	mux.HandleFunc("POST /api/evaluate", s.handleEvaluate)
	mux.HandleFunc("POST /api/diff", s.handleDiff)
	return withCORS(mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) pick(version string) (*Topology, error) {
	if version != "" {
		return s.Store.Get(version)
	}
	return s.Store.Draft()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok", "mode": "simulation"})
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	versions := s.Store.Versions()
	writeJSON(w, 200, map[string]any{"versions": versions, "canUndo": s.Store.CanUndo()})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Draft()
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	var t Topology
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeErr(w, 400, err)
		return
	}
	saved, err := s.Store.Save(&t)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, saved)
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	t, err := s.Store.Undo()
	if err != nil {
		writeErr(w, 409, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, 400, err)
		return
	}
	t, err := s.Store.Import(raw)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var req evaluateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	t, err := s.pick(req.Version)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if len(req.Conns) > 0 {
		res, err := t.EvaluateAll(req.Conns, req.MaxPaths)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]any{"results": res})
		return
	}
	res, err := t.Evaluate(req.Conn, req.MaxPaths)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	var req diffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	before, err := s.Store.Get(req.FromVersion)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	after, err := s.pick(req.ToVersion)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	d, err := Diff(before, after, req.Conns, req.MaxPaths)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, 200, d)
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}
