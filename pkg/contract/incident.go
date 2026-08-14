package contract

import (
	"fmt"
	"strings"
	"time"
)

// IncidentStatus is the user-facing lifecycle of a persisted security incident.
type IncidentStatus string

const (
	IncidentStatusNew          IncidentStatus = "new"
	IncidentStatusViewed       IncidentStatus = "viewed"
	IncidentStatusAcknowledged IncidentStatus = "acknowledged"
	IncidentStatusResolved     IncidentStatus = "resolved"
)

func (status IncidentStatus) Validate() error {
	switch status {
	case IncidentStatusNew, IncidentStatusViewed, IncidentStatusAcknowledged, IncidentStatusResolved:
		return nil
	default:
		return fmt.Errorf("invalid incident status %q", status)
	}
}

// IncidentIdentityKind deliberately separates a known resident from an
// unknown or low-confidence subject. It is not an interface translation.
type IncidentIdentityKind string

const (
	IncidentIdentityResident  IncidentIdentityKind = "resident"
	IncidentIdentityUnknown   IncidentIdentityKind = "unknown"
	IncidentIdentityUncertain IncidentIdentityKind = "uncertain"
	IncidentIdentityNone      IncidentIdentityKind = "none"
)

func (kind IncidentIdentityKind) Validate() error {
	switch kind {
	case IncidentIdentityResident, IncidentIdentityUnknown, IncidentIdentityUncertain, IncidentIdentityNone:
		return nil
	default:
		return fmt.Errorf("invalid incident identity kind %q", kind)
	}
}

// IncidentCause contains decision facts that can be rendered by clients
// without relying on a translated free-form message.
type IncidentCause struct {
	EventType    string   `json:"event_type,omitempty"`
	DecisionType string   `json:"decision_type,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Contributors []string `json:"contributors,omitempty"`
	Evidence     []string `json:"evidence,omitempty"`
	DecisionID   string   `json:"decision_id,omitempty"`
	SequenceKey  string   `json:"sequence_key,omitempty"`
	ActivationID string   `json:"activation_id,omitempty"`
	GroupKey     string   `json:"group_key,omitempty"`
}

// IncidentTimelineEntry is a bounded, structured reference to one source
// observation. Raw event payloads remain in the existing event history.
type IncidentTimelineEntry struct {
	Key          string               `json:"key"`
	Timestamp    time.Time            `json:"timestamp"`
	Type         string               `json:"type"`
	EventID      string               `json:"event_id,omitempty"`
	CameraID     string               `json:"camera_id,omitempty"`
	NodeID       string               `json:"node_id,omitempty"`
	IdentityKind IncidentIdentityKind `json:"identity_kind"`
	ResidentID   string               `json:"resident_id,omitempty"`
	EntityID     string               `json:"entity_id,omitempty"`
	Score        float64              `json:"score,omitempty"`
	Confidence   float64              `json:"confidence,omitempty"`
}

// Incident is the durable product record created by Core for a real intrusion.
type Incident struct {
	ID             string                  `json:"id"`
	Status         IncidentStatus          `json:"status"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	StartedAt      time.Time               `json:"started_at"`
	LastEventAt    time.Time               `json:"last_event_at"`
	AcknowledgedAt *time.Time              `json:"acknowledged_at,omitempty"`
	ViewedAt       *time.Time              `json:"viewed_at,omitempty"`
	ResolvedAt     *time.Time              `json:"resolved_at,omitempty"`
	SecurityState  string                  `json:"security_state"`
	Severity       string                  `json:"severity,omitempty"`
	Cause          IncidentCause           `json:"cause"`
	Score          float64                 `json:"score"`
	CameraID       string                  `json:"camera_id,omitempty"`
	NodeID         string                  `json:"node_id,omitempty"`
	IdentityKind   IncidentIdentityKind    `json:"identity_kind"`
	ResidentID     string                  `json:"resident_id,omitempty"`
	EntityID       string                  `json:"entity_id,omitempty"`
	TrackID        string                  `json:"track_id,omitempty"`
	EventIDs       []string                `json:"event_ids,omitempty"`
	ClipIDs        []string                `json:"clip_ids,omitempty"`
	Timeline       []IncidentTimelineEntry `json:"timeline,omitempty"`
	Revision       uint64                  `json:"revision"`
}

func (incident Incident) Validate() error {
	if strings.TrimSpace(incident.ID) == "" {
		return fmt.Errorf("incident id is required")
	}
	if err := incident.Status.Validate(); err != nil {
		return err
	}
	if err := incident.IdentityKind.Validate(); err != nil {
		return err
	}
	if incident.IdentityKind != IncidentIdentityResident && incident.ResidentID != "" {
		return fmt.Errorf("resident_id requires resident identity kind")
	}
	return nil
}
