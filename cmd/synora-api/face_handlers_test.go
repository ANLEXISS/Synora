package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

type faceTestProvider struct {
	resident map[string]any
}

func (p *faceTestProvider) Resident(string) (map[string]any, error) {
	return p.resident, nil
}

func (p *faceTestProvider) UpdateResident(_ string, body json.RawMessage) (map[string]any, error) {
	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		return nil, err
	}
	p.resident["face_profile"] = patch["face_profile"]
	return p.resident, nil
}

func faceUploadRequest(t *testing.T, payload []byte) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/residents/alexis/face/base", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, httptest.NewRecorder()
}

func TestFaceUploadMaxFourAndMetadata(t *testing.T) {
	root := t.TempDir()
	provider := &faceTestProvider{resident: map[string]any{"id": "alexis"}}
	store := newFaceStore(root)

	request, recorder := faceUploadRequest(t, []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'})
	photo, err := store.upload(recorder, provider, "alexis", request)
	if err != nil {
		t.Fatalf("upload face photo: %v", err)
	}
	if photo.ID == "" || photo.Filename == "" || photo.Path == "" || photo.Source != "manual_upload" || photo.View != "face" {
		t.Fatalf("unexpected photo metadata: %#v", photo)
	}
	if filepath.Dir(photo.Path) != filepath.Join(root, "alexis", "base") {
		t.Fatalf("photo was not stored in canonical base directory: %s", photo.Path)
	}
	if _, err := os.Stat(photo.Path); err != nil {
		t.Fatalf("uploaded file missing: %v", err)
	}

	profile, err := store.profile(provider, "alexis")
	if err != nil || len(profile.BasePhotos) != 1 || profile.Status != "needs_rebuild" {
		t.Fatalf("unexpected profile after upload: %#v err=%v", profile, err)
	}

	for _, name := range []string{"two.jpg", "three.jpg", "four.jpg"} {
		if err := os.WriteFile(filepath.Join(root, "alexis", "base", name), []byte{0xff, 0xd8, 0xff, 0xe0}, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	profile.BasePhotos = []contract.FacePhoto{{ID: photo.ID, Filename: photo.Filename, Path: photo.Path, CreatedAt: time.Now(), UpdatedAt: time.Now()}}
	if _, err := store.updateProfile(provider, "alexis", profile); err != nil {
		t.Fatal(err)
	}
	request, recorder = faceUploadRequest(t, []byte{0xff, 0xd8, 0xff, 0xe0})
	if _, err := store.upload(recorder, provider, "alexis", request); contract.APIErrorCode(err) != contract.ErrorValidationFailed {
		t.Fatalf("fifth photo should be rejected, err=%v", err)
	}
}

func TestFacePathTraversalAndNonAdminProtection(t *testing.T) {
	store := newFaceStore(t.TempDir())
	if _, ok := store.safeBasePath("alexis", filepath.Join(store.root, "alexis", "pending", "evil.jpg")); ok {
		t.Fatal("pending path escaped base directory")
	}
	if _, ok := store.safeBasePath("alexis", filepath.Join(store.root, "alexis", "base", "..", "evil.jpg")); ok {
		t.Fatal("path traversal was accepted")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/residents/alexis/face", nil)
	recorder := httptest.NewRecorder()
	handleResidentFaceRoute(&faceTestProvider{resident: map[string]any{"id": "alexis"}}, store).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin face access status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestFaceProfileScansCanonicalAutoAndReviewDirectories(t *testing.T) {
	root := t.TempDir()
	store := newFaceStore(root)
	provider := &faceTestProvider{resident: map[string]any{"id": "alexis"}}
	for _, directory := range []string{"auto", "review"} {
		path := filepath.Join(root, "alexis", directory)
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, directory+"-one.jpg"), []byte{0xff, 0xd8, 0xff, 0xe0}, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := store.profile(provider, "alexis")
	if err != nil {
		t.Fatal(err)
	}
	if profile.AutoCount != 1 || profile.ReviewCount != 1 || profile.PendingCount != 1 {
		t.Fatalf("unexpected canonical counts: %#v", profile)
	}
	review, err := store.listPhotos("alexis", "review")
	if err != nil || len(review) != 1 {
		t.Fatalf("review scan failed: %#v err=%v", review, err)
	}
	if _, ok := store.safeFacePath("alexis", "review", filepath.Join(root, "alexis", "base", "escape.jpg")); ok {
		t.Fatal("review path escaped canonical directory")
	}
}
