package cognitive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

const (
	StateFrameSchemaVersion   = "state-frame/v1"
	StateEncoderSchemaVersion = "state-encoder/v4"
)

// StateFrame is the deterministic, model-independent projection of Core
// facts. It deliberately contains no pointers into Core-owned stores.
type StateFrame struct {
	SchemaVersion string          `json:"schema_version"`
	Revision      uint64          `json:"revision"`
	CapturedAt    time.Time       `json:"captured_at"`
	CurrentEvent  *StateEvent     `json:"current_event,omitempty"`
	System        StateSystem     `json:"system"`
	Devices       []StateDevice   `json:"devices"`
	Topology      []StateNode     `json:"topology"`
	Residents     []StateResident `json:"residents"`
	RecentEvents  []StateEvent    `json:"recent_events"`
	// These additive fields are observed inputs for the versioned v4 encoder.
	// They do not grant authority to the cognitive runtime.
	SecurityMode          string                   `json:"security_mode,omitempty"`
	KnownResidentsPresent bool                     `json:"known_residents_present,omitempty"`
	KnownResidentCount    int                      `json:"known_resident_count,omitempty"`
	ContextFacts          []string                 `json:"context_facts,omitempty"`
	AvailableCapabilities []string                 `json:"available_capabilities,omitempty"`
	AvailableActionIDs    []string                 `json:"available_action_ids,omitempty"`
	Temporal              EncoderV4TemporalContext `json:"temporal,omitempty"`
}

type StateEvent struct {
	ID         string    `json:"id,omitempty"`
	Type       string    `json:"type"`
	Source     string    `json:"source,omitempty"`
	DeviceID   string    `json:"device_id,omitempty"`
	NodeID     string    `json:"node_id,omitempty"`
	ResidentID string    `json:"resident_id,omitempty"`
	TrackID    string    `json:"track_id,omitempty"`
	Priority   int       `json:"priority,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
	Timestamp  time.Time `json:"timestamp,omitempty"`
}

type StateSystem struct {
	LastState   string  `json:"last_state,omitempty"`
	DangerLevel string  `json:"danger_level,omitempty"`
	DangerScore float64 `json:"danger_score,omitempty"`
	Armed       bool    `json:"armed"`
	Degraded    bool    `json:"degraded"`
}

type StateDevice struct {
	ID      string `json:"id"`
	Type    string `json:"type,omitempty"`
	Role    string `json:"role,omitempty"`
	NodeID  string `json:"node_id,omitempty"`
	Online  bool   `json:"online"`
	Enabled bool   `json:"enabled"`
}

type StateNode struct {
	ID           string   `json:"id"`
	Type         string   `json:"type,omitempty"`
	ParentID     string   `json:"parent_id,omitempty"`
	ConnectedIDs []string `json:"connected_ids,omitempty"`
	DynamicScore float64  `json:"dynamic_score,omitempty"`
}

type StateResident struct {
	ID         string  `json:"id"`
	Role       string  `json:"role,omitempty"`
	State      string  `json:"state,omitempty"`
	NodeID     string  `json:"node_id,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Enabled    bool    `json:"enabled"`
	Trusted    bool    `json:"trusted"`
}

func (f StateFrame) Validate() error {
	if f.SchemaVersion != StateFrameSchemaVersion {
		return fmt.Errorf("invalid state frame schema version %q", f.SchemaVersion)
	}
	if f.CapturedAt.IsZero() {
		return fmt.Errorf("state frame captured_at is required")
	}
	return nil
}

func (f StateFrame) Normalized() StateFrame {
	f.SchemaVersion = StateFrameSchemaVersion
	f.CapturedAt = f.CapturedAt.UTC()
	f.Devices = append([]StateDevice(nil), f.Devices...)
	f.Topology = append([]StateNode(nil), f.Topology...)
	f.Residents = append([]StateResident(nil), f.Residents...)
	f.RecentEvents = append([]StateEvent(nil), f.RecentEvents...)
	f.ContextFacts = append([]string(nil), f.ContextFacts...)
	f.AvailableCapabilities = append([]string(nil), f.AvailableCapabilities...)
	f.AvailableActionIDs = append([]string(nil), f.AvailableActionIDs...)
	sort.Slice(f.Devices, func(i, j int) bool { return f.Devices[i].ID < f.Devices[j].ID })
	sort.Slice(f.Topology, func(i, j int) bool { return f.Topology[i].ID < f.Topology[j].ID })
	sort.Slice(f.Residents, func(i, j int) bool { return f.Residents[i].ID < f.Residents[j].ID })
	sort.SliceStable(f.RecentEvents, func(i, j int) bool {
		if f.RecentEvents[i].Timestamp.Equal(f.RecentEvents[j].Timestamp) {
			return f.RecentEvents[i].ID < f.RecentEvents[j].ID
		}
		return f.RecentEvents[i].Timestamp.Before(f.RecentEvents[j].Timestamp)
	})
	for i := range f.Topology {
		f.Topology[i].ConnectedIDs = append([]string(nil), f.Topology[i].ConnectedIDs...)
		sort.Strings(f.Topology[i].ConnectedIDs)
	}
	sort.Strings(f.ContextFacts)
	sort.Strings(f.AvailableCapabilities)
	sort.Strings(f.AvailableActionIDs)
	return f
}

