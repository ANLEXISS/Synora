package contract

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// V1SchemaVersion is the schema version used by newly introduced V1 records.
// Existing contracts keep their established Version fields and JSON shape.
const V1SchemaVersion = 1

// TimestampFormatV1 documents the wire representation used by the V1
// contracts. time.Time's standard JSON encoding is RFC3339Nano, and remains
// the canonical representation for compatibility with existing consumers.
const TimestampFormatV1 = "RFC3339"

// ValidateSchemaVersion accepts an omitted version for legacy records and the
// current V1 version for new records. Future versions fail closed at the
// boundary instead of being silently interpreted as V1.
func ValidateSchemaVersion(version int) error {
	if version == 0 {
		return nil
	}
	if version != V1SchemaVersion {
		return fmt.Errorf("unsupported V1 schema version %d", version)
	}
	return nil
}

// ValidateIdentifier checks the common, transport-safe subset of Synora
// identifiers without imposing a format on existing IDs (some contain ':' or
// other separators). Empty optional identifiers are accepted by callers.
func ValidateIdentifier(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if len(value) > 256 {
		return fmt.Errorf("%s exceeds 256 bytes", name)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%s contains whitespace or control characters", name)
		}
	}
	return nil
}

func validateOptionalIdentifier(name, value string) error {
	if value == "" {
		return nil
	}
	return ValidateIdentifier(name, value)
}

func (m Message) Validate() error {
	if err := ValidateIdentifier("message type", m.Type); err != nil {
		return err
	}
	if err := ValidateIdentifier("message source", m.Source); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"message id": m.ID, "message target": m.Target,
		"message correlation_id": m.CorrelationID, "message request_id": m.RequestID,
	} {
		if err := validateOptionalIdentifier(name, value); err != nil {
			return err
		}
	}
	if m.Timestamp.IsZero() {
		return fmt.Errorf("message timestamp is required")
	}
	return nil
}

func (e Event) Validate() error {
	if err := ValidateIdentifier("event type", e.Type); err != nil {
		return err
	}
	if err := ValidateIdentifier("event source", e.Source); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"event id": e.ID, "event device_id": e.DeviceID,
		"event node_id": e.NodeID, "event track_id": e.TrackID,
		"event clip_id": e.ClipID, "event activation_id": e.ActivationID,
		"event sequence_key": e.SequenceKey,
	} {
		if err := validateOptionalIdentifier(name, value); err != nil {
			return err
		}
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("event timestamp is required")
	}
	return nil
}

func (d Decision) Validate() error {
	if err := ValidateIdentifier("decision type", d.Type); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"decision id": d.ID, "decision event_id": d.EventID,
		"decision node_id": d.NodeID, "decision clip_id": d.ClipID,
		"decision track_id": d.TrackID, "decision sequence_key": d.SequenceKey,
	} {
		if err := validateOptionalIdentifier(name, value); err != nil {
			return err
		}
	}
	return nil
}

func (a Action) Validate() error {
	if a.Type == "" && a.Command == "" {
		return fmt.Errorf("action type or command is required")
	}
	if err := validateOptionalIdentifier("action device", a.Device); err != nil {
		return err
	}
	if a.TimeoutMs < 0 || a.Retry < 0 {
		return fmt.Errorf("action timeout and retry cannot be negative")
	}
	return nil
}

func (r ActionRequest) Validate() error {
	if err := r.Action.Validate(); err != nil && r.Type == "" {
		return err
	}
	for name, value := range map[string]string{
		"action request id": r.ID, "action request_id": r.RequestID,
		"action correlation_id": r.CorrelationID, "action target": r.Target,
		"action source": r.Source, "action idempotency_key": r.IdempotencyKey,
	} {
		if err := validateOptionalIdentifier(name, value); err != nil {
			return err
		}
	}
	if r.TimeoutMs < 0 || r.RetryCount < 0 || r.Retry < 0 {
		return fmt.Errorf("action request timeout and retry cannot be negative")
	}
	return nil
}

func (r ActionResult) Validate() error {
	if err := ValidateIdentifier("action result status", r.Status); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"action result id": r.ID, "action result request_id": r.RequestID,
		"action result action_id": r.ActionID, "action result target": r.Target,
	} {
		if err := validateOptionalIdentifier(name, value); err != nil {
			return err
		}
	}
	return nil
}

func (r Resident) Validate() error {
	if err := ValidateIdentifier("resident id", r.ID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("resident name is required")
	}
	return nil
}

// FaceDatasetVersion is the immutable descriptor exchanged while Discovery
// builds and Vision loads a facial dataset. It deliberately contains metadata
// only; embeddings and source paths remain outside the service contract.
type FaceDatasetVersion struct {
	SchemaVersion      int       `json:"schema_version"`
	Version            string    `json:"version"`
	DesiredRevision    uint64    `json:"desired_revision"`
	BuiltAt            time.Time `json:"built_at"`
	ManifestChecksum   string    `json:"manifest_checksum"`
	ModelFingerprint   string    `json:"model_fingerprint"`
	EmbeddingDimension int       `json:"embedding_dimension"`
}

func (v FaceDatasetVersion) Validate() error {
	if err := ValidateSchemaVersion(v.SchemaVersion); err != nil {
		return err
	}
	if err := ValidateIdentifier("face dataset version", v.Version); err != nil {
		return err
	}
	if v.BuiltAt.IsZero() {
		return fmt.Errorf("face dataset built_at is required")
	}
	if v.EmbeddingDimension <= 0 {
		return fmt.Errorf("face dataset embedding dimension must be positive")
	}
	if err := ValidateIdentifier("face dataset manifest checksum", v.ManifestChecksum); err != nil {
		return err
	}
	if err := ValidateIdentifier("face dataset model fingerprint", v.ModelFingerprint); err != nil {
		return err
	}
	return nil
}
