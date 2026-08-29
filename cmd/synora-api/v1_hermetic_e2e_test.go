package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

type durableIncidentProvider struct {
	store *state.Store
}

func (p durableIncidentProvider) Incidents(limit int) ([]contract.Incident, error) {
	return p.store.IncidentsList(limit), nil
}

func (p durableIncidentProvider) Incident(id string) (*contract.Incident, error) {
	incident, ok := p.store.Incident(id)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "incident not found")
	}
	return incident, nil
}

func (p durableIncidentProvider) MarkIncidentViewed(id string) (*contract.Incident, error) {
	incident, ok, err := p.store.MarkIncidentViewed(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return p.Incident(id)
	}
	return &incident, nil
}

func (p durableIncidentProvider) AcknowledgeIncident(id string) (*contract.Incident, error) {
	incident, ok, err := p.store.AcknowledgeIncident(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return p.Incident(id)
	}
	return &incident, nil
}

func TestV1HermeticIncidentAPIReadsAndAcknowledgesPersistedIncident(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	store := state.NewStore(state.WithPersistencePath(statePath))
	when := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	incident, created, _, err := store.RecordIncident(state.IncidentObservation{
		EventID: "evt-hermetic-unknown", EventType: contract.EventVisionUnknown,
		Timestamp: when, CameraID: "cam_01", NodeID: "entry", ClipID: "clip-unknown",
		TrackID: "track-unknown", IdentityKind: contract.IncidentIdentityUnknown,
		SecurityState: "intrusion", Severity: "critical", Score: 0.91,
		Cause: contract.IncidentCause{EventType: contract.EventVisionUnknown},
	})
	if err != nil || !created || incident.ID == "" {
		t.Fatalf("failed to create durable fixture: incident=%#v created=%t err=%v", incident, created, err)
	}

	provider := durableIncidentProvider{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/incidents", handleIncidentCollection(provider))
	mux.HandleFunc("/api/incidents/", handleIncidentRoute(provider))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	response, err := server.Client().Get(server.URL + "/api/incidents?limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("incident list status=%d", response.StatusCode)
	}
	var listed []contract.Incident
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != incident.ID || listed[0].SecurityState != "intrusion" {
		t.Fatalf("unexpected API list: %#v", listed)
	}

	ackRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/incidents/"+incident.ID+"/acknowledge", nil)
	if err != nil {
		t.Fatal(err)
	}
	ackResponse, err := server.Client().Do(ackRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer ackResponse.Body.Close()
	if ackResponse.StatusCode != http.StatusOK {
		t.Fatalf("acknowledge status=%d", ackResponse.StatusCode)
	}
	var acknowledged contract.Incident
	if err := json.NewDecoder(ackResponse.Body).Decode(&acknowledged); err != nil {
		t.Fatal(err)
	}
	if acknowledged.Status != contract.IncidentStatusAcknowledged || acknowledged.ID != incident.ID {
		t.Fatalf("unexpected acknowledgement: %#v", acknowledged)
	}

	restored := state.NewStore(state.WithPersistencePath(statePath))
	if _, err := restored.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	if value, ok := restored.Incident(incident.ID); !ok || value.Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("acknowledgement was not durable: %#v ok=%t", value, ok)
	}
}
