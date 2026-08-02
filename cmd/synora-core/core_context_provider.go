package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	cgecontext "synora/internal/cge/context"
	"synora/internal/device"
	"synora/internal/state"
	"synora/internal/topology"
)

type coreReadOnlyContextProvider struct {
	app    *coreApp
	policy cgecontext.ContextFreshnessPolicy
	now    func() time.Time
}

func newCoreReadOnlyContextProvider(app *coreApp) *coreReadOnlyContextProvider {
	return &coreReadOnlyContextProvider{app: app, policy: cgecontext.DefaultContextFreshnessPolicy(), now: func() time.Time { return time.Now().UTC() }}
}

func (p *coreReadOnlyContextProvider) Resolve(ctx context.Context, id string, at time.Time, nodeID string) (cgecontext.Frame, error) {
	snapshot, err := p.Snapshot(ctx, cgecontext.SnapshotRequest{ObservationID: id, ObservedAt: at, NodeID: nodeID})
	if err != nil {
		return cgecontext.Frame{}, err
	}
	return snapshot.Frame(cgecontext.SnapshotRequest{ObservationID: id, ObservedAt: at, NodeID: nodeID})
}

func (p *coreReadOnlyContextProvider) Snapshot(ctx context.Context, request cgecontext.SnapshotRequest) (cgecontext.CoreContextSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return cgecontext.CoreContextSnapshot{}, err
	}
	if p == nil || p.app == nil || p.app.state == nil {
		return cgecontext.CoreContextSnapshot{}, fmt.Errorf("core_context_provider_unavailable")
	}
	if request.ObservedAt.IsZero() || strings.TrimSpace(request.ObservationID) == "" {
		return cgecontext.CoreContextSnapshot{}, fmt.Errorf("core_context_request_invalid")
	}
	if err := p.policy.Validate(); err != nil {
		return cgecontext.CoreContextSnapshot{}, fmt.Errorf("core_context_policy_invalid")
	}
	capturedAt := time.Now().UTC()
	if p.now != nil {
		capturedAt = p.now().UTC()
	}
	if capturedAt.IsZero() {
		return cgecontext.CoreContextSnapshot{}, fmt.Errorf("core_context_clock_invalid")
	}

	p.app.mu.RLock()
	stateSnapshot := p.app.state.ContextSnapshot()
	residents := residentIDs(p.app.residents)
	topologySnapshot := coreTopologySnapshot(p.app.topology)
	topologySnapshot.ObservationNode = request.NodeID
	topologySnapshot.ImmediateNeighbors = topologyNeighbors(topologySnapshot, request.NodeID)
	devices := []device.DeviceConfig(nil)
	if p.app.device != nil {
		devices = p.app.device.Ordered()
	}
	p.app.mu.RUnlock()

	snapshot := buildCoreContextSnapshot(capturedAt, stateSnapshot, residents, devices, topologySnapshot, p.policy)
	if err := snapshot.Validate(); err != nil {
		return cgecontext.CoreContextSnapshot{}, fmt.Errorf("core_context_snapshot_invalid")
	}
	return snapshot, nil
}

