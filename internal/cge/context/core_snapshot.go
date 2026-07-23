package context

import (
	stdcontext "context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	CoreContextSchemaVersion = "core-context-v1"
	CoreContextPolicyPrefix  = "core-context-freshness-policy-v1:"
)

type FreshnessCode string

const (
	FreshnessFresh   FreshnessCode = "fresh"
	FreshnessAging   FreshnessCode = "aging"
	FreshnessStale   FreshnessCode = "stale"
	FreshnessUnknown FreshnessCode = "unknown"
)

func (c FreshnessCode) valid() bool {
	return c == FreshnessFresh || c == FreshnessAging || c == FreshnessStale || c == FreshnessUnknown
}

type ContextFreshnessPolicy struct {
	ResidentFreshFor   time.Duration
	ResidentStaleAfter time.Duration
	DeviceFreshFor     time.Duration
	DeviceStaleAfter   time.Duration
	CameraFreshFor     time.Duration
	CameraStaleAfter   time.Duration
}

func DefaultContextFreshnessPolicy() ContextFreshnessPolicy {
	return ContextFreshnessPolicy{
		ResidentFreshFor: 2 * time.Minute, ResidentStaleAfter: 15 * time.Minute,
		DeviceFreshFor: 2 * time.Minute, DeviceStaleAfter: 15 * time.Minute,
		CameraFreshFor: 2 * time.Minute, CameraStaleAfter: 15 * time.Minute,
	}
}

func (p ContextFreshnessPolicy) Validate() error {
	if p.ResidentFreshFor <= 0 || p.ResidentFreshFor > p.ResidentStaleAfter ||
		p.DeviceFreshFor <= 0 || p.DeviceFreshFor > p.DeviceStaleAfter ||
		p.CameraFreshFor <= 0 || p.CameraFreshFor > p.CameraStaleAfter {
		return fmt.Errorf("invalid context freshness policy")
	}
	return nil
}

func (p ContextFreshnessPolicy) Fingerprint() string {
	value, _ := json.Marshal(struct {
		ResidentFreshFor, ResidentStaleAfter time.Duration
		DeviceFreshFor, DeviceStaleAfter     time.Duration
		CameraFreshFor, CameraStaleAfter     time.Duration
	}{p.ResidentFreshFor, p.ResidentStaleAfter, p.DeviceFreshFor, p.DeviceStaleAfter, p.CameraFreshFor, p.CameraStaleAfter})
	digest := sha256.Sum256(value)
	return CoreContextPolicyPrefix + hex.EncodeToString(digest[:])
}

type ContextFreshness struct {
	Overall   FreshnessCode `json:"overall"`
	Residents FreshnessCode `json:"residents"`
	Devices   FreshnessCode `json:"devices"`
	Cameras   FreshnessCode `json:"cameras"`
	Topology  FreshnessCode `json:"topology"`
}

type CoreContextMarkers struct {
	ReadOnlySnapshot       bool `json:"read_only_snapshot"`
	Expurgated             bool `json:"expurgated"`
	NotAProductionDecision bool `json:"not_a_production_decision"`
	NotAuthorization       bool `json:"not_authorization"`
	NotACommand            bool `json:"not_a_command"`
	NotAnAction            bool `json:"not_an_action"`
	NotAnAlert             bool `json:"not_an_alert"`
	DoesNotOwnCoreState    bool `json:"does_not_own_core_state"`
	DoesNotModifyCoreState bool `json:"does_not_modify_core_state"`
	ContextOnly            bool `json:"context_only"`
	NoSecurityMeaning      bool `json:"no_security_meaning"`
}

func readOnlyContextMarkers() CoreContextMarkers {
	return CoreContextMarkers{
		ReadOnlySnapshot: true, Expurgated: true, NotAProductionDecision: true,
		NotAuthorization: true, NotACommand: true, NotAnAction: true, NotAnAlert: true,
		DoesNotOwnCoreState: true, DoesNotModifyCoreState: true, ContextOnly: true,
		NoSecurityMeaning: true,
	}
}

type ResidentContext struct {
	ResidentFingerprint string `json:"resident_fingerprint"`
	PresenceCode        string `json:"presence_code"`
	CurrentNodeCode     string `json:"current_node_code,omitempty"`
	ConfidencePermille  int    `json:"confidence_permille"`
	LastSeenUnixNano    int64  `json:"last_seen_unix_nano,omitempty"`
	FreshnessCode       string `json:"freshness_code"`
}

type DeviceContext struct {
	DeviceFingerprint string `json:"device_fingerprint"`
	NodeCode          string `json:"node_code,omitempty"`
	DeviceKind        string `json:"device_kind"`
	HealthCode        string `json:"health_code"`
	Online            bool   `json:"online"`
	LastSeenUnixNano  int64  `json:"last_seen_unix_nano,omitempty"`
	FreshnessCode     string `json:"freshness_code"`
}

