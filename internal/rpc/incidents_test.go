package rpc

import (
	"encoding/json"
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

func TestIncidentRPCListLifecycleAndErrors(t *testing.T) {
	store := state.NewStore()
	item := contract.Incident{
		ID: "incident-rpc", Status: contract.IncidentStatusNew,
		CreatedAt:    time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 8, 2, 14, 0, 0, 0, time.UTC),
		IdentityKind: contract.IncidentIdentityUnknown, SecurityState: "intrusion",
	}
	store.SetIncident(&item)
	server := NewServer(Config{State: store})

	listed, err := server.Handler("incidents.list")(rpcMessage(`{"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var incidents []contract.Incident
	encoded, _ := json.Marshal(listed)
	if err := json.Unmarshal(encoded, &incidents); err != nil || len(incidents) != 1 || incidents[0].ID != item.ID {
		t.Fatalf("unexpected incident list=%s err=%v", encoded, err)
	}

	viewed, err := server.Handler("incidents.mark_viewed")(rpcMessage(`{"id":"incident-rpc"}`))
	if err != nil || viewed.(contract.Incident).Status != contract.IncidentStatusViewed {
		t.Fatalf("viewed=%#v err=%v", viewed, err)
	}
	acknowledged, err := server.Handler("incidents.acknowledge")(rpcMessage(`{"id":"incident-rpc"}`))
	if err != nil || acknowledged.(contract.Incident).Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("acknowledged=%#v err=%v", acknowledged, err)
	}
	if _, err := server.Handler("incidents.mark_viewed")(rpcMessage(`{"id":"incident-rpc"}`)); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("expected conflict for reverse transition, err=%v", err)
	}
	if _, err := server.Handler("incidents.get")(rpcMessage(`{"id":"missing"}`)); contract.APIErrorCode(err) != contract.ErrorNotFound {
		t.Fatalf("expected not found, err=%v", err)
	}
}

func TestIncidentRPCPublishesOnlyRealTransitions(t *testing.T) {
	store := state.NewStore()
	store.SetIncident(&contract.Incident{ID: "incident-publish", Status: contract.IncidentStatusNew})
	var published []contract.RealtimeIncidentUpdatedPayload
	server := NewServer(Config{
		State: store,
		PublishEvent: func(eventType string, payload any, _ int) {
			if eventType != "incident.updated" {
				t.Fatalf("unexpected event type %q", eventType)
			}
			value, ok := payload.(contract.RealtimeIncidentUpdatedPayload)
			if !ok {
				t.Fatalf("unexpected publication payload %#v", payload)
			}
			published = append(published, value)
		},
	})

	if _, err := server.Handler("incidents.mark_viewed")(rpcMessage(`{"id":"incident-publish"}`)); err != nil {
		t.Fatalf("mark viewed: %v", err)
	}
	if _, err := server.Handler("incidents.mark_viewed")(rpcMessage(`{"id":"incident-publish"}`)); err != nil {
		t.Fatalf("idempotent mark viewed: %v", err)
	}
	if _, err := server.Handler("incidents.acknowledge")(rpcMessage(`{"id":"incident-publish"}`)); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if _, err := server.Handler("incidents.acknowledge")(rpcMessage(`{"id":"incident-publish"}`)); err != nil {
		t.Fatalf("idempotent acknowledge: %v", err)
	}
	if len(published) != 2 || published[0].Reason != "viewed" || published[1].Reason != "acknowledged" {
		t.Fatalf("only real transitions should publish: %#v", published)
	}

	if _, err := server.Handler("incidents.acknowledge")(rpcMessage(`{"id":"missing"}`)); contract.APIErrorCode(err) != contract.ErrorNotFound {
		t.Fatalf("missing incident error=%v", err)
	}
	if len(published) != 2 {
		t.Fatalf("rejected mutation should not publish: %#v", published)
	}
}
