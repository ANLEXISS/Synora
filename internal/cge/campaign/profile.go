package campaign

import (
	"fmt"
	"sort"
	"strings"
	"time"

	cgecontext "synora/internal/cge/context"
)

var defaultStart = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

func referenceNodes() []NodeProfile {
	return []NodeProfile{
		{ID: "bathroom", ZoneID: "ground", Kind: cgecontext.NodeRoom},
		{ID: "bedroom-a", ZoneID: "upstairs", Kind: cgecontext.NodeRoom},
		{ID: "bedroom-b", ZoneID: "upstairs", Kind: cgecontext.NodeRoom},
		{ID: "entrance", ZoneID: "ground", Kind: cgecontext.NodeEntrance, EntryPoint: true},
		{ID: "exterior", ZoneID: "outside", Kind: cgecontext.NodeExterior, Exterior: true},
		{ID: "garage", ZoneID: "outside", Kind: cgecontext.NodeGarage, Exterior: true},
		{ID: "garden", ZoneID: "outside", Kind: cgecontext.NodeGarden, Exterior: true},
		{ID: "hallway", ZoneID: "ground", Kind: cgecontext.NodeCorridor},
		{ID: "kitchen", ZoneID: "ground", Kind: cgecontext.NodeRoom},
		{ID: "living-room", ZoneID: "ground", Kind: cgecontext.NodeRoom},
	}
}