func (f StateFrame) CanonicalJSON() ([]byte, error) {
	return json.Marshal(f.Normalized())
}

func StateEventFromContract(event *contract.Event) *StateEvent {
	if event == nil {
		return nil
	}
	return &StateEvent{ID: event.ID, Type: event.Type, Source: event.Source, DeviceID: event.DeviceID, NodeID: event.NodeID, ResidentID: event.ResidentID, TrackID: event.TrackID, Priority: event.Priority, Confidence: event.Confidence, Timestamp: event.Timestamp.UTC()}
}

// EncodedState is the numeric representation consumed by a ModelBackend.
// Feature names are stable machine identifiers, never natural-language model
// output. The checksum binds the vector to one StateFrame.
type EncodedState struct {
	SchemaVersion  string    `json:"schema_version"`
	EncoderID      string    `json:"encoder_id"`
	EncoderVersion string    `json:"encoder_version"`
	DType          string    `json:"dtype"`
	Shape          []int     `json:"shape"`
	FeatureNames   []string  `json:"feature_names"`
	Values         []float32 `json:"values"`
	FrameChecksum  string    `json:"frame_checksum"`
}

func (e EncodedState) Validate() error {
	if (e.SchemaVersion != StateFrameSchemaVersion && e.SchemaVersion != StateEncoderSchemaVersion) || e.EncoderID == "" || e.EncoderVersion == "" || e.DType != "float32" {
		return fmt.Errorf("invalid encoded state contract")
	}
	if len(e.Values) == 0 || len(e.Values) != len(e.FeatureNames) || len(e.Shape) != 1 || e.Shape[0] != len(e.Values) || len(e.FrameChecksum) != sha256.Size*2 {
		return fmt.Errorf("invalid encoded state shape or checksum")
	}
	return nil
}

type StateEncoder interface {
	ID() string
	Version() string
	Encode(context.Context, StateFrame) (EncodedState, error)
}

// DeterministicStateEncoder is a small baseline encoder, not a learned model.
// It gives the future backend a reproducible vector while preserving the full
// StateFrame checksum for offline replay.
type DeterministicStateEncoder struct{}

func (DeterministicStateEncoder) ID() string      { return "deterministic" }
func (DeterministicStateEncoder) Version() string { return "0.1.0" }

func (e DeterministicStateEncoder) Encode(ctx context.Context, frame StateFrame) (EncodedState, error) {
	if err := ctx.Err(); err != nil {
		return EncodedState{}, err
	}
	frame = frame.Normalized()
	if frame.CapturedAt.IsZero() {
		frame.CapturedAt = time.Unix(0, 0).UTC()
	}
	canonical, err := frame.CanonicalJSON()
	if err != nil {
		return EncodedState{}, err
	}
	values := []float32{
		float32(frame.Revision),
		float32(len(frame.Devices)),
		float32(len(frame.Topology)),
		float32(len(frame.Residents)),
		float32(len(frame.RecentEvents)),
		boolValue(frame.System.Armed),
		boolValue(frame.System.Degraded),
		float32(frame.System.DangerScore),
	}
	names := []string{"revision", "device_count", "topology_count", "resident_count", "recent_event_count", "armed", "degraded", "danger_score"}
	if frame.CurrentEvent != nil {
		values = append(values, float32(frame.CurrentEvent.Priority), float32(frame.CurrentEvent.Confidence), categoryCode(frame.CurrentEvent.Type))
		names = append(names, "current_event_priority", "current_event_confidence", "current_event_type_code")
	}
	checksum := sha256.Sum256(canonical)
	return EncodedState{
		SchemaVersion: StateFrameSchemaVersion, EncoderID: e.ID(), EncoderVersion: e.Version(), DType: "float32",
		Shape: []int{len(values)}, FeatureNames: names, Values: values, FrameChecksum: hex.EncodeToString(checksum[:]),
	}, nil
}

func boolValue(value bool) float32 {
	if value {
		return 1
	}
	return 0
}

func categoryCode(value string) float32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(value))))
	return float32(hash.Sum32()) / float32(^uint32(0))
}
