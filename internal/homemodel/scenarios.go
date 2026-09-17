package homemodel

import (
	"fmt"
	"sort"
	"time"

	"synora/internal/topology"
	"synora/pkg/contract"
)

// Scenario is a material-free deterministic acceptance input. ExpectedTypes
// are advisory assertions used by the scenario runner and can be extended
// without changing the Home Model contract.
type Scenario struct {
	ID            string
	Topology      *topology.Topology
	Events        []*contract.Event
	ExpectedTypes []string
}

func RunScenario(s Scenario) *contract.HomeModelSnapshot {
	model := New(s.Topology, DefaultConfig())
	for _, event := range s.Events {
		model.Observe(event)
	}
	return model.Snapshot()
}

func ValidateScenario(s Scenario) error {
	snapshot := RunScenario(s)
	seen := map[string]bool{}
	for _, situation := range snapshot.Situations {
		seen[situation.Type] = true
	}
	for _, expected := range s.ExpectedTypes {
		if !seen[expected] {
			return fmt.Errorf("scenario %s missing situation %q", s.ID, expected)
		}
	}
	return nil
}

func ReferenceScenarios() []Scenario {
	base := time.Date(2026, 9, 1, 18, 0, 0, 0, time.UTC)
	makeTopology := func() *topology.Topology {
		return &topology.Topology{Nodes: map[string]*topology.Node{
			"driveway":    {ID: "driveway", Name: "Driveway", Type: topology.NodeZone, Metadata: map[string]any{"exterior": true}, Connect: []string{"front_door", "garden"}},
			"front_door":  {ID: "front_door", Name: "Front door", Type: topology.NodeRoom, Connect: []string{"driveway", "hallway"}},
			"hallway":     {ID: "hallway", Name: "Hallway", Type: topology.NodeRoom, Connect: []string{"front_door", "living_room"}},
			"living_room": {ID: "living_room", Name: "Living room", Type: topology.NodeRoom, Connect: []string{"hallway"}},
			"garden":      {ID: "garden", Name: "Garden", Type: topology.NodeZone, Metadata: map[string]any{"exterior": true}, Connect: []string{"driveway"}},
		}}
	}
	event := func(id, source, track, class, identity, zone string, at time.Time, confidence float64) *contract.Event {
		return &contract.Event{ID: id, Type: contract.EventVisionUnknown, Source: source, DeviceID: source, TrackID: track, Identity: identity, NodeID: zone, Timestamp: at, Confidence: confidence, Payload: map[string]any{"entity_class": class}}
	}
	return []Scenario{
		{ID: "S01", Topology: makeTopology(), Events: []*contract.Event{event("s01-a", "cam-driveway", "1", "person", "resident-1", "driveway", base, .96), event("s01-b", "cam-door", "2", "person", "resident-1", "front_door", base.Add(20*time.Second), .94)}, ExpectedTypes: []string{"resident_returned"}},
		{ID: "S02", Topology: makeTopology(), Events: []*contract.Event{event("s02-a", "cam-driveway", "1", "person", "", "driveway", base, .88), event("s02-b", "cam-door", "2", "person", "", "front_door", base.Add(10*time.Second), .9)}, ExpectedTypes: []string{"unknown_approaching"}},
		{ID: "S03", Topology: makeTopology(), Events: []*contract.Event{event("s03-a", "cam-door", "1", "person", "", "front_door", base, .9), event("s03-b", "cam-driveway", "2", "person", "", "driveway", base.Add(20*time.Second), .88)}, ExpectedTypes: []string{"entity_departed"}},
		{ID: "S04", Topology: makeTopology(), Events: []*contract.Event{event("s04-a", "cam-driveway", "1", "person", "", "driveway", base, .9), event("s04-b", "cam-garden", "2", "person", "", "garden", base.Add(10*time.Second), .86), event("s04-c", "cam-driveway", "3", "person", "", "driveway", base.Add(20*time.Second), .86)}, ExpectedTypes: []string{"property_transition"}},
		{ID: "S05", Topology: makeTopology(), Events: []*contract.Event{event("s05-a", "cam-door", "resident", "person", "resident-1", "front_door", base, .95), event("s05-b", "cam-door", "unknown", "person", "", "front_door", base, .9)}, ExpectedTypes: []string{"unknown_remaining"}},
		{ID: "S06", Topology: makeTopology(), Events: []*contract.Event{event("s06-a", "cam-driveway", "vehicle-1", "vehicle", "", "driveway", base, .93)}},
		{ID: "S07", Topology: makeTopology(), Events: []*contract.Event{event("s07-a", "cam-driveway", "1", "person", "", "driveway", base, .9), event("s07-b", "cam-door", "2", "person", "", "", base.Add(8*time.Second), .8), event("s07-c", "cam-door", "2", "person", "", "front_door", base.Add(16*time.Second), .9)}},
		{ID: "S08", Topology: makeTopology(), Events: []*contract.Event{{ID: "s08-a", Type: contract.EventVisionUnknown, Source: "cam-garden", DeviceID: "cam-garden", TrackID: "pet-a", NodeID: "garden", Timestamp: base, Confidence: .8, Payload: map[string]any{"entity_class": "pet", "features": map[string]any{"species": "dog"}}}, {ID: "s08-b", Type: contract.EventDeviceTrigger, Source: "radar-garden", DeviceID: "radar-garden", TrackID: "pet-a", NodeID: "garden", Timestamp: base, Confidence: .9, Payload: map[string]any{"entity_class": "pet", "features": map[string]any{"species": "dog"}}}}},
		{ID: "S09", Topology: makeTopology(), Events: []*contract.Event{event("s09-a", "cam-door", "resident", "person", "resident-1", "front_door", base, .95), event("s09-b", "cam-door", "unknown", "person", "", "hallway", base.Add(5*time.Second), .9), event("s09-c", "cam-driveway", "resident", "person", "resident-1", "driveway", base.Add(20*time.Second), .92)}, ExpectedTypes: []string{"entity_departed", "unknown_remaining"}},
		{ID: "S10", Topology: makeTopology(), Events: []*contract.Event{event("s10-a", "cam-door", "1", "person", "resident-1", "front_door", base, .72), event("s10-b", "radar-door", "1", "person", "", "front_door", base, .84)}, ExpectedTypes: []string{"unknown_remaining"}},
	}
}

func ScenarioIDs() []string {
	items := ReferenceScenarios()
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}
