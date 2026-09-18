package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

const (
	BoundarySchemaVersion = "discovery-boundary/v1"
	WebCommandEvent       = "discovery.web.command"
	ActionResultEvent     = "discovery.action.result"
	AvailabilityEvent     = "discovery.availability"
	MaxBoundaryPayload    = 1 << 20
)

var (
	ErrForbiddenBoundaryData   = errors.New("boundary payload contains forbidden raw or identifying data")
	ErrBoundaryPayloadTooLarge = errors.New("boundary payload exceeds the V1 limit")
)

// StoreReader is deliberately read-only. Discovery can serve a view, but it
// has no interface through which it can mutate Core-owned runtime state.
type StoreReader interface {
	SnapshotJSON() ([]byte, error)
}

// ActionRequest is abstract and topological. It contains no hardware ID and
// is safe to persist in the Core outbox before Discovery resolves capability.
type ActionRequest struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	EpisodeID     string `json:"episode_id,omitempty"`
	Action        string `json:"action"`
	Topology      string `json:"topology,omitempty"`
	Capability    string `json:"capability,omitempty"`
	DryRun        bool   `json:"dry_run"`
}

type ActionResult struct {
	SchemaVersion          string `json:"schema_version"`
	RequestID              string `json:"request_id"`
	Status                 string `json:"status"`
	Reason                 string `json:"reason,omitempty"`
	PhysicalActionExecuted bool   `json:"physical_action_executed"`
}

type Boundary struct {
	DryRun       bool
	Now          func() time.Time
	Store        StoreReader
	Capabilities map[string]bool
}

func (b *Boundary) now() time.Time {
	if b != nil && b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// ValidatePayload applies the minimum ingress contract. Raw media, biometric
// material and transport-specific hardware identifiers never cross the
// Discovery/Core boundary.
func ValidatePayload(payload []byte) error {
	if len(payload) > MaxBoundaryPayload {
		return ErrBoundaryPayloadTooLarge
	}
	if len(payload) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return fmt.Errorf("invalid boundary JSON: %w", err)
	}
	if hasForbiddenValue(value) {
		return ErrForbiddenBoundaryData
	}
	return nil
}

func hasForbiddenValue(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			key = strings.ToLower(strings.TrimSpace(key))
			switch key {
			case "frame", "frames", "image", "images", "media", "raw_media", "bbox", "bboxes", "crop", "crops", "embedding", "embeddings", "biometric_embedding", "hardware_id", "mac", "serial_number", "device_serial":
				return true
			case "identity":
				return true
			}
			if hasForbiddenValue(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if hasForbiddenValue(child) {
				return true
			}
		}
	}
	return false
}

// NormalizeEvent creates the only event shape accepted by the V1 Core.
// Discovery performs no danger or action selection here.
func (b *Boundary) NormalizeEvent(event contract.Event) (contract.Event, error) {
	if strings.TrimSpace(event.Type) == "" || strings.TrimSpace(event.Source) == "" {
		return contract.Event{}, errors.New("event type and source are required")
	}
	body, err := json.Marshal(event.Payload)
	if err != nil {
		return contract.Event{}, err
	}
	if err := ValidatePayload(body); err != nil {
		return contract.Event{}, err
	}
	if event.ID == "" {
		event.ID = idgen.New("evt")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = b.now()
	}
	event.Timestamp = event.Timestamp.UTC()
	if event.Priority == 0 {
		event.Priority = contract.PriorityNormal
	}
	return event, nil
}

// WebCommand converts an external command into a normal Core event. It never
// calls a device and never writes the Store.
func (b *Boundary) WebCommand(command string, payload map[string]any) (contract.Event, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return contract.Event{}, err
	}
	if err := ValidatePayload(body); err != nil {
		return contract.Event{}, err
	}
	event := contract.Event{ID: idgen.New("web"), Type: WebCommandEvent, Source: "discovery", Timestamp: b.now(), Payload: map[string]any{"command": command, "arguments": payload}}
	return b.NormalizeEvent(event)
}

// ExecuteAction is the hardware boundary. V1 is always dry-run, so the
// returned result is a new event and physical_action_executed is false.
func (b *Boundary) ExecuteAction(request ActionRequest) (contract.Event, error) {
	if request.SchemaVersion == "" {
		request.SchemaVersion = BoundarySchemaVersion
	}
	if request.RequestID == "" || strings.TrimSpace(request.Action) == "" {
		return contract.Event{}, errors.New("action request_id and action are required")
	}
	result := ActionResult{SchemaVersion: BoundarySchemaVersion, RequestID: request.RequestID, Status: "dry_run", PhysicalActionExecuted: false}
	if request.Action == "no_action" {
		result.Status = "not_requested"
	}
	if b == nil || !b.DryRun {
		result.Status = "blocked"
		result.Reason = "physical_execution_disabled_in_v1"
	}
	if b != nil && b.DryRun && request.Action != "no_action" && !b.capabilityAvailable(request.Capability) {
		result.Status = "unavailable"
		result.Reason = "capability_unavailable"
	}
	body, err := json.Marshal(result)
	if err != nil {
		return contract.Event{}, err
	}
	return contract.Event{ID: idgen.New("act-result"), Type: ActionResultEvent, Source: "discovery", Timestamp: b.now(), Payload: map[string]any{"request": request, "result": json.RawMessage(body), "status": result.Status, "physical_action_executed": false}}, nil
}

func (b *Boundary) capabilityAvailable(capability string) bool {
	if strings.TrimSpace(capability) == "" || b == nil || b.Capabilities == nil {
		return false
	}
	return b.Capabilities[capability]
}

func (b *Boundary) Availability(component, status, reason string) (contract.Event, error) {
	payload := map[string]any{"component": component, "status": status, "reason": reason}
	return b.NormalizeEvent(contract.Event{ID: idgen.New("availability"), Type: AvailabilityEvent, Source: "discovery", Timestamp: b.now(), Payload: payload})
}

// ReadSnapshot is the only Store operation exposed to Discovery.
func (b *Boundary) ReadSnapshot() ([]byte, error) {
	if b == nil || b.Store == nil {
		return nil, errors.New("discovery store reader unavailable")
	}
	return b.Store.SnapshotJSON()
}

// IsForbiddenValue is exported for architecture tests and ingress adapters.
func IsForbiddenValue(value any) bool {
	return hasForbiddenValue(value)
}
