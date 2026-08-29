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

func TestHostileJSONDepthAndTrailingDataFailClosed(t *testing.T) {
	deep := strings.Repeat("[", 11000) + strings.Repeat("]", 11000)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(deep))
	if _, accepted := readJSONObject(response, request, true); accepted {
		t.Fatal("deep non-object JSON was accepted")
	}
	if response.Code != http.StatusBadRequest {
		t.Fatalf("deep JSON status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"ok":true}{"second":true}`))
	if _, accepted := readJSONObject(response, request, true); accepted || response.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHostileMediaRangesStayBoundedToVerifiedClip(t *testing.T) {
	root := t.TempDir()
	cameraRoot := filepath.Join(root, "cam-1")
	if err := os.MkdirAll(cameraRoot, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("0123456789")
	if err := os.WriteFile(filepath.Join(cameraRoot, "clip-range.mp4"), content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	core := &testClipCore{items: []contract.Clip{{
		ID: "clip-range", CameraID: "cam-1", Status: contract.ClipStatusProcessed,
		SizeBytes: int64(len(content)), Checksum: fmt.Sprintf("%x", digest), UpdatedAt: time.Unix(10, 0).UTC(),
	}}}
	request := httptest.NewRequest(http.MethodGet, "/api/clips/clip-range/media", nil)
	request.Header.Set("Range", "bytes=0-999999999999999,2-3")
	response := httptest.NewRecorder()
	handleClipMedia(core, root).ServeHTTP(response, request)
	if response.Code == http.StatusOK || len(response.Body.Bytes()) > len(content) {
		t.Fatalf("abusive range was not bounded: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestHostileResourceIDsRejectEncodedSeparatorsAndTraversal(t *testing.T) {
	for _, raw := range []string{"..", "%2e%2e", "%2Fsecret", "camera%2Fclip"} {
		if id, ok := resourceID("/api/clips/"+raw, "/api/clips/"); ok {
			t.Fatalf("hostile resource id accepted raw=%q decoded=%q", raw, id)
		}
	}
}
