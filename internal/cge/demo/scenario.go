package demo

import (
	"fmt"
	"math/rand"
	"time"

	cgecontext "synora/internal/cge/context"
)

type syntheticEvent struct {
	ID          string
	At          time.Time
	NodeID      string
	Identity    string
	DeviceID    string
	SequenceKey string
	TrackID     string
	Chapter     string
	Kind        string
	Label       string
	Mode        cgecontext.HouseMode
	Occupancy   cgecontext.OccupancyState
	Partial     bool
	Missing     bool
	Fixture     bool
	Restart     bool
}

func topology(start time.Time) cgecontext.TopologySnapshot {
	nodes := []cgecontext.Node{
		{ID: "bedroom", ZoneID: "upstairs", Kind: cgecontext.NodeRoom},
		{ID: "entrance", ZoneID: "ground", Kind: cgecontext.NodeEntrance, EntryPoint: true},
		{ID: "exterior", ZoneID: "outside", Kind: cgecontext.NodeExterior, Exterior: true},
		{ID: "hallway", ZoneID: "ground", Kind: cgecontext.NodeCorridor},
		{ID: "kitchen", ZoneID: "ground", Kind: cgecontext.NodeRoom},
		{ID: "living-room", ZoneID: "ground", Kind: cgecontext.NodeRoom},
	}
	edges := []cgecontext.Edge{
		{From: "bedroom", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "entrance", To: "exterior", TraversalKind: cgecontext.TraversalExterior},
		{From: "entrance", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "exterior", To: "entrance", TraversalKind: cgecontext.TraversalExterior},
		{From: "hallway", To: "bedroom", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "entrance", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "kitchen", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "living-room", TraversalKind: cgecontext.TraversalDoor},
		{From: "kitchen", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "living-room", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
	}
	return cgecontext.CanonicalTopology(cgecontext.TopologySnapshot{Revision: "demo-topology-v1", CapturedAt: start, Nodes: nodes, Edges: edges})
}

func buildEvents(seed uint64, scenario string, start time.Time) []syntheticEvent {
	rng := rand.New(rand.NewSource(int64(seed)))
	events := make([]syntheticEvent, 0, 64)
	var lastAt time.Time
	add := func(day int, minute int, node, chapter, kind, label string, mode cgecontext.HouseMode, partial bool) {
		jitter := 0
		if kind == "routine" {
			jitter = rng.Intn(5) - 2
		}
		at := start.AddDate(0, 0, day).Add(time.Duration(minute+jitter) * time.Minute)
		if !lastAt.IsZero() && !at.After(lastAt) {
			at = lastAt.Add(time.Minute)
		}
		lastAt = at
		events = append(events, syntheticEvent{ID: fmt.Sprintf("demo-%s-%03d", scenario, len(events)+1), At: at, NodeID: node, Identity: "subject-a", DeviceID: "synthetic-camera", SequenceKey: "home-routine", TrackID: "track-a", Chapter: chapter, Kind: kind, Label: label, Mode: mode, Occupancy: cgecontext.OccupancyOccupied, Partial: partial})
	}
	// Warm-up: the same path is observed over distinct days.
	for day := 0; day < 4; day++ {
		add(day, 18*60+30, "entrance", "memory", "routine", "ordinary arrival", cgecontext.HouseModeHome, false)
		add(day, 18*60+32, "hallway", "memory", "routine", "ordinary arrival", cgecontext.HouseModeHome, false)
		add(day, 18*60+35, "kitchen", "memory", "routine", "ordinary arrival", cgecontext.HouseModeHome, false)
	}
	// A lower-level duplicate branch is seeded immediately before this event;
	// the association planner then receives two genuinely equal candidates.
	add(3, 18*60+36, "entrance", "ambiguity", "ambiguity", "two plausible chains", cgecontext.HouseModeHome, false)
	events[len(events)-1].Fixture = true
	// A close occurrence is attached to the known path.
	add(4, 18*60+36, "kitchen", "deviation", "aligned", "aligned occurrence", cgecontext.HouseModeHome, false)
	// A new chain at night still compares its routine occurrence to the durable
	// subject baseline before learning it.
	add(5, 2*60+15, "entrance", "deviation", "deviation", "late-night occurrence", cgecontext.HouseModeNight, false)
	// Shifted schedule: enough independent days for a second temporal pattern.
	for day := 6; day < 11; day++ {
		add(day, 9*60+10, "entrance", "adaptation", "shift", "legitimate routine shift", cgecontext.HouseModeHome, false)
		add(day, 9*60+12, "hallway", "adaptation", "shift", "legitimate routine shift", cgecontext.HouseModeHome, false)
		add(day, 9*60+15, "living-room", "adaptation", "shift", "legitimate routine shift", cgecontext.HouseModeHome, false)
	}
	// One event with incomplete topology/context demonstrates degraded context.
	add(11, 20*60, "living-room", "transparency", "degraded-context", "partial context", cgecontext.HouseModeUnknown, true)
	for index := range events {
		if events[index].Kind == "shift" {
			events[index].Restart = true
			break
		}
	}
	return events
}
