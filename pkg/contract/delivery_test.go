package contract

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestDeliveryIdentityAndRecordAreDeterministic(t *testing.T) {
	m := Message{ID: "msg-1", Epoch: "epoch-1", Sequence: 7, Revision: 4, Type: EventVisionMotion, Source: "core"}
	record := DeliveryRecord{
		Identity:  m.DeliveryIdentity(),
		Message:   m,
		State:     DeliveryPending,
		CreatedAt: time.Unix(10, 0).UTC(),
		UpdatedAt: time.Unix(10, 0).UTC(),
	}
	if err := record.Validate(); err != nil {
		t.Fatalf("record should validate: %v", err)
	}
	if got, want := record.Identity.Key(), "epoch-1/msg-1/7/4"; got != want {
		t.Fatalf("identity key=%q want %q", got, want)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	var decoded DeliveryRecord
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	if decoded.Identity != record.Identity || !reflect.DeepEqual(decoded.Message, record.Message) || decoded.State != DeliveryPending {
		t.Fatalf("record changed across serialization: %#v", decoded)
	}
}

func TestDeliveryAckRequiresMatchingTerminalOutcome(t *testing.T) {
	identity := DeliveryIdentity{ID: "msg-1", Epoch: "epoch-1", Sequence: 1}
	for _, state := range []DeliveryState{DeliveryAcknowledged, DeliveryFailed, DeliveryQuarantined} {
		if err := (DeliveryAck{Identity: identity, State: state}).Validate(); err != nil {
			t.Fatalf("ack state %q rejected: %v", state, err)
		}
	}
	if err := (DeliveryAck{Identity: identity, State: DeliveryRetryWait}).Validate(); err == nil {
		t.Fatal("retry state must not be accepted as an ACK")
	}
}

func TestDeliveryStateTransitionsAreFailClosed(t *testing.T) {
	valid := [][2]DeliveryState{
		{DeliveryPending, DeliveryInFlight},
		{DeliveryInFlight, DeliveryRetryWait},
		{DeliveryRetryWait, DeliveryInFlight},
		{DeliveryInFlight, DeliveryAcknowledged},
		{DeliveryInFlight, DeliveryFailed},
	}
	for _, pair := range valid {
		if err := NextDeliveryState(pair[0], pair[1]); err != nil {
			t.Fatalf("transition %q -> %q rejected: %v", pair[0], pair[1], err)
		}
	}
	for _, pair := range [][2]DeliveryState{
		{DeliveryAcknowledged, DeliveryInFlight},
		{DeliveryFailed, DeliveryPending},
		{DeliveryPending, DeliveryAcknowledged},
		{DeliveryRetryWait, DeliveryAcknowledged},
	} {
		if err := NextDeliveryState(pair[0], pair[1]); err == nil {
			t.Fatalf("invalid transition %q -> %q was accepted", pair[0], pair[1])
		}
	}
}

func TestMessageWireShapeRemainsCompatible(t *testing.T) {
	m := Message{ID: "legacy", Type: EventVisionMotion, Source: "vision", Kind: KindEvent}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if decoded.ID != m.ID || decoded.Type != m.Type || decoded.Source != m.Source || decoded.Kind != m.Kind {
		t.Fatalf("legacy message changed across serialization: %#v", decoded)
	}
}
