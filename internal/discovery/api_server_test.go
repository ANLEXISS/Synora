package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/bus"
	"synora/internal/cognitivecore"
	"synora/internal/security"
	"synora/pkg/contract"
)

type fakeResidentGalleryService struct{ lastRef string }

func (f *fakeResidentGalleryService) Create(_ context.Context, ref, _ string) (ResidentGalleryView, bool, error) {
	f.lastRef = ref
	return ResidentGalleryView{ResidentRef: ref, GalleryStatus: "not_enrolled", PolicyVersion: "face-gallery-policy/v1"}, false, nil
}

func TestResidentGalleryHTTPDiscoveryCoreStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	socket := filepath.Join(root, "bus.sock")
	server := bus.NewServerWithConfig(socket, bus.ServerConfig{AllowTestProcess: true})
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start() }()
	defer server.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test bus did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	coreClient, err := bus.NewClient(socket, "core")
	if err != nil {
		t.Fatal(err)
	}
	defer coreClient.Close()
	discoveryClient, err := bus.NewClient(socket, "discovery")
	if err != nil {
		t.Fatal(err)
	}
	defer discoveryClient.Close()
	store := cognitivecore.NewUniversalStore()
	coreService := &cognitivecore.Service{Bus: coreClient, Core: &cognitivecore.Core{Store: store}, Name: "core"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = coreService.Run(ctx) }()

	adminToken, err := security.RandomHex(32)
	if err != nil {
		t.Fatal(err)
	}
	config := &security.Config{APITokenScopes: map[string][]string{security.HashSecret(adminToken): {"resident:write", "face_gallery:manage"}}}
	handler := NewExternalAPI(config, &Boundary{DryRun: true, Store: NewSnapshotCache()}, busEventPublisher{client: discoveryClient}, nil, nil, busResidentGalleryService{client: discoveryClient})
	create := httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{}`))
	create.Header.Set("Authorization", "Bearer "+adminToken)
	create.Header.Set("Idempotency-Key", "http-core-create-0001")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("HTTP resident create did not reach Core: %d %s", created.Code, created.Body.String())
	}
	var projection ResidentGalleryView
	if err := json.Unmarshal(created.Body.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if projection.GalleryStatus != "not_enrolled" || projection.ResidentRef == "" {
		t.Fatalf("unexpected redacted create result: %+v", projection)
	}
	stored, exists := store.ResidentGallery(projection.ResidentRef)
	if !exists || stored.GalleryStatus != "not_enrolled" {
		t.Fatalf("Core Universal Store missed API resident: %+v exists=%t", stored, exists)
	}
	retry := httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{}`))
	retry.Header.Set("Authorization", "Bearer "+adminToken)
	retry.Header.Set("Idempotency-Key", "http-core-create-0001")
	retried := httptest.NewRecorder()
	handler.ServeHTTP(retried, retry)
	var retriedProjection map[string]any
	if retried.Code != http.StatusCreated || json.Unmarshal(retried.Body.Bytes(), &retriedProjection) != nil || retriedProjection["resident_ref"] != projection.ResidentRef || retriedProjection["duplicate"] != true {
		t.Fatalf("API/Core idempotence failed: %d %s", retried.Code, retried.Body.String())
	}

	status := httptest.NewRequest(http.MethodGet, "/api/residents/"+projection.ResidentRef+"/face-gallery/status", nil)
	status.Header.Set("Authorization", "Bearer "+adminToken)
	statusResult := httptest.NewRecorder()
	handler.ServeHTTP(statusResult, status)
	if statusResult.Code != http.StatusOK || strings.Contains(statusResult.Body.String(), "embedding") || strings.Contains(statusResult.Body.String(), "name") || strings.Contains(statusResult.Body.String(), root) {
		t.Fatalf("Discovery status was not redacted: %d %s", statusResult.Code, statusResult.Body.String())
	}
	rollback := httptest.NewRequest(http.MethodPost, "/api/residents/"+projection.ResidentRef+"/face-gallery/rollback", strings.NewReader(`{}`))
	rollback.Header.Set("Authorization", "Bearer "+adminToken)
	rolled := httptest.NewRecorder()
	handler.ServeHTTP(rolled, rollback)
	if rolled.Code != http.StatusOK {
		t.Fatalf("Core gallery rollback failed: %d %s", rolled.Code, rolled.Body.String())
	}
	remove := httptest.NewRequest(http.MethodDelete, "/api/residents/"+projection.ResidentRef, nil)
	remove.Header.Set("Authorization", "Bearer "+adminToken)
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, remove)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"gallery_status":"disabled"`) {
		t.Fatalf("Core logical deletion failed: %d %s", removed.Code, removed.Body.String())
	}
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("test bus failed: %v", err)
		}
	default:
	}
}
func (f *fakeResidentGalleryService) Status(_ context.Context, ref string) (ResidentGalleryView, error) {
	return ResidentGalleryView{ResidentRef: ref, GalleryStatus: "not_enrolled", PolicyVersion: "face-gallery-policy/v1"}, nil
}
func (f *fakeResidentGalleryService) Rollback(_ context.Context, ref string) (ResidentGalleryView, error) {
	return ResidentGalleryView{ResidentRef: ref, GalleryStatus: "not_enrolled", PolicyVersion: "face-gallery-policy/v1"}, nil
}
func (f *fakeResidentGalleryService) Delete(_ context.Context, ref string) (ResidentGalleryView, error) {
	return ResidentGalleryView{ResidentRef: ref, GalleryStatus: "disabled", PolicyVersion: "face-gallery-policy/v1"}, nil
}

type recordingPublisher struct{ events []contract.Event }

func (p *recordingPublisher) Publish(event contract.Event) error {
	p.events = append(p.events, event)
	return nil
}

func TestExternalAPIUsesDiscoveryAuthAndEventBoundary(t *testing.T) {
	cfg := &security.Config{APITokenHash: security.HashSecret("token")}
	cache := NewSnapshotCache()
	publisher := &recordingPublisher{}
	handler := NewExternalAPI(cfg, &Boundary{DryRun: true, Store: cache}, publisher, nil, nil)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/state", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(`{"command":"refresh"}`))
	request.Header.Set("Authorization", "Bearer token")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusAccepted || len(publisher.events) != 1 {
		t.Fatalf("command status=%d events=%d body=%s", accepted.Code, len(publisher.events), accepted.Body.String())
	}
	if publisher.events[0].Source != "discovery" || publisher.events[0].Type != WebCommandEvent {
		t.Fatalf("unexpected event: %#v", publisher.events[0])
	}
}

func TestExternalAPIHealthIsDiscoveryOwned(t *testing.T) {
	handler := NewExternalAPI(&security.Config{}, &Boundary{DryRun: true, Store: NewSnapshotCache()}, &recordingPublisher{}, nil, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("private health status=%d", recorder.Code)
	}
	public := &security.Config{PublicSystemHealth: true}
	recorder = httptest.NewRecorder()
	NewExternalAPI(public, &Boundary{DryRun: true, Store: NewSnapshotCache()}, &recordingPublisher{}, nil, nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"api_owner":"discovery"`) {
		t.Fatalf("public health status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestResidentGalleryAdminScopesAndRedactedRoutes(t *testing.T) {
	adminToken, err := security.RandomHex(32)
	if err != nil {
		t.Fatal(err)
	}
	readerToken, err := security.RandomHex(32)
	if err != nil {
		t.Fatal(err)
	}
	legacyToken, err := security.RandomHex(32)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &security.Config{
		APITokenHash: security.HashSecret(legacyToken),
		APITokenScopes: map[string][]string{
			security.HashSecret(adminToken):  {"resident:write", "face_gallery:manage"},
			security.HashSecret(readerToken): {"resident:read"},
		},
	}
	service := &fakeResidentGalleryService{}
	handler := NewExternalAPI(cfg, &Boundary{DryRun: true, Store: NewSnapshotCache()}, &recordingPublisher{}, nil, nil, service)

	withoutToken := httptest.NewRecorder()
	handler.ServeHTTP(withoutToken, httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{}`)))
	if withoutToken.Code != http.StatusUnauthorized || strings.Contains(strings.ToLower(withoutToken.Body.String()), "scope") {
		t.Fatalf("unauthenticated response leaked details or was accepted: %d %s", withoutToken.Code, withoutToken.Body.String())
	}

	noScope := httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{}`))
	noScope.Header.Set("Authorization", "Bearer "+legacyToken)
	noScope.Header.Set("Idempotency-Key", "create-test-0001")
	noScopeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(noScopeRecorder, noScope)
	if noScopeRecorder.Code != http.StatusForbidden || strings.Contains(strings.ToLower(noScopeRecorder.Body.String()), "resident:") {
		t.Fatalf("unscoped token response=%d %s", noScopeRecorder.Code, noScopeRecorder.Body.String())
	}

	create := httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{}`))
	create.Header.Set("Authorization", "Bearer "+adminToken)
	create.Header.Set("Idempotency-Key", "create-test-0002")
	created := httptest.NewRecorder()
	handler.ServeHTTP(created, create)
	if created.Code != http.StatusCreated || service.lastRef == "" || !residentRefPattern.MatchString(service.lastRef) {
		t.Fatalf("scoped create failed: status=%d body=%s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "name") || strings.Contains(created.Body.String(), "embedding") || strings.Contains(created.Body.String(), "/") {
		t.Fatalf("create response is not redacted: %s", created.Body.String())
	}

	status := httptest.NewRequest(http.MethodGet, "/api/residents/"+service.lastRef+"/face-gallery/status", nil)
	status.Header.Set("Authorization", "Bearer "+readerToken)
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, status)
	var projection map[string]any
	if statusRecorder.Code != http.StatusOK || json.Unmarshal(statusRecorder.Body.Bytes(), &projection) != nil {
		t.Fatalf("redacted status unavailable: %d %s", statusRecorder.Code, statusRecorder.Body.String())
	}
	for _, forbidden := range []string{"name", "identity", "path", "embedding", "score", "image"} {
		if _, exists := projection[forbidden]; exists {
			t.Fatalf("status exposed forbidden field %q: %v", forbidden, projection)
		}
	}

	rollback := httptest.NewRequest(http.MethodPost, "/api/residents/"+service.lastRef+"/face-gallery/rollback", strings.NewReader(`{}`))
	rollback.Header.Set("Authorization", "Bearer "+readerToken)
	rollbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rollbackRecorder, rollback)
	if rollbackRecorder.Code != http.StatusForbidden {
		t.Fatalf("read-only scope performed rollback: %d", rollbackRecorder.Code)
	}
	rollback = httptest.NewRequest(http.MethodPost, "/api/residents/"+service.lastRef+"/face-gallery/rollback", strings.NewReader(`{}`))
	rollback.Header.Set("Authorization", "Bearer "+adminToken)
	rollbackRecorder = httptest.NewRecorder()
	handler.ServeHTTP(rollbackRecorder, rollback)
	if rollbackRecorder.Code != http.StatusOK {
		t.Fatalf("explicit administration scopes could not rollback: %d %s", rollbackRecorder.Code, rollbackRecorder.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/api/residents/"+service.lastRef, nil)
	deleteRequest.Header.Set("Authorization", "Bearer "+adminToken)
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK || !strings.Contains(deleteRecorder.Body.String(), `"gallery_status":"disabled"`) {
		t.Fatalf("logical resident delete failed: %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	badPayload := httptest.NewRequest(http.MethodPost, "/api/residents", strings.NewReader(`{"name":"private"}`))
	badPayload.Header.Set("Authorization", "Bearer "+adminToken)
	badPayload.Header.Set("Idempotency-Key", "create-test-0003")
	badRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badRecorder, badPayload)
	if badRecorder.Code != http.StatusBadRequest {
		t.Fatalf("resident API accepted identity payload: %d", badRecorder.Code)
	}
}
