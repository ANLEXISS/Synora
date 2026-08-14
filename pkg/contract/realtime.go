package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const RealtimeSchemaVersion = "synora.realtime.v1"

type RealtimeMessageType string

const (
	RealtimeConnectionReady      RealtimeMessageType = "connection.ready"
	RealtimeSnapshot             RealtimeMessageType = "snapshot"
	RealtimeSecurityStateChanged RealtimeMessageType = "security_state.changed"
	RealtimeIncidentCreated      RealtimeMessageType = "incident.created"
	RealtimeIncidentUpdated      RealtimeMessageType = "incident.updated"
	RealtimeClipAvailable        RealtimeMessageType = "clip.available"
	RealtimeResyncRequired       RealtimeMessageType = "resync_required"
)

func (kind RealtimeMessageType) Validate() error {
	if strings.TrimSpace(string(kind)) == "" {
		return fmt.Errorf("realtime message type is required")
	}
	return nil
}

// RealtimeEnvelope is the public server-to-client wire contract. Payload is a
// typed JSON object selected by Type; it is intentionally raw at this shared
// boundary so Core and API do not duplicate one another's business ownership.
type RealtimeEnvelope struct {
	SchemaVersion string              `json:"schema_version"`
	Type          RealtimeMessageType `json:"type"`
	MessageID     string              `json:"message_id"`
	OccurredAt    time.Time           `json:"occurred_at"`
	Source        string              `json:"source"`
	Epoch         string              `json:"epoch"`
	Sequence      uint64              `json:"sequence"`
	Revision      uint64              `json:"revision,omitempty"`
	Payload       json.RawMessage     `json:"payload"`
}

func (envelope RealtimeEnvelope) Validate() error {
	if envelope.SchemaVersion != RealtimeSchemaVersion {
		return fmt.Errorf("unsupported realtime schema version %q", envelope.SchemaVersion)
	}
	if err := envelope.Type.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(envelope.MessageID) == "" || strings.TrimSpace(envelope.Source) == "" || strings.TrimSpace(envelope.Epoch) == "" {
		return fmt.Errorf("realtime envelope requires message_id, source and epoch")
	}
	if envelope.OccurredAt.IsZero() || envelope.Sequence == 0 {
		return fmt.Errorf("realtime envelope requires occurred_at and positive sequence")
	}
	if len(envelope.Payload) == 0 || !json.Valid(envelope.Payload) {
		return fmt.Errorf("realtime envelope payload must be valid JSON")
	}
	return nil
}

type RealtimeConnectionReadyPayload struct {
	ServerTime time.Time `json:"server_time"`
	Epoch      string    `json:"epoch"`
	Sequence   uint64    `json:"sequence"`
}

type RealtimeSnapshotPayload struct {
	Snapshot PublicSnapshot `json:"snapshot"`
	Reason   string         `json:"reason,omitempty"`
}

type RealtimeSecurityStateChangedPayload struct {
	State map[string]any `json:"state"`
}

type RealtimeIncidentCreatedPayload struct {
	Incident Incident `json:"incident"`
}

type RealtimeIncidentUpdatedPayload struct {
	IncidentID string         `json:"incident_id"`
	Revision   uint64         `json:"revision"`
	Status     IncidentStatus `json:"status"`
	Reason     string         `json:"reason"`
	Incident   Incident       `json:"incident"`
}

type RealtimeClipAvailablePayload struct {
	ClipID      string     `json:"clip_id"`
	CameraID    string     `json:"camera_id"`
	NodeID      string     `json:"node_id,omitempty"`
	Status      ClipStatus `json:"status"`
	Revision    uint64     `json:"revision"`
	IncidentIDs []string   `json:"incident_ids,omitempty"`
}

type RealtimeResyncRequiredPayload struct {
	Reason            string `json:"reason"`
	RequestedEpoch    string `json:"requested_epoch,omitempty"`
	RequestedSequence uint64 `json:"requested_sequence,omitempty"`
}
