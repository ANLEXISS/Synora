package contract

import "time"

// HomeObservation is the canonical, source-neutral observation boundary used
// by the Home World Model. Timestamp is event time; ReceivedAt is ingestion
// time and must not be used as a substitute for it.
type HomeObservation struct {
	ObservationID      string         `json:"observation_id"`
	SourceID           string         `json:"source_id"`
	Timestamp          time.Time      `json:"timestamp"`
	ReceivedAt         time.Time      `json:"received_at"`
	EntityClass        string         `json:"entity_class,omitempty"`
	LocalTrackID       string         `json:"local_track_id,omitempty"`
	Zone               string         `json:"zone,omitempty"`
	Position           *HomePosition  `json:"position,omitempty"`
	IdentityCandidate  string         `json:"identity_candidate,omitempty"`
	IdentityConfidence *float64       `json:"identity_confidence,omitempty"`
	Features           map[string]any `json:"features,omitempty"`
	Confidence         float64        `json:"confidence"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type HomePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z,omitempty"`
}

type HomeSite struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type HomeZone struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
	SiteID   string `json:"site_id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Exterior bool   `json:"exterior,omitempty"`
}

type HomeTransition struct {
	ID       string `json:"id"`
	FromZone string `json:"from_zone"`
	ToZone   string `json:"to_zone"`
	Directed bool   `json:"directed,omitempty"`
}

type HomeSensorCoverage struct {
	SourceID string   `json:"source_id"`
	ZoneIDs  []string `json:"zone_ids"`
}

type HomeRelation struct {
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
}

type HomeTopology struct {
	Sites           []HomeSite           `json:"sites,omitempty"`
	Zones           []HomeZone           `json:"zones"`
	Transitions     []HomeTransition     `json:"transitions,omitempty"`
	SensorCoverages []HomeSensorCoverage `json:"sensor_coverages,omitempty"`
	Relations       []HomeRelation       `json:"relations,omitempty"`
}

type HomeTrackRef struct {
	SourceID      string `json:"source_id"`
	LocalTrackID  string `json:"local_track_id,omitempty"`
	ObservationID string `json:"observation_id"`
}

type HomeIdentityCandidate struct {
	ID         string  `json:"id"`
	Confidence float64 `json:"confidence"`
}

type HomeEvidence struct {
	ObservationIDs []string  `json:"observation_ids,omitempty"`
	SourceIDs      []string  `json:"source_ids,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	Confidence     float64   `json:"confidence"`
	ObservedAt     time.Time `json:"observed_at,omitempty"`
}

type HomeTrajectorySegment struct {
	Zone                   string     `json:"zone"`
	EnteredAt              time.Time  `json:"entered_at"`
	LeftAt                 *time.Time `json:"left_at,omitempty"`
	Confidence             float64    `json:"confidence"`
	SupportingObservations []string   `json:"supporting_observations,omitempty"`
}

type HomeEntity struct {
	ID                 string                  `json:"id"`
	Class              string                  `json:"class"`
	Identity           string                  `json:"identity,omitempty"`
	IdentityCandidates []HomeIdentityCandidate `json:"identity_candidates,omitempty"`
	Location           string                  `json:"location,omitempty"`
	FirstSeen          time.Time               `json:"first_seen"`
	LastSeen           time.Time               `json:"last_seen"`
	Confidence         float64                 `json:"confidence"`
	Status             string                  `json:"status"`
	Tracks             []HomeTrackRef          `json:"tracks,omitempty"`
	Trajectory         []HomeTrajectorySegment `json:"trajectory,omitempty"`
	LikelyPath         []string                `json:"likely_path,omitempty"`
	Evidence           []HomeEvidence          `json:"evidence,omitempty"`
}

type HomePresence struct {
	EntityID       string    `json:"entity_id"`
	Identity       string    `json:"identity,omitempty"`
	State          string    `json:"state"`
	Location       string    `json:"location,omitempty"`
	Confidence     float64   `json:"confidence"`
	LastObservedAt time.Time `json:"last_observed_at,omitempty"`
}

type HomeSituation struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	EntityIDs  []string       `json:"entities,omitempty"`
	Zones      []string       `json:"zones,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	Confidence float64        `json:"confidence"`
	Evidence   []HomeEvidence `json:"evidence,omitempty"`
}

type HomeFact struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Value          map[string]any `json:"value,omitempty"`
	Confidence     float64        `json:"confidence"`
	ObservationIDs []string       `json:"observation_ids,omitempty"`
	Evidence       []HomeEvidence `json:"evidence,omitempty"`
}

type HomeHypothesis struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Status         string         `json:"status"`
	Confidence     float64        `json:"confidence"`
	ObservationIDs []string       `json:"observation_ids,omitempty"`
	Evidence       []HomeEvidence `json:"evidence,omitempty"`
}

type HomeDerivedContext struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Confidence     float64        `json:"confidence"`
	ObservationIDs []string       `json:"observation_ids,omitempty"`
	Evidence       []HomeEvidence `json:"evidence,omitempty"`
}

// HomeModelSnapshot is a stable projection. Observations and implementation
// indexes remain private; conclusions retain enough evidence to explain them.
type HomeModelSnapshot struct {
	Version     string               `json:"version"`
	GeneratedAt time.Time            `json:"generated_at"`
	Topology    HomeTopology         `json:"topology"`
	Entities    []HomeEntity         `json:"entities"`
	Presence    []HomePresence       `json:"presence"`
	Situations  []HomeSituation      `json:"situations"`
	Facts       []HomeFact           `json:"facts,omitempty"`
	Hypotheses  []HomeHypothesis     `json:"hypotheses,omitempty"`
	Derived     []HomeDerivedContext `json:"derived_context,omitempty"`
	WorkingSet  HomeWorkingSet       `json:"working_set"`
}

type HomeWorkingSet struct {
	ObservationCount int `json:"observation_count"`
	EntityCount      int `json:"entity_count"`
	EvidenceCount    int `json:"evidence_count"`
}
