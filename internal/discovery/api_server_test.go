package discovery

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"synora/internal/security"
	"synora/pkg/contract"
)

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
