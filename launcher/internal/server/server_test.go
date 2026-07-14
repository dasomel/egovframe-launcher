package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"egovframe-launcher/internal/runner"
)

func newTestServer() http.Handler {
	assets := fstest.MapFS{"index.html": {Data: []byte("<h1>ok</h1>")}}
	return New(runner.New(""), assets, "test")
}

func TestTargetsEndpointReturnsJSON(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/api/targets", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("code=%d", w.Code)
	}
	var views []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &views); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("no targets returned")
	}
}

func TestUnknownActionIs400(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("POST", "/api/targets/boot-sample/frobnicate", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", w.Code)
	}
}

func TestIndexServed(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("index not served: %d %q", w.Code, w.Body.String())
	}
}

func TestExtensionStatusRouteReturnsInstalledBool(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest("GET", "/api/extension-status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("code=%d, want 200", w.Code)
	}
	var result map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if _, ok := result["installed"]; !ok {
		t.Fatal("response missing 'installed' key")
	}
}

func TestTomcatActionWithEmptyTomcatPathIs202(t *testing.T) {
	// newTestServer creates a server with empty tomcatPath by default, which now returns 202 because it triggers auto-installation.
	srv := newTestServer()
	req := httptest.NewRequest("POST", "/api/targets/web-sample/tomcat", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("code=%d, want 202", w.Code)
	}
}
