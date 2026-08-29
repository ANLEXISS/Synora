package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	webapi "synora/internal/api"
	"synora/pkg/contract"
)

type photoHTTPProvider struct {
	faceTestProvider
	photos []contract.FacePhoto
}

func (p *photoHTTPProvider) RequestFaceDatasetRebuild() (*contract.FaceDatasetState, error) {
	return &contract.FaceDatasetState{SchemaVersion: 1, Status: contract.FaceDatasetBuilding}, nil
}

func (p *photoHTTPProvider) FaceDatasetStatus() (*contract.FaceDatasetState, error) {
	return &contract.FaceDatasetState{SchemaVersion: 1, Status: contract.FaceDatasetActive}, nil
}

func (p *photoHTTPProvider) Residents() ([]map[string]any, error) {
	return []map[string]any{p.resident}, nil
}
func (p *photoHTTPProvider) CreateResident(json.RawMessage) (map[string]any, error) {
	return p.resident, nil
}
func (p *photoHTTPProvider) UpdateResident(id string, data json.RawMessage) (map[string]any, error) {
	return p.faceTestProvider.UpdateResident(id, data)
}
func (p *photoHTTPProvider) DeleteResident(string) (map[string]any, error) { return p.resident, nil }

func (p *photoHTTPProvider) ResidentPhotos(id string, _ int) ([]contract.FacePhoto, error) {
	out := []contract.FacePhoto{}
	for _, photo := range p.photos {
		if photo.ResidentID == id {
			out = append(out, photo)
		}
	}
	return out, nil
}
func (p *photoHTTPProvider) ResidentPhoto(id string) (*contract.FacePhoto, error) {
	for _, photo := range p.photos {
		if photo.ID == id {
			copy := photo
			return &copy, nil
		}
	}
	return nil, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
}
func (p *photoHTTPProvider) RegisterResidentPhoto(photo contract.FacePhoto) (*contract.FacePhoto, error) {
	for _, existing := range p.photos {
		if existing.ResidentID == photo.ResidentID && existing.Checksum == photo.Checksum {
			return &existing, nil
		}
	}
	p.photos = append(p.photos, photo)
	return &photo, nil
}
func (p *photoHTTPProvider) DeleteResidentPhoto(_ string, id string) (*contract.FacePhoto, error) {
	for _, photo := range p.photos {
		if photo.ID == id {
			photo.Status = string(contract.FacePhotoRemovalPending)
			return &photo, nil
		}
	}
	return nil, contract.NewAPIError(contract.ErrorNotFound, "face photo not found")
}

func adminPhotoRequest(req *http.Request) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), authPrincipalContextKey{}, webapi.AuthUser{Role: webapi.RoleAdmin, Permissions: webapi.PermissionsForRole(webapi.RoleAdmin)}))
}

func TestResidentPhotoRouteReturnsMetadataWithoutInternalPath(t *testing.T) {
	provider := &photoHTTPProvider{faceTestProvider: faceTestProvider{resident: map[string]any{"id": "alexis"}}}
	store := newFaceStore(t.TempDir())
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "client-provided-name.png")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/residents/alexis/photos", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req = adminPhotoRequest(req)
	rec := httptest.NewRecorder()
	handleResidentRoute(provider, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if _, ok := response["path"]; ok {
		t.Fatalf("path leaked: %s", rec.Body.String())
	}
	if _, ok := response["storage_key"]; ok {
		t.Fatalf("storage key leaked: %s", rec.Body.String())
	}
	if response["status"] != "stored" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestLegacyFaceBaseUsesCanonicalValidatedSourceAndRebuildState(t *testing.T) {
	provider := &photoHTTPProvider{faceTestProvider: faceTestProvider{resident: map[string]any{"id": "alexis"}}}
	store := newFaceStore(t.TempDir())
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("view", "face"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "hostile-name.bin")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(part, img); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := adminPhotoRequest(httptest.NewRequest(http.MethodPost, "/api/residents/alexis/face/base", &body))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handleResidentRoute(provider, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || len(provider.photos) != 1 {
		t.Fatalf("status=%d body=%s photos=%#v", rec.Code, rec.Body.String(), provider.photos)
	}
	photo := provider.photos[0]
	if photo.View != "face" || photo.StorageKey == "" {
		t.Fatalf("canonical metadata=%#v", photo)
	}
	if _, err := os.Stat(filepath.Join(store.root, "sources", "alexis", photo.Filename)); err != nil {
		t.Fatalf("canonical source missing: %v", err)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("storage_key")) || bytes.Contains(rec.Body.Bytes(), []byte(store.root)) {
		t.Fatalf("private source leaked: %s", rec.Body.String())
	}

	rebuild := adminPhotoRequest(httptest.NewRequest(http.MethodPost, "/api/residents/alexis/face/rebuild", nil))
	rebuildRec := httptest.NewRecorder()
	handleResidentRoute(provider, store).ServeHTTP(rebuildRec, rebuild)
	if rebuildRec.Code != http.StatusAccepted || !bytes.Contains(rebuildRec.Body.Bytes(), []byte(`"status":"building"`)) {
		t.Fatalf("rebuild status=%d body=%s", rebuildRec.Code, rebuildRec.Body.String())
	}
}

func TestResidentPhotoUploadRejectsMultipleParts(t *testing.T) {
	provider := &photoHTTPProvider{faceTestProvider: faceTestProvider{resident: map[string]any{"id": "alexis"}}}
	store := newFaceStore(t.TempDir())
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := 0; i < 2; i++ {
		part, err := writer.CreateFormFile("file", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(part, img); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := adminPhotoRequest(httptest.NewRequest(http.MethodPost, "/api/residents/alexis/photos", &body))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handleResidentRoute(provider, store).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || len(provider.photos) != 0 {
		t.Fatalf("multiple upload status=%d body=%s photos=%#v", rec.Code, rec.Body.String(), provider.photos)
	}
}