func residentIDs(values map[string]*topology.Resident) []string {
	ids := make([]string, 0, len(values))
	for id, value := range values {
		if value != nil && value.ID != "" {
			ids = append(ids, value.ID)
		} else if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return uniqueStrings(ids)
}

func buildCoreContextSnapshot(capturedAt time.Time, source state.ContextSourceSnapshot, residentIDs []string, devices []device.DeviceConfig, topologyValue cgecontext.CoreTopologyContext, policy cgecontext.ContextFreshnessPolicy) cgecontext.CoreContextSnapshot {
	topologyValue = cgecontext.CanonicalCoreTopology(topologyValue)
	if topologyValue.Fingerprint == "" && len(topologyValue.Nodes) > 0 {
		topologyValue.Fingerprint = stableJSONFingerprint("topology", topologyValue)
	}
	presence := make(map[string]state.PresenceState, len(source.Presence))
	for _, value := range source.Presence {
		presence[value.ResidentID] = value
	}
	for _, value := range source.Presence {
		residentIDs = append(residentIDs, value.ResidentID)
	}
	residentIDs = uniqueStrings(residentIDs)

	residents := make([]cgecontext.ResidentContext, 0, len(residentIDs))
	for _, id := range residentIDs {
		value := cgecontext.ResidentContext{ResidentFingerprint: stableContextFingerprint("resident", id), PresenceCode: "unknown", FreshnessCode: string(cgecontext.FreshnessUnknown)}
		if current, ok := presence[id]; ok {
			value.PresenceCode = normalizePresenceCode(current.State)
			value.CurrentNodeCode = strings.TrimSpace(current.Location)
			value.ConfidencePermille = confidencePermille(current.Confidence)
			value.LastSeenUnixNano = current.LastSeen.UnixNano()
			value.FreshnessCode = string(cgecontextFreshness(capturedAt, current.LastSeen, policy.ResidentFreshFor, policy.ResidentStaleAfter))
		}
		residents = append(residents, value)
	}
	sort.Slice(residents, func(i, j int) bool { return residents[i].ResidentFingerprint < residents[j].ResidentFingerprint })

	deviceStates := make(map[string]state.DeviceState, len(source.Devices))
	for _, value := range source.Devices {
		deviceStates[value.ID] = value
	}
	deviceConfigs := make(map[string]device.DeviceConfig, len(devices))
	for _, value := range devices {
		deviceConfigs[value.ID] = value
	}
	deviceIDs := make([]string, 0, len(deviceStates)+len(deviceConfigs))
	for id := range deviceStates {
		deviceIDs = append(deviceIDs, id)
	}
	for id := range deviceConfigs {
		deviceIDs = append(deviceIDs, id)
	}
	deviceIDs = uniqueStrings(deviceIDs)

	cameras := make(map[string]state.CameraState, len(source.Cameras))
	for _, value := range source.Cameras {
		cameras[value.ID] = value
	}
	deviceValues := make([]cgecontext.DeviceContext, 0, len(deviceIDs))
	cameraValues := make([]cgecontext.CameraContext, 0)
	for _, id := range deviceIDs {
		config := deviceConfigs[id]
		current := deviceStates[id]
		kind := strings.TrimSpace(current.Type)
		if kind == "" {
			kind = strings.TrimSpace(config.Type)
		}
		node := strings.TrimSpace(current.NodeID)
		if node == "" {
			node = strings.TrimSpace(config.NodeID)
			if node == "" {
				node = strings.TrimSpace(config.Room)
			}
		}
		health := healthCode(current.LastSeen, current.Online)
		deviceValues = append(deviceValues, cgecontext.DeviceContext{DeviceFingerprint: stableContextFingerprint("device", id), NodeCode: node, DeviceKind: kind, HealthCode: health, Online: health == "online", LastSeenUnixNano: current.LastSeen.UnixNano(), FreshnessCode: string(cgecontextFreshness(capturedAt, current.LastSeen, policy.DeviceFreshFor, policy.DeviceStaleAfter))})
		if kind == "camera" {
			camera := cameras[id]
			cameraNode := strings.TrimSpace(camera.NodeID)
			if cameraNode == "" {
				cameraNode = node
			}
			cameraHealth := healthCode(camera.LastSeen, camera.Online)
			if camera.LastSeen.IsZero() && !current.LastSeen.IsZero() {
				cameraHealth = health
				camera.LastSeen = current.LastSeen
			}
			cameraValues = append(cameraValues, cgecontext.CameraContext{CameraFingerprint: stableContextFingerprint("camera", id), NodeCode: cameraNode, Online: cameraHealth == "online", HealthCode: cameraHealth, StreamAvailable: cameraHealth == "online", DetectionAvailable: cameraHealth == "online", LastSeenUnixNano: camera.LastSeen.UnixNano(), FreshnessCode: string(cgecontextFreshness(capturedAt, camera.LastSeen, policy.CameraFreshFor, policy.CameraStaleAfter))})
		}
	}
	sort.Slice(deviceValues, func(i, j int) bool { return deviceValues[i].DeviceFingerprint < deviceValues[j].DeviceFingerprint })
	sort.Slice(cameraValues, func(i, j int) bool { return cameraValues[i].CameraFingerprint < cameraValues[j].CameraFingerprint })

	residentsFreshness := itemFreshness(residents, func(value cgecontext.ResidentContext) string { return value.FreshnessCode })
	devicesFreshness := itemFreshness(deviceValues, func(value cgecontext.DeviceContext) string { return value.FreshnessCode })
	camerasFreshness := itemFreshness(cameraValues, func(value cgecontext.CameraContext) string { return value.FreshnessCode })
	topologyFreshness := cgecontext.FreshnessUnknown
	if topologyValue.Revision != "" {
		topologyFreshness = cgecontext.FreshnessFresh
	}

	homeMode := source.System.SecurityMode
	if source.System.Armed {
		homeMode = "armed"
	}
	if homeMode == "" {
		homeMode = "unknown"
	}
	snapshot := cgecontext.CoreContextSnapshot{
		SchemaVersion: CoreContextSchemaVersion(), CapturedAtUnixNano: capturedAt.UnixNano(),
		HomeMode: homeMode, SystemState: source.System.LastState,
		Residents: residents, Devices: deviceValues, Cameras: cameraValues, Topology: topologyValue,
		Freshness: cgecontext.ContextFreshness{Overall: cgecontext.AggregateFreshness(residentsFreshness, devicesFreshness, camerasFreshness, topologyFreshness), Residents: residentsFreshness, Devices: devicesFreshness, Cameras: camerasFreshness, Topology: topologyFreshness},
	}
	return snapshot.WithFingerprint()
}

func CoreContextSchemaVersion() string { return cgecontext.CoreContextSchemaVersion }

func coreTopologySnapshot(value *topology.Topology) cgecontext.CoreTopologyContext {
	if value == nil || len(value.Nodes) == 0 {
		return cgecontext.CoreTopologyContext{}
	}
	nodes := make([]cgecontext.Node, 0, len(value.Nodes))
	for _, item := range value.Nodes {
		if item == nil {
			continue
		}
		parentID := ""
		if item.Parent != nil {
			parentID = item.Parent.ID
		}
		zoneID := ""
		for parent := item; parent != nil; parent = parent.Parent {
			if parent.Type == topology.NodeZone {
				zoneID = parent.ID
				break
			}
		}
		nodes = append(nodes, cgecontext.Node{ID: item.ID, ParentID: parentID, ZoneID: zoneID, Kind: contextNodeKind(item.Type)})
	}
	edges := make([]cgecontext.Edge, 0)
	seenEdges := make(map[string]struct{})
	for _, item := range value.Nodes {
		if item == nil {
			continue
		}
		for _, target := range item.Connect {
			from, to := item.ID, target
			if from > to {
				from, to = to, from
			}
			key := from + "\x00" + to
			if from == "" || to == "" || from == to {
				continue
			}
			if _, ok := seenEdges[key]; ok {
				continue
			}
			seenEdges[key] = struct{}{}
			edges = append(edges, cgecontext.Edge{From: from, To: to, Directed: false, TraversalKind: cgecontext.TraversalUnknown})
		}
	}
	result := cgecontext.CanonicalCoreTopology(cgecontext.CoreTopologyContext{Nodes: nodes, Edges: edges})
	result.Fingerprint = stableJSONFingerprint("topology", result)
	result.Revision = result.Fingerprint
	return result
}

func topologyNeighbors(value cgecontext.CoreTopologyContext, nodeID string) []string {
	neighbors := make([]string, 0)
	for _, edge := range value.Edges {
		if edge.From == nodeID {
			neighbors = append(neighbors, edge.To)
		} else if edge.To == nodeID {
			neighbors = append(neighbors, edge.From)
		}
	}
	return uniqueStrings(neighbors)
}

func contextNodeKind(value topology.NodeType) cgecontext.NodeKind {
	switch value {
	case topology.NodeRoom:
		return cgecontext.NodeRoom
	case topology.NodeZone, topology.NodeFloor, topology.NodeHouse, topology.NodeRoot:
		return cgecontext.NodeRoom
	default:
		return cgecontext.NodeUnknown
	}
}

func stableContextFingerprint(kind, value string) string {
	return stableJSONFingerprint(kind, value)
}

func stableJSONFingerprint(kind string, value any) string {
	payload, _ := json.Marshal(struct {
		Kind  string `json:"kind"`
		Value any    `json:"value"`
	}{kind, value})
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func normalizePresenceCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "present":
		return "present"
	case "absent":
		return "absent"
	default:
		return "unknown"
	}
}

func healthCode(lastSeen time.Time, online bool) string {
	if lastSeen.IsZero() {
		return "unknown"
	}
	if online {
		return "online"
	}
	return "offline"
}

func confidencePermille(value float64) int {
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1000
	}
	return int(value * 1000)
}

func cgecontextFreshness(at, lastSeen time.Time, freshFor, staleAfter time.Duration) cgecontext.FreshnessCode {
	return cgecontext.ClassifyFreshness(at, lastSeen, freshFor, staleAfter)
}

func itemFreshness[T any](values []T, code func(T) string) cgecontext.FreshnessCode {
	if len(values) == 0 {
		return cgecontext.FreshnessUnknown
	}
	items := make([]cgecontext.FreshnessCode, 0, len(values))
	for _, value := range values {
		items = append(items, cgecontext.FreshnessCode(code(value)))
	}
	return cgecontext.AggregateFreshness(items...)
}

func uniqueStrings(values []string) []string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	out := copyValues[:0]
	for _, value := range copyValues {
		if value != "" && (len(out) == 0 || out[len(out)-1] != value) {
			out = append(out, value)
		}
	}
	return out
}