func referenceTopology() cgecontext.TopologySnapshot {
	profiles := referenceNodes()
	nodes := make([]cgecontext.Node, 0, len(profiles))
	for _, node := range profiles {
		nodes = append(nodes, cgecontext.Node{ID: node.ID, ZoneID: node.ZoneID, Kind: node.Kind, EntryPoint: node.EntryPoint, Exterior: node.Exterior})
	}
	edges := []cgecontext.Edge{
		{From: "bathroom", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "bedroom-a", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "bedroom-b", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "entrance", To: "exterior", TraversalKind: cgecontext.TraversalExterior},
		{From: "exterior", To: "entrance", TraversalKind: cgecontext.TraversalExterior},
		{From: "entrance", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "exterior", To: "garden", TraversalKind: cgecontext.TraversalWalk},
		{From: "exterior", To: "garage", TraversalKind: cgecontext.TraversalWalk},
		{From: "garage", To: "exterior", TraversalKind: cgecontext.TraversalWalk},
		{From: "garden", To: "exterior", TraversalKind: cgecontext.TraversalWalk},
		{From: "hallway", To: "bathroom", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "bedroom-a", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "bedroom-b", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "entrance", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "kitchen", TraversalKind: cgecontext.TraversalDoor},
		{From: "hallway", To: "living-room", TraversalKind: cgecontext.TraversalDoor},
		{From: "kitchen", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
		{From: "living-room", To: "hallway", TraversalKind: cgecontext.TraversalDoor},
	}
	return cgecontext.CanonicalTopology(cgecontext.TopologySnapshot{Revision: "campaign-topology-v1", CapturedAt: defaultStart, Nodes: nodes, Edges: edges})
}

func resident(id string, weekday, weekend int) ResidentProfile {
	return ResidentProfile{ID: id, WeekdaySchedule: ScheduleTemplate{StartMinuteOfDay: weekday, EndMinuteOfDay: weekday + 30}, WeekendSchedule: ScheduleTemplate{StartMinuteOfDay: weekend, EndMinuteOfDay: weekend + 30}, Variation: VariationPolicy{ScheduleJitterMinutes: 5}}
}

func routine(id, residentID string, days []time.Weekday, minute int, path ...string) RoutineTemplate {
	return RoutineTemplate{ID: id, ResidentID: residentID, DaysOfWeek: append([]time.Weekday(nil), days...), StartMinuteOfDay: minute, VariationMinutes: 5, Path: path, HouseMode: cgecontext.HouseModeHome, Occupancy: cgecontext.OccupancyOccupied, ProbabilityPermille: 1000}
}

func baseProfile(id, description string, days int, seed uint64) Profile {
	return Profile{ID: id, Description: description, StartAt: defaultStart, Timezone: "Europe/Paris", DurationDays: days, Seed: seed, Residents: []ResidentProfile{resident("resident-a", 7*60+30, 9*60)}, Nodes: referenceNodes(), RestartPolicy: RestartPolicy{EveryDays: 0}, CheckpointPolicy: CheckpointPolicy{EveryDays: 7, BeforeRestart: true}}
}

func DefaultProfiles() []Profile {
	weekday := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	weekend := []time.Weekday{time.Saturday, time.Sunday}
	profiles := make([]Profile, 0, 8)
	a := baseProfile("stable_single_resident_30d", "stable single resident", 30, 3501)
	a.RoutineTemplates = []RoutineTemplate{routine("a-morning-kitchen", "resident-a", weekday, 7*60+30, "bedroom-a", "hallway", "kitchen"), routine("a-weekend-kitchen", "resident-a", weekend, 9*60, "bedroom-a", "hallway", "kitchen")}
	profiles = append(profiles, a)
	b := baseProfile("stable_two_residents_30d", "stable two residents", 30, 3502)
	b.Residents = []ResidentProfile{resident("resident-a", 7*60+30, 9*60), resident("resident-b", 8*60+15, 10*60)}
	b.RoutineTemplates = []RoutineTemplate{routine("a-kitchen", "resident-a", weekday, 7*60+30, "bedroom-a", "hallway", "kitchen"), routine("b-living", "resident-b", weekday, 8*60+15, "bedroom-b", "hallway", "living-room")}
	profiles = append(profiles, b)
	c := baseProfile("routine_shift_45d", "progressive routine shift", 45, 3503)
	c.RoutineTemplates = []RoutineTemplate{routine("shift-baseline", "resident-a", weekday, 7*60+30, "bedroom-a", "hallway", "kitchen")}
	c.Episodes = []EpisodeTemplate{{ID: "shift", Label: LabelRoutineChange, StartDay: 15, StartMinuteOfDay: 9 * 60, Duration: 10 * time.Minute, ResidentID: "resident-a", Path: []string{"bedroom-a", "hallway", "living-room"}, RepeatEveryDays: 1, RepeatCount: 15}}
	profiles = append(profiles, c)
	d := baseProfile("benign_irregularity_30d", "benign irregularity", 30, 3504)
	d.RoutineTemplates = a.RoutineTemplates
	d.Episodes = []EpisodeTemplate{{ID: "late-return", Label: LabelRareLegitimate, StartDay: 5, StartMinuteOfDay: 23*60 + 20, Duration: 20 * time.Minute, ResidentID: "resident-a", Path: []string{"exterior", "entrance", "hallway", "entrance", "exterior", "garage"}, RepeatEveryDays: 7, RepeatCount: 3}, {ID: "weekend-garden", Label: LabelBenignVariation, StartDay: 7, StartMinuteOfDay: 14 * 60, Duration: 20 * time.Minute, ResidentID: "resident-a", Path: []string{"hallway", "entrance", "exterior", "garden"}, RepeatEveryDays: 7, RepeatCount: 3}}
	profiles = append(profiles, d)
	e := baseProfile("synthetic_intrusion_30d", "synthetic episodes", 30, 3505)
	e.RoutineTemplates = a.RoutineTemplates
	e.Episodes = []EpisodeTemplate{{ID: "intrusion", Label: LabelSyntheticIntrusion, StartDay: 10, StartMinuteOfDay: 2*60 + 10, Duration: 15 * time.Minute, Path: []string{"exterior", "entrance", "hallway", "bedroom-b"}, RepeatEveryDays: 10, RepeatCount: 2}}
	profiles = append(profiles, e)
	f := baseProfile("degraded_sensors_30d", "partial sensors", 30, 3506)
	f.RoutineTemplates = a.RoutineTemplates
	f.Episodes = []EpisodeTemplate{{ID: "dropout", Label: LabelSensorDropout, StartDay: 4, StartMinuteOfDay: 11 * 60, Duration: 10 * time.Minute, ResidentID: "resident-a", Path: []string{"kitchen", "hallway"}, RepeatEveryDays: 5, RepeatCount: 5}, {ID: "uncertain", Label: LabelIdentityUncertain, StartDay: 8, StartMinuteOfDay: 17 * 60, Duration: 10 * time.Minute, Path: []string{"entrance", "hallway"}, RepeatEveryDays: 6, RepeatCount: 4}, {ID: "topology-missing", Label: LabelTopologyUnavailable, StartDay: 12, StartMinuteOfDay: 20 * 60, Duration: 10 * time.Minute, ResidentID: "resident-a", Path: []string{"living-room", "hallway"}, RepeatEveryDays: 6, RepeatCount: 3}}
	profiles = append(profiles, f)
	g := baseProfile("restart_stress_14d", "daily restart stress", 14, 3507)
	g.RoutineTemplates = a.RoutineTemplates
	g.RestartPolicy = RestartPolicy{EveryDays: 1}
	g.CheckpointPolicy = CheckpointPolicy{EveryDays: 2, BeforeRestart: true}
	profiles = append(profiles, g)
	h := baseProfile("long_memory_90d", "long memory", 90, 3508)
	h.RoutineTemplates = []RoutineTemplate{routine("long-a", "resident-a", weekday, 7*60+30, "bedroom-a", "hallway", "kitchen"), routine("long-weekend", "resident-a", weekend, 9*60, "bedroom-a", "hallway", "entrance", "exterior", "garden")}
	h.Episodes = []EpisodeTemplate{{ID: "long-rare", Label: LabelBenignVariation, StartDay: 21, StartMinuteOfDay: 22 * 60, Duration: 10 * time.Minute, ResidentID: "resident-a", Path: []string{"living-room", "hallway", "entrance"}, RepeatEveryDays: 17, RepeatCount: 5}}
	h.CheckpointPolicy = CheckpointPolicy{EveryDays: 7, BeforeRestart: true}
	profiles = append(profiles, h)
	return profiles
}

func ProfileByID(id string) (Profile, bool) {
	for _, profile := range DefaultProfiles() {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

func (p Profile) Validate() error {
	if strings.TrimSpace(p.ID) == "" || p.StartAt.IsZero() || p.DurationDays <= 0 || p.DurationDays > 366 || p.Timezone == "" || p.RestartPolicy.EveryDays < 0 || p.CheckpointPolicy.EveryDays < 0 {
		return fmt.Errorf("invalid campaign profile")
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("invalid campaign timezone: %w", err)
	}
	if len(p.Nodes) == 0 {
		return fmt.Errorf("campaign topology is empty")
	}
	residentIDs := map[string]bool{}
	for _, resident := range p.Residents {
		if resident.ID == "" || residentIDs[resident.ID] {
			return fmt.Errorf("invalid campaign resident")
		}
		residentIDs[resident.ID] = true
	}
	ids := map[string]bool{}
	for _, node := range p.Nodes {
		if node.ID == "" || ids[node.ID] {
			return fmt.Errorf("invalid campaign node")
		}
		ids[node.ID] = true
	}
	topology := referenceTopology()
	topology.Nodes = make([]cgecontext.Node, 0, len(p.Nodes))
	for _, node := range p.Nodes {
		topology.Nodes = append(topology.Nodes, cgecontext.Node{ID: node.ID, ZoneID: node.ZoneID, Kind: node.Kind, EntryPoint: node.EntryPoint, Exterior: node.Exterior})
	}
	filteredEdges := topology.Edges[:0]
	for _, edge := range topology.Edges {
		if ids[edge.From] && ids[edge.To] {
			filteredEdges = append(filteredEdges, edge)
		}
	}
	topology.Edges = filteredEdges
	topology = cgecontext.CanonicalTopology(topology)
	if err := topology.Validate(); err != nil {
		return err
	}
	for _, template := range p.RoutineTemplates {
		if template.ID == "" || template.ResidentID == "" || !residentIDs[template.ResidentID] || len(template.Path) == 0 || template.StartMinuteOfDay < 0 || template.StartMinuteOfDay >= 1440 || template.VariationMinutes < 0 || template.ProbabilityPermille < 0 || template.ProbabilityPermille > 1000 {
			return fmt.Errorf("invalid routine template %s", template.ID)
		}
		for _, node := range template.Path {
			if !ids[node] {
				return fmt.Errorf("routine path node missing")
			}
		}
		if err := validatePath(template.Path, topology.Edges); err != nil {
			return fmt.Errorf("invalid routine path %s: %w", template.ID, err)
		}
	}
	for _, episode := range p.Episodes {
		if episode.ID == "" || !ValidLabel(episode.Label) || len(episode.Path) == 0 || episode.StartDay < 0 || episode.StartMinuteOfDay < 0 || episode.StartMinuteOfDay >= 1440 || episode.RepeatEveryDays < 0 || episode.RepeatCount < 0 {
			return fmt.Errorf("invalid episode template %s", episode.ID)
		}
		if episode.ResidentID != "" && !residentIDs[episode.ResidentID] {
			return fmt.Errorf("episode resident missing")
		}
		for _, node := range episode.Path {
			if !ids[node] {
				return fmt.Errorf("episode path node missing")
			}
		}
		if err := validatePath(episode.Path, topology.Edges); err != nil {
			return fmt.Errorf("invalid episode path %s: %w", episode.ID, err)
		}
	}
	return nil
}

func validatePath(path []string, edges []cgecontext.Edge) error {
	if len(path) < 2 {
		return nil
	}
	for index := 1; index < len(path); index++ {
		found := false
		for _, edge := range edges {
			if edge.From == path[index-1] && edge.To == path[index] {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing edge %s->%s", path[index-1], path[index])
		}
	}
	return nil
}

func canonicalWeekdays(values []time.Weekday) []time.Weekday {
	out := append([]time.Weekday(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
