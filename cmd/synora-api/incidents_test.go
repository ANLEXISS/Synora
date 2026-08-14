package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	webapi "synora/internal/api"
	"synora/pkg/contract"
)

type fakeIncidentProvider struct {
	items       []contract.Incident
	item        *contract.Incident
	viewed      *contract.Incident
	acknowledge *contract.Incident
	err         error
}

func (f *fakeIncidentProvider) Incidents(limit int) ([]contract.Incident, error) {
	if f.err != nil {
		return nil, f.err
	}
	if limit < len(f.items) {
		return f.items[:limit], nil
	}
	return f.items, nil
}

func (f *fakeIncidentProvider) Incident(string) (*contract.Incident, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.item, nil
}

func (f *fakeIncidentProvider) MarkIncidentViewed(string) (*contract.Incident, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.viewed, nil
}

func (f *fakeIncidentProvider) AcknowledgeIncident(string) (*contract.Incident, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.acknowledge, nil
}

func TestIncidentAPIRoutesAndErrorStatuses(t *testing.T) {
	item := &contract.Incident{ID: "incident-1", Status: contract.IncidentStatusNew, CreatedAt: time.Now().UTC()}
	provider := &fakeIncidentProvider{
		items: []contract.Incident{*item}, item: item,
		viewed:      &contract.Incident{ID: item.ID, Status: contract.IncidentStatusViewed},
		acknowledge: &contract.Incident{ID: item.ID, Status: contract.IncidentStatusAcknowledged},
	}

	list := httptest.NewRecorder()
	handleIncidentCollection(provider).ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/incidents?limit=10", nil))
	if list.Code != http.StatusOK || list.Header().Get("Content-Type") == "" {
		t.Fatalf("incident list status=%d content-type=%q body=%s", list.Code, list.Header().Get("Content-Type"), list.Body.String())
	}
	invalidLimit := httptest.NewRecorder()
	handleIncidentCollection(provider).ServeHTTP(invalidLimit, httptest.NewRequest(http.MethodGet, "/api/incidents?limit=0", nil))
	if invalidLimit.Code != http.StatusBadRequest {
		t.Fatalf("invalid incident limit should return 400: status=%d body=%s", invalidLimit.Code, invalidLimit.Body.String())
	}

	detail := httptest.NewRecorder()
	handleIncidentRoute(provider).ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/incidents/incident-1", nil))
	if detail.Code != http.StatusOK {
		t.Fatalf("incident detail status=%d body=%s", detail.Code, detail.Body.String())
	}

	view := httptest.NewRecorder()
	handleIncidentRoute(provider).ServeHTTP(view, httptest.NewRequest(http.MethodPost, "/api/incidents/incident-1/view", nil))
	if view.Code != http.StatusOK || provider.viewed.Status != contract.IncidentStatusViewed {
		t.Fatalf("incident view status=%d body=%s", view.Code, view.Body.String())
	}

	ack := httptest.NewRecorder()
	handleIncidentRoute(provider).ServeHTTP(ack, httptest.NewRequest(http.MethodPost, "/api/incidents/incident-1/acknowledge", nil))
	if ack.Code != http.StatusOK || provider.acknowledge.Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("incident acknowledge status=%d body=%s", ack.Code, ack.Body.String())
	}

	method := httptest.NewRecorder()
	handleIncidentCollection(provider).ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/api/incidents", nil))
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("collection POST should be rejected: status=%d body=%s", method.Code, method.Body.String())
	}

	provider.err = contract.NewAPIError(contract.ErrorConflict, "transition not allowed")
	conflict := httptest.NewRecorder()
	handleIncidentRoute(provider).ServeHTTP(conflict, httptest.NewRequest(http.MethodPost, "/api/incidents/incident-1/view", nil))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict should map to 409: status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestRequiredIncidentPermissions(t *testing.T) {
	get := httptest.NewRequest(http.MethodGet, "/api/incidents/incident-1", nil)
	if permission := requiredAPIPermission(get); permission != webapi.PermissionStateRead {
		t.Fatalf("incident GET permission=%q", permission)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/incidents/incident-1/acknowledge", nil)
	if permission := requiredAPIPermission(post); permission != webapi.PermissionSecurityAdmin {
		t.Fatalf("incident POST permission=%q", permission)
	}
}
