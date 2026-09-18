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