type CameraContext struct {
	CameraFingerprint  string `json:"camera_fingerprint"`
	NodeCode           string `json:"node_code,omitempty"`
	Online             bool   `json:"online"`
	HealthCode         string `json:"health_code"`
	StreamAvailable    bool   `json:"stream_available"`
	DetectionAvailable bool   `json:"detection_available"`
	LastSeenUnixNano   int64  `json:"last_seen_unix_nano,omitempty"`
	FreshnessCode      string `json:"freshness_code"`
}

type CoreTopologyContext struct {
	Revision           string   `json:"revision,omitempty"`
	Nodes              []Node   `json:"nodes,omitempty"`
	Edges              []Edge   `json:"edges,omitempty"`
	ObservationNode    string   `json:"observation_node,omitempty"`
	ImmediateNeighbors []string `json:"immediate_neighbors,omitempty"`
	Fingerprint        string   `json:"fingerprint,omitempty"`
}

type CoreContextSnapshot struct {
	SchemaVersion      string              `json:"schema_version"`
	CapturedAtUnixNano int64               `json:"captured_at_unix_nano"`
	SourceRevision     uint64              `json:"source_revision,omitempty"`
	HomeMode           string              `json:"home_mode"`
	SystemState        string              `json:"system_state"`
	Residents          []ResidentContext   `json:"residents,omitempty"`
	Devices            []DeviceContext     `json:"devices,omitempty"`
	Cameras            []CameraContext     `json:"cameras,omitempty"`
	Topology           CoreTopologyContext `json:"topology"`
	Freshness          ContextFreshness    `json:"freshness"`
	Markers            CoreContextMarkers  `json:"markers"`
	Fingerprint        string              `json:"fingerprint"`
}

type SnapshotRequest struct {
	ObservationID string
	ObservedAt    time.Time
	NodeID        string
}

// CoreContextProvider extends the existing detached Provider boundary. Core
// integrations expose a rich snapshot for diagnostics while Resolve remains
// the compact frame consumed by the existing CGE pipeline.
type CoreContextProvider interface {
	Provider
	Snapshot(stdcontext.Context, SnapshotRequest) (CoreContextSnapshot, error)
}

func (s CoreContextSnapshot) Clone() CoreContextSnapshot {
	out := s
	out.Residents = append([]ResidentContext(nil), s.Residents...)
	out.Devices = append([]DeviceContext(nil), s.Devices...)
	out.Cameras = append([]CameraContext(nil), s.Cameras...)
	out.Topology.Nodes = append([]Node(nil), s.Topology.Nodes...)
	out.Topology.Edges = append([]Edge(nil), s.Topology.Edges...)
	out.Topology.ImmediateNeighbors = append([]string(nil), s.Topology.ImmediateNeighbors...)
	return out
}

func (s CoreContextSnapshot) Validate() error {
	if s.SchemaVersion != CoreContextSchemaVersion || s.CapturedAtUnixNano <= 0 || s.Fingerprint == "" {
		return fmt.Errorf("invalid core context snapshot envelope")
	}
	if !s.Freshness.Overall.valid() || !s.Freshness.Residents.valid() || !s.Freshness.Devices.valid() || !s.Freshness.Cameras.valid() || !s.Freshness.Topology.valid() {
		return fmt.Errorf("invalid core context freshness")
	}
	if s.Markers != readOnlyContextMarkers() {
		return fmt.Errorf("invalid core context markers")
	}
	if err := validateContextItems(s.Residents, func(value ResidentContext) string { return value.ResidentFingerprint }, valueFreshnessResident); err != nil {
		return err
	}
	for _, value := range s.Residents {
		if value.PresenceCode != "present" && value.PresenceCode != "absent" && value.PresenceCode != "unknown" || value.ConfidencePermille < 0 || value.ConfidencePermille > 1000 || !FreshnessCode(value.FreshnessCode).valid() {
			return fmt.Errorf("invalid resident context")
		}
	}
	if err := validateContextItems(s.Devices, func(value DeviceContext) string { return value.DeviceFingerprint }, valueFreshnessDevice); err != nil {
		return err
	}
	for _, value := range s.Devices {
		if value.HealthCode != "online" && value.HealthCode != "offline" && value.HealthCode != "unknown" || !FreshnessCode(value.FreshnessCode).valid() {
			return fmt.Errorf("invalid device context")
		}
	}
	if err := validateContextItems(s.Cameras, func(value CameraContext) string { return value.CameraFingerprint }, valueFreshnessCamera); err != nil {
		return err
	}
	for _, value := range s.Cameras {
		if value.HealthCode != "online" && value.HealthCode != "offline" && value.HealthCode != "unknown" || !FreshnessCode(value.FreshnessCode).valid() {
			return fmt.Errorf("invalid camera context")
		}
	}
	if s.Topology.Revision != "" {
		topology := TopologySnapshot{Revision: s.Topology.Revision, CapturedAt: time.Unix(0, s.CapturedAtUnixNano).UTC(), Nodes: s.Topology.Nodes, Edges: s.Topology.Edges}
		if canonical := CanonicalTopology(topology); !reflect.DeepEqual(canonical, topology) {
			return fmt.Errorf("invalid core topology order")
		}
		if err := topology.Validate(); err != nil {
			return err
		}
	}
	if snapshotFingerprint(s) != s.Fingerprint {
		return fmt.Errorf("invalid core context fingerprint")
	}
	return nil
}

