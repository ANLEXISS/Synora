package discovery

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

type webPublisher struct{ events []contract.Event }

func (p *webPublisher) Publish(event contract.Event) error {
	p.events = append(p.events, event)
	return nil
}

type webStore struct{}

func (webStore) SnapshotJSON() ([]byte, error) {
	return []byte(`{"schema_version":"cognitive-snapshot/v1"}`), nil
}

func (webStore) HistoryJSON() ([]byte, error) {
	return []byte(`[{"revision":1}]`), nil
}

func TestWebCommandBecomesCoreEventAndStateIsReadOnly(t *testing.T) {
	publisher := &webPublisher{}
	api := &WebAPI{Boundary: &Boundary{DryRun: true, Store: webStore{}, Now: func() time.Time { return time.Unix(60, 0).UTC() }}, Publisher: publisher}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/api/v1/commands", "application/json", strings.NewReader(`{"command":"review","arguments":{"topology":"unknown"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 202 || len(publisher.events) != 1 || publisher.events[0].Type != WebCommandEvent {
		t.Fatalf("unexpected Web command result status=%d events=%#v", response.StatusCode, publisher.events)
	}
	response, err = server.Client().Get(server.URL + "/api/v1/state")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("state read failed: %d", response.StatusCode)
	}
}

func TestDiscoveryWebSurfaceReadsHistoryHealthAndPublishesMessages(t *testing.T) {
	publisher := &webPublisher{}
	api := &WebAPI{Boundary: &Boundary{DryRun: true, Store: webStore{}, Now: func() time.Time { return time.Unix(60, 0).UTC() }}, Publisher: publisher, Health: func() map[string]any {
		return map[string]any{"service": "discovery", "status": "ok", "source": "discovery"}
	}}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	for _, path := range []string{"/api/v1/history", "/api/v1/health", "/api/v1/subscribe"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			t.Fatalf("GET %s status=%d", path, response.StatusCode)
		}
		_ = response.Body.Close()
	}
	response, err := server.Client().Post(server.URL+"/api/v1/messages", "application/json", strings.NewReader(`{"id":"message-1","type":"sensor.motion","source":"external","payload":{"movement":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 202 || len(publisher.events) != 1 || publisher.events[0].Source != "discovery" {
		t.Fatalf("message was not normalized through Discovery: status=%d events=%#v", response.StatusCode, publisher.events)
	}
	_ = response.Body.Close()
}

func TestDiscoveryWebSurfaceRejectsRawMessage(t *testing.T) {
	publisher := &webPublisher{}
	api := &WebAPI{Boundary: &Boundary{DryRun: true, Store: webStore{}}, Publisher: publisher}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/api/v1/messages", "application/json", strings.NewReader(`{"id":"message-raw","type":"vision","payload":{"frame":"raw"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 400 || len(publisher.events) != 0 {
		t.Fatalf("raw message crossed Discovery boundary: status=%d events=%#v", response.StatusCode, publisher.events)
	}
	_ = response.Body.Close()
}
