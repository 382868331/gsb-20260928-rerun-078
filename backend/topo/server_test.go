package topo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postJSON(t *testing.T, h http.Handler, path string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestHTTPEvaluateFlow(t *testing.T) {
	srv := NewServer()
	h := srv.Routes()
	tp := sampleTopo()
	permitAll(tp)
	if code, out := postJSON(t, h, "/api/topology", tp); code != 200 {
		t.Fatalf("save: %d %v", code, out)
	}
	code, out := postJSON(t, h, "/api/evaluate", map[string]any{
		"conn":     map[string]string{"src": "n-h1", "dst": "n-h2", "protocol": "tcp"},
		"maxPaths": 3,
	})
	if code != 200 || out["reachable"] != true {
		t.Fatalf("evaluate: %d %v", code, out)
	}
	paths := out["paths"].([]any)
	if len(paths) != 2 {
		t.Fatalf("expected 2 candidate paths, got %v", paths)
	}
	// save changed policy then diff against the first version
	before := out
	_ = before
	v1resp := getJSON(t, h, "/api/topology")
	v1 := v1resp["version"].(string)
	tp2 := sampleTopo() // fw permits tcp but core default-denies
	code, saved := postJSON(t, h, "/api/topology", tp2)
	if code != 200 {
		t.Fatalf("save v2: %v", saved)
	}
	code, diff := postJSON(t, h, "/api/diff", map[string]any{
		"fromVersion": v1,
		"conns": []map[string]string{
			{"src": "n-h1", "dst": "n-h2", "protocol": "tcp"},
			{"src": "n-h1", "dst": "n-h2", "protocol": "icmp"},
		},
		"maxPaths": 3,
	})
	if code != 200 {
		t.Fatalf("diff: %v", diff)
	}
	blocked := diff["blocked"].([]any)
	if len(blocked) != 1 {
		t.Fatalf("expected tcp blocked after rollback, got %v", blocked)
	}
}

func getJSON(t *testing.T, h http.Handler, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %s", path, rec.Body.String())
	}
	return out
}
