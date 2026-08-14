package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
