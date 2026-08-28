package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// CameraObservation is the technical discovery record exchanged between
// Discovery and Core. It contains no camera credentials or media data.
// LastSeen keeps the RFC3339 V1 timestamp semantics; timestamp_ms is not a
// parallel representation of this contract.
type CameraObservation struct {
	SchemaVersion int       `json:"schema_version"`
	ObservationID string    `json:"observation_id"`
	CameraID      string    `json:"camera_id"`
	HardwareID    string    `json:"hardware_id,omitempty"`
	Endpoint      string    `json:"endpoint,omitempty"`
	Firmware      string    `json:"firmware,omitempty"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	Online        bool      `json:"online"`
	LastSeen      time.Time `json:"last_seen"`
}

// Canonical returns the wire-stable form used for IDs and comparisons.
func (o CameraObservation) Canonical() CameraObservation {
	o.SchemaVersion = V1SchemaVersion
	o.ObservationID = strings.TrimSpace(o.ObservationID)
	o.CameraID = strings.TrimSpace(o.CameraID)
	o.HardwareID = strings.TrimSpace(o.HardwareID)
	o.Endpoint = strings.TrimSpace(o.Endpoint)
	o.Firmware = strings.TrimSpace(o.Firmware)
	o.LastSeen = o.LastSeen.UTC()
	seen := make(map[string]struct{}, len(o.Capabilities))
	capabilities := make([]string, 0, len(o.Capabilities))
	for _, capability := range o.Capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if _, exists := seen[capability]; exists {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	o.Capabilities = capabilities
	return o
}

// EnsureID derives an idempotency key from the observation content when a
// producer did not provide one. The identifier is independent of map order
// and of local receive time.
func (o *CameraObservation) EnsureID() error {
	if o == nil {
		return fmt.Errorf("camera observation is required")
	}
	canonical := o.Canonical()
	if canonical.ObservationID == "" {
		withoutID := canonical
		withoutID.ObservationID = ""
		data, err := json.Marshal(withoutID)
		if err != nil {
			return fmt.Errorf("camera observation id: %w", err)
		}
		digest := sha256.Sum256(data)
		canonical.ObservationID = "camera-observation-" + hex.EncodeToString(digest[:])
	}
	*o = canonical
	return nil
}

func (o CameraObservation) Validate() error {
	o = o.Canonical()
	if err := ValidateSchemaVersion(o.SchemaVersion); err != nil {
		return err
	}
	if err := ValidateIdentifier("camera observation_id", o.ObservationID); err != nil {
		return err
	}
	if err := ValidateIdentifier("camera_id", o.CameraID); err != nil {
		return err
	}
	if o.LastSeen.IsZero() {
		return fmt.Errorf("camera observation last_seen is required")
	}
	return nil
}

// CameraObservationFromPayload decodes the canonical transport payload and
// applies the same normalization and validation at every service boundary.
func CameraObservationFromPayload(payload map[string]any) (CameraObservation, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return CameraObservation{}, err
	}
	var observation CameraObservation
	if err := json.Unmarshal(data, &observation); err != nil {
		return CameraObservation{}, err
	}
	if err := observation.EnsureID(); err != nil {
		return CameraObservation{}, err
	}
	if err := observation.Validate(); err != nil {
		return CameraObservation{}, err
	}
	return observation, nil
}
