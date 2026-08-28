package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

type testClipCore struct {
	items []contract.Clip
}

func (c *testClipCore) Clips(int) ([]contract.Clip, error) { return c.items, nil }

func (c *testClipCore) Clip(id string) (*contract.Clip, error) {
	for _, item := range c.items {
		if item.ID == id {
			value := item
			return &value, nil
		}
	}
	return nil, contract.NewAPIError(contract.ErrorNotFound, "clip not found")
}

func TestClipRESTMetadataRoutes(t *testing.T) {
	core := &testClipCore{items: []contract.Clip{{ID: "clip-api", CameraID: "cam-1", Status: contract.ClipStatusMissing, Path: "/secret/path"}}}
	req := httptest.NewRequest(http.MethodGet, "/api/clips?limit=10", nil)
	rec := httptest.NewRecorder()
	handleClipCollection(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") == "" {
		t.Fatalf("list status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/secret") {
		t.Fatalf("REST exposed internal path: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/clips/clip-api", nil)
	rec = httptest.NewRecorder()
	handleClipRoute(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "/secret") {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/clips/clip-api", nil)
	rec = httptest.NewRecorder()
	handleClipRoute(core).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d", rec.Code)
	}
}

func TestClipMediaRouteServesVerifiedRangeAndRejectsInvalidState(t *testing.T) {
	root := t.TempDir()
	cameraDir := filepath.Join(root, "cam-1")
	if err := os.MkdirAll(cameraDir, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("0123456789")
	path := filepath.Join(cameraDir, "clip-media.mp4")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	core := &testClipCore{items: []contract.Clip{{
		ID: "clip-media", CameraID: "cam-1", Status: contract.ClipStatusProcessed,
		SizeBytes: int64(len(content)), Checksum: fmt.Sprintf("%x", digest), UpdatedAt: time.Unix(10, 0).UTC(),
	}}}
	req := httptest.NewRequest(http.MethodGet, "/api/clips/clip-media/media", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handleClipRouteWithRoot(core, root).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range status=%d body=%q headers=%v", rec.Code, rec.Body.String(), rec.Header())
	}

	core.items[0].Checksum = strings.Repeat("0", 64)
	req = httptest.NewRequest(http.MethodGet, "/api/clips/clip-media/media", nil)
	rec = httptest.NewRecorder()
	handleClipMedia(core, root).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("checksum mismatch status=%d body=%s", rec.Code, rec.Body.String())
	}

	core.items[0].Status = contract.ClipStatusExpired
	rec = httptest.NewRecorder()
	handleClipMedia(core, root).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expired media status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestClipMediaRouteRejectsSymlinkAndTraversal(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cam-1"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "cam-1", "clip-link.mp4")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	core := &testClipCore{items: []contract.Clip{{ID: "clip-link", CameraID: "cam-1", Status: contract.ClipStatusReady, SizeBytes: 6}}}
	req := httptest.NewRequest(http.MethodGet, "/api/clips/clip-link/media", nil)
	rec := httptest.NewRecorder()
	handleClipMedia(core, root).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("symlink status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	traversalRequest := httptest.NewRequest(http.MethodGet, "/api/clips/../outside/media", nil)
	handleClipMedia(core, root).ServeHTTP(rec, traversalRequest)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal status=%d body=%s", rec.Code, rec.Body.String())
	}
}