type contextValueKind uint8

const (
	valueFreshnessResident contextValueKind = iota
	valueFreshnessDevice
	valueFreshnessCamera
)

func validateContextItems[T any](values []T, key func(T) string, _ contextValueKind) error {
	last := ""
	for _, value := range values {
		current := key(value)
		if current == "" || len(current) > 128 || strings.ContainsAny(current, "\r\n") || last != "" && current <= last {
			return fmt.Errorf("invalid core context item ordering")
		}
		last = current
	}
	return nil
}

func (s CoreContextSnapshot) WithFingerprint() CoreContextSnapshot {
	out := s.Clone()
	out.SchemaVersion = CoreContextSchemaVersion
	out.Markers = readOnlyContextMarkers()
	out.Fingerprint = snapshotFingerprint(out)
	return out
}

func snapshotFingerprint(s CoreContextSnapshot) string {
	value := s.Clone()
	value.Fingerprint = ""
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (s CoreContextSnapshot) Frame(request SnapshotRequest) (Frame, error) {
	if err := s.Validate(); err != nil {
		return Frame{}, err
	}
	occupancy := OccupancyUnknown
	hasAbsent := false
	hasUnknown := false
	for _, resident := range s.Residents {
		if resident.PresenceCode == "present" {
			occupancy = OccupancyOccupied
			break
		}
		if resident.PresenceCode == "absent" {
			hasAbsent = true
		} else {
			hasUnknown = true
		}
	}
	if occupancy == OccupancyUnknown && hasAbsent && !hasUnknown {
		occupancy = OccupancyUnoccupied
	}
	houseMode := HouseMode(s.HomeMode)
	if !validHouseMode(houseMode) {
		houseMode = HouseModeUnknown
	}
	topology := TopologySnapshot{Revision: s.Topology.Revision, CapturedAt: time.Unix(0, s.CapturedAtUnixNano).UTC(), Nodes: append([]Node(nil), s.Topology.Nodes...), Edges: append([]Edge(nil), s.Topology.Edges...)}
	frame, err := ResolveFrame(ResolveInput{ObservationID: request.ObservationID, ObservedAt: request.ObservedAt, NodeID: request.NodeID, Timezone: "UTC", Occupancy: occupancy, HouseMode: houseMode, Topology: topology, AllowPartial: true})
	if err != nil {
		return Frame{}, err
	}
	frame.SnapshotFingerprint = s.Fingerprint
	frame.FreshnessCode = string(s.Freshness.Overall)
	frame.Fingerprint = frameFingerprint(frame)
	return frame, nil
}

func freshnessAt(at, lastSeen time.Time, freshFor, staleAfter time.Duration) FreshnessCode {
	if lastSeen.IsZero() {
		return FreshnessUnknown
	}
	age := at.Sub(lastSeen)
	if age <= freshFor {
		return FreshnessFresh
	}
	if age <= staleAfter {
		return FreshnessAging
	}
	return FreshnessStale
}

// ClassifyFreshness applies the explicit freshness policy without retaining
// any source state.
func ClassifyFreshness(at, lastSeen time.Time, freshFor, staleAfter time.Duration) FreshnessCode {
	return freshnessAt(at, lastSeen, freshFor, staleAfter)
}

func AggregateFreshness(values ...FreshnessCode) FreshnessCode {
	if len(values) == 0 {
		return FreshnessUnknown
	}
	worst := FreshnessFresh
	known := false
	for _, value := range values {
		switch value {
		case FreshnessStale:
			return FreshnessStale
		case FreshnessAging:
			known = true
			if worst == FreshnessFresh {
				worst = FreshnessAging
			}
		case FreshnessFresh:
			known = true
		case FreshnessUnknown:
			if !known {
				worst = FreshnessUnknown
			}
		}
	}
	if !known {
		return FreshnessUnknown
	}
	return worst
}

func CanonicalCoreTopology(value CoreTopologyContext) CoreTopologyContext {
	out := value
	out.Nodes = append([]Node(nil), value.Nodes...)
	out.Edges = append([]Edge(nil), value.Edges...)
	out.ImmediateNeighbors = append([]string(nil), value.ImmediateNeighbors...)
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Edges, func(i, j int) bool { return coreEdgeKey(out.Edges[i]) < coreEdgeKey(out.Edges[j]) })
	sort.Strings(out.ImmediateNeighbors)
	return out
}

func coreEdgeKey(value Edge) string {
	return fmt.Sprintf("%s\x00%s\x00%t\x00%s", value.From, value.To, value.Directed, value.TraversalKind)
}
