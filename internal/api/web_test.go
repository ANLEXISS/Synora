package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebHandlerServesIndexAndSPAPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>synora</html>"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("console.log('ok')"), 0o640); err != nil {
		t.Fatal(err)
	}

	handler := (&Server{WebEnabled: true, WebRoot: root}).WebHandler()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantCache  string
	}{
		{name: "index", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "<html>synora</html>"},
		{name: "spa fallback", method: http.MethodGet, path: "/automations", wantStatus: http.StatusOK, wantBody: "<html>synora</html>"},
		{name: "asset", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "console.log('ok')", wantCache: "public, max-age=31536000, immutable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body=%s", rec.Body.String())
			}
			if tc.wantCache != "" && rec.Header().Get("Cache-Control") != tc.wantCache {
				t.Fatalf("cache-control=%q", rec.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestWebHandlerRejectsApiAndTraversalPaths(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "web")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>synora</html>"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "outside.txt"), []byte("outside"), 0o640); err != nil {
		t.Fatal(err)
	}

	handler := (&Server{WebEnabled: true, WebRoot: root}).WebHandler()

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "api-miss", method: http.MethodGet, path: "/api/does-not-exist"},
		{name: "api-post", method: http.MethodPost, path: "/automations"},
		{name: "traversal", method: http.MethodGet, path: "/../outside.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			handler.ServeHTTP(rec, req)
			if tc.name == "traversal" && rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "outside") {
				t.Fatalf("traversal leaked file: %s", rec.Body.String())
			}
			if tc.name != "traversal" && rec.Code != http.StatusNotFound {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "<html>synora</html>") && strings.HasPrefix(tc.path, "/api/") {
				t.Fatalf("api path served index: %s", rec.Body.String())
			}
		})
	}
}
