// Package homemodel maintains a bounded, deterministic world model above the
// existing Synora event boundary. It is descriptive only: it creates facts,
// hypotheses and derived context, but it never authorizes an action.
package homemodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/internal/topology"
	"synora/pkg/contract"
)

const (
	Version                  = "home-world-model-v1"
	DefaultObservationWindow = 15 * time.Minute
	DefaultEntityRetention   = 24 * time.Hour
	DefaultCorrelationWindow = 10 * time.Minute
	DefaultActiveWindow      = 45 * time.Second
	DefaultMaxObservations   = 256
	DefaultMaxEntities       = 128
	DefaultMaxEvidence       = 512
	DefaultIdentityThreshold = 0.85
)

type Config struct {
	ObservationWindow time.Duration
	EntityRetention   time.Duration
	CorrelationWindow time.Duration
	ActiveWindow      time.Duration
	MaxObservations   int
	MaxEntities       int
	MaxEvidence       int
	IdentityThreshold float64
}

func DefaultConfig() Config {
	return Config{ObservationWindow: DefaultObservationWindow, EntityRetention: DefaultEntityRetention, CorrelationWindow: DefaultCorrelationWindow, ActiveWindow: DefaultActiveWindow, MaxObservations: DefaultMaxObservations, MaxEntities: DefaultMaxEntities, MaxEvidence: DefaultMaxEvidence, IdentityThreshold: DefaultIdentityThreshold}
}

type TransitionStatus string

const (
	TransitionExpected   TransitionStatus = "expected"
	TransitionPossible   TransitionStatus = "possible"
	TransitionUnlikely   TransitionStatus = "unlikely"
	TransitionImpossible TransitionStatus = "impossible"
	TransitionUnknown    TransitionStatus = "unknown"
)

type ContinuityStatus string

const (
	SameEntity           ContinuityStatus = "same_entity"
	PossibleSameEntity   ContinuityStatus = "possible_same_entity"
	DifferentEntity      ContinuityStatus = "different_entity"
	InsufficientEvidence ContinuityStatus = "insufficient_evidence"
)

type ContinuityAssessment struct {
	EntityID   string                  `json:"entity_id,omitempty"`
	Status     ContinuityStatus        `json:"status"`
	Confidence float64                 `json:"confidence"`
	Signals    map[string]float64      `json:"signals"`
	Evidence   []contract.HomeEvidence `json:"evidence,omitempty"`
}

type persisted struct {
	Version      string                     `json:"version"`
	Topology     contract.HomeTopology      `json:"topology"`
	Observations []contract.HomeObservation `json:"observations"`
	Entities     []contract.HomeEntity      `json:"entities"`
	Facts        []contract.HomeFact        `json:"facts,omitempty"`
	Hypotheses   []contract.HomeHypothesis  `json:"hypotheses,omitempty"`
}

type Model struct {
	mu           sync.Mutex
	cfg          Config
	topology     contract.HomeTopology
	observations map[string]contract.HomeObservation
	entities     map[string]*contract.HomeEntity
	facts        []contract.HomeFact
	hypotheses   []contract.HomeHypothesis
	coverage     map[string]map[string]bool
	loaded       bool
}

func New(topo *topology.Topology, cfg Config) *Model {
	cfg = normalizeConfig(cfg)
	m := &Model{cfg: cfg, observations: map[string]contract.HomeObservation{}, entities: map[string]*contract.HomeEntity{}, coverage: map[string]map[string]bool{}}
	m.topology = topologyFromLegacy(topo)
	return m
}

func normalizeConfig(cfg Config) Config {
	d := DefaultConfig()
	if cfg.ObservationWindow <= 0 {
		cfg.ObservationWindow = d.ObservationWindow
	}
	if cfg.EntityRetention <= 0 {
		cfg.EntityRetention = d.EntityRetention
	}
	if cfg.CorrelationWindow <= 0 {
		cfg.CorrelationWindow = d.CorrelationWindow
	}
	if cfg.ActiveWindow <= 0 {
		cfg.ActiveWindow = d.ActiveWindow
	}
	if cfg.MaxObservations <= 0 {
		cfg.MaxObservations = d.MaxObservations
	}
	if cfg.MaxEntities <= 0 {
		cfg.MaxEntities = d.MaxEntities
	}
	if cfg.MaxEvidence <= 0 {
		cfg.MaxEvidence = d.MaxEvidence
	}
	if cfg.IdentityThreshold <= 0 || cfg.IdentityThreshold > 1 {
		cfg.IdentityThreshold = d.IdentityThreshold
	}
	return cfg
}

// SetSensorCoverage is intentionally source-neutral. A camera, radar, audio
// sensor or future capability can all declare the semantic zones it covers.
func (m *Model) SetSensorCoverage(source string, zones ...string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	if m.coverage[source] == nil {
		m.coverage[source] = map[string]bool{}
	}
	for _, zone := range zones {
		if zone = strings.TrimSpace(zone); zone != "" {
			m.coverage[source][zone] = true
		}
	}
	m.refreshCoverageLocked()
}

func (m *Model) SetTopology(topo *topology.Topology) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topology = topologyFromLegacy(topo)
	m.refreshCoverageLocked()
}

func (m *Model) SetConfig(cfg Config) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = normalizeConfig(cfg)
	at := time.Unix(0, 0).UTC()
	for _, observation := range m.observations {
		if observation.Timestamp.After(at) {
			at = observation.Timestamp
		}
	}
	m.rebuildLocked(at)
}

func (m *Model) Load(raw json.RawMessage) error {
	if m == nil || len(raw) == 0 {
		return nil
	}
	var value persisted
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode home model: %w", err)
	}
	if value.Version != "" && value.Version != Version {
		return fmt.Errorf("unsupported home model version %q", value.Version)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(value.Topology.Zones) > 0 {
		m.topology = cloneHomeTopology(value.Topology)
	}
	m.observations = map[string]contract.HomeObservation{}
	for _, observation := range value.Observations {
		if observation.ObservationID != "" {
			m.observations[observation.ObservationID] = cloneObservation(observation)
		}
	}
	m.entities = map[string]*contract.HomeEntity{}
	for _, entity := range value.Entities {
		copy := cloneEntity(entity)
		if copy.ID != "" {
			m.entities[copy.ID] = &copy
		}
	}
	m.facts = append([]contract.HomeFact(nil), value.Facts...)
	m.hypotheses = append([]contract.HomeHypothesis(nil), value.Hypotheses...)
	m.loaded = true
	return nil
}

func (m *Model) Marshal() json.RawMessage {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	observations := sortedObservations(m.observations)
	entities := sortedEntities(m.entities)
	data, err := json.Marshal(persisted{Version: Version, Topology: cloneHomeTopology(m.topology), Observations: observations, Entities: entities, Facts: cloneFacts(m.facts), Hypotheses: cloneHypotheses(m.hypotheses)})
	if err != nil {
		return nil
	}
	return data
}

// Snapshot returns a defensive projection without recording an observation.
func (m *Model) Snapshot() *contract.HomeModelSnapshot {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	at := time.Unix(0, 0).UTC()
	for _, observation := range m.observations {
		if observation.Timestamp.After(at) {
			at = observation.Timestamp
		}
	}
	return m.snapshotLocked(at)
}

// Observe normalizes one existing Synora event and updates the model. The
// result is deterministic for identical event/model inputs; wall clock time
// is never used to fill event time.
func (m *Model) Observe(event *contract.Event) (*contract.HomeModelSnapshot, json.RawMessage) {
	if m == nil || event == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	observation := NormalizeObservation(event)
	if observation.ObservationID == "" {
		return m.snapshotLocked(observation.Timestamp), m.marshalLocked()
	}
	if _, duplicate := m.observations[observation.ObservationID]; !duplicate {
		m.observations[observation.ObservationID] = observation
	}
	currentAt := observation.Timestamp
	if currentAt.IsZero() {
		currentAt = observation.ReceivedAt
	}
	for _, known := range m.observations {
		if known.Timestamp.After(currentAt) {
			currentAt = known.Timestamp
		}
	}
	m.rebuildLocked(currentAt)
	return m.snapshotLocked(currentAt), m.marshalLocked()
}

func boolValue(values map[string]any, key string) bool {
	value, ok := values[key]
	if !ok {
		return false
	}
	result, ok := value.(bool)
	return ok && result
}

func NormalizeObservation(event *contract.Event) contract.HomeObservation {
	if event == nil {
		return contract.HomeObservation{}
	}
	metadata := cloneMap(event.Payload)
	observationID := strings.TrimSpace(event.ID)
	if observationID == "" {
		observationID = deterministicID("observation", event.Source, event.DeviceID, event.TrackID, event.Timestamp.UTC().Format(time.RFC3339Nano), event.Type)
	}
	source := strings.TrimSpace(event.DeviceID)
	if source == "" {
		source = strings.TrimSpace(event.Source)
	}
	zone := stringValue(metadata, "zone", "zone_id", "node_id", "node")
	if zone == "" {
		zone = strings.TrimSpace(event.NodeID)
	}
	identity := strings.TrimSpace(event.Identity)
	if identity == "" {
		identity = strings.TrimSpace(event.ResidentID)
	}
	if identity == "" {
		identity = stringValue(metadata, "identity_candidate", "identity", "resident_id", "best_match")
	}
	confidence := event.Confidence
	if confidence == 0 {
		confidence = floatValue(metadata["confidence"])
	}
	confidence = clamp(confidence)
	identityConfidence := floatValuePointer(metadata, "identity_confidence")
	if identityConfidence == nil && identity != "" && event.Confidence > 0 {
		identityConfidence = floatPointer(clamp(event.Confidence))
	}
	entityClass := stringValue(metadata, "entity_class", "class", "subject_class")
	if entityClass == "" {
		switch contract.NormalizeEventType(event.Type) {
		case contract.EventVisionIdentity, contract.EventVisionUnknown, contract.EventVisionUncertain:
			entityClass = "person"
		default:
			entityClass = "unknown"
		}
	}
	features := mapValue(metadata["features"])
	delete(metadata, "features")
	received := event.ReceivedAt
	if received.IsZero() {
		received = event.Timestamp
	}
	return contract.HomeObservation{ObservationID: observationID, SourceID: source, Timestamp: event.Timestamp.UTC(), ReceivedAt: received.UTC(), EntityClass: entityClass, LocalTrackID: strings.TrimSpace(event.TrackID), Zone: zone, Position: positionValue(metadata["position"]), IdentityCandidate: identity, IdentityConfidence: identityConfidence, Features: features, Confidence: confidence, Metadata: metadata}
}

func (m *Model) rebuildLocked(reference time.Time) {
	items := sortedObservations(m.observations)
	if reference.IsZero() && len(items) > 0 {
		reference = items[len(items)-1].Timestamp
	}
	if reference.IsZero() {
		reference = time.Unix(0, 0).UTC()
	}
	for id, observation := range m.observations {
		if !observation.Timestamp.IsZero() && reference.Sub(observation.Timestamp) > m.cfg.ObservationWindow {
			delete(m.observations, id)
		}
	}
	items = sortedObservations(m.observations)
	if len(items) > m.cfg.MaxObservations {
		for _, observation := range items[:len(items)-m.cfg.MaxObservations] {
			delete(m.observations, observation.ObservationID)
		}
		items = items[len(items)-m.cfg.MaxObservations:]
	}
	entities := map[string]*contract.HomeEntity{}
	for _, observation := range items {
		candidate, assessment := m.bestEntityLocked(observation, entities)
		if candidate == nil {
			id := deterministicID("entity", observation.ObservationID)
			candidate = &contract.HomeEntity{ID: id, Class: observation.EntityClass, FirstSeen: observation.Timestamp, Status: "active"}
			entities[id] = candidate
		}
		m.addObservationLocked(candidate, observation, assessment)
	}
	m.entities = entities
	m.trimEntitiesLocked(reference)
	for _, entity := range m.entities {
		if entity.Status == "departed" {
			continue
		}
		if reference.Sub(entity.LastSeen) > m.cfg.ActiveWindow {
			entity.Status = "lost"
		} else {
			entity.Status = "active"
		}
	}
	m.rebuildKnowledgeLocked(items, reference)
}

func (m *Model) bestEntityLocked(observation contract.HomeObservation, entities map[string]*contract.HomeEntity) (*contract.HomeEntity, ContinuityAssessment) {
	var best *contract.HomeEntity
	bestAssessment := ContinuityAssessment{Status: InsufficientEvidence, Signals: map[string]float64{}}
	var second float64
	for _, entity := range sortedEntityPointers(entities) {
		assessment := m.assessLocked(observation, entity)
		if assessment.Status == DifferentEntity || assessment.Status == InsufficientEvidence {
			continue
		}
		if assessment.Confidence > bestAssessment.Confidence || (assessment.Confidence == bestAssessment.Confidence && (best == nil || entity.ID < best.ID)) {
			second = bestAssessment.Confidence
			best, bestAssessment = entity, assessment
		} else if assessment.Confidence > second {
			second = assessment.Confidence
		}
	}
	minimum := .62
	// A blind spot removes topology evidence. Keep a plausible continuation
	// when the remaining temporal/source signals support it; the trajectory
	// still records UNKNOWN rather than fabricating an intermediate zone.
	if observation.Zone == "" {
		minimum = .55
	}
	if best == nil || bestAssessment.Confidence < minimum || bestAssessment.Confidence-second < .05 {
		if best != nil && bestAssessment.Confidence >= .45 {
			bestAssessment.Status = PossibleSameEntity
		}
		return nil, bestAssessment
	}
	bestAssessment.Status = SameEntity
	return best, bestAssessment
}

func (m *Model) AssessContinuity(observation contract.HomeObservation, entity contract.HomeEntity) ContinuityAssessment {
	if m == nil {
		return ContinuityAssessment{Status: InsufficientEvidence}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.assessLocked(observation, &entity)
}

// TransitionStatus classifies semantic movement without turning an unlikely
// or impossible path into an intrusion decision.
func (m *Model) TransitionStatus(from, to string) TransitionStatus {
	if m == nil {
		return TransitionUnknown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.transitionStatus(from, to)
}

func (m *Model) Topology() contract.HomeTopology {
	if m == nil {
		return contract.HomeTopology{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneHomeTopology(m.topology)
}

func (m *Model) assessLocked(observation contract.HomeObservation, entity *contract.HomeEntity) ContinuityAssessment {
	assessment := ContinuityAssessment{Status: InsufficientEvidence, Signals: map[string]float64{}}
	if entity == nil || entity.LastSeen.IsZero() || observation.Timestamp.IsZero() {
		return assessment
	}
	assessment.EntityID = entity.ID
	if observation.IdentityCandidate != "" && entity.Identity != "" && observation.IdentityCandidate != entity.Identity {
		assessment.Status = DifferentEntity
		return assessment
	}
	if observation.IdentityCandidate == "" && entity.Identity != "" && len(entity.Tracks) > 0 {
		last := entity.Tracks[len(entity.Tracks)-1]
		if observation.SourceID != last.SourceID || observation.LocalTrackID != last.LocalTrackID {
			assessment.Status = DifferentEntity
			return assessment
		}
	}
	delta := observation.Timestamp.Sub(entity.LastSeen)
	if delta < 0 {
		delta = -delta
	}
	if delta == 0 && observation.IdentityCandidate == "" {
		last := entity.Tracks[len(entity.Tracks)-1]
		petFusion := entity.Class == "pet" && observation.EntityClass == "pet" && m.featureSimilarityLocked(entity, observation) >= 1
		if (observation.SourceID != last.SourceID || observation.LocalTrackID != last.LocalTrackID) && !petFusion {
			assessment.Status = DifferentEntity
			return assessment
		}
	}
	if delta > m.cfg.CorrelationWindow {
		assessment.Status = DifferentEntity
		return assessment
	}
	temporal := 1 - delta.Seconds()/m.cfg.CorrelationWindow.Seconds()
	if delta == 0 && observation.LocalTrackID == "" {
		temporal = 0
	}
	topologySignal := .35
	transition := m.transitionStatus(entity.Location, observation.Zone)
	switch transition {
	case TransitionExpected:
		topologySignal = 1
	case TransitionPossible:
		topologySignal = .72
	case TransitionUnlikely:
		topologySignal = .12
	case TransitionImpossible:
		topologySignal = 0
	case TransitionUnknown:
		topologySignal = .25
	}
	identitySignal := .45
	if observation.IdentityCandidate != "" && entity.Identity == observation.IdentityCandidate {
		identitySignal = 1
	}
	appearanceSignal := m.featureSimilarityLocked(entity, observation)
	movementSignal := .35
	if len(entity.Tracks) > 0 && entity.Tracks[len(entity.Tracks)-1].SourceID == observation.SourceID {
		movementSignal = .65
	}
	if observation.LocalTrackID != "" && len(entity.Tracks) > 0 && entity.Tracks[len(entity.Tracks)-1].LocalTrackID == observation.LocalTrackID {
		movementSignal = 1
	}
	assessment.Signals["temporal_compatibility"] = clamp(temporal)
	assessment.Signals["topology_compatibility"] = topologySignal
	assessment.Signals["identity_similarity"] = identitySignal
	assessment.Signals["appearance_similarity"] = appearanceSignal
	assessment.Signals["movement_consistency"] = movementSignal
	assessment.Confidence = clamp(.25*temporal + .25*topologySignal + .25*identitySignal + .15*appearanceSignal + .10*movementSignal)
	if transition == TransitionImpossible {
		assessment.Status = DifferentEntity
		assessment.Confidence = 0
		return assessment
	}
	if assessment.Confidence >= .62 {
		assessment.Status = SameEntity
	} else if assessment.Confidence >= .45 {
		assessment.Status = PossibleSameEntity
	}
	assessment.Evidence = []contract.HomeEvidence{{ObservationIDs: []string{observation.ObservationID}, SourceIDs: []string{observation.SourceID}, Reason: string(assessment.Status), Confidence: assessment.Confidence, ObservedAt: observation.Timestamp}}
	return assessment
}

func (m *Model) addObservationLocked(entity *contract.HomeEntity, observation contract.HomeObservation, assessment ContinuityAssessment) {
	if entity.Class == "" || entity.Class == "unknown" {
		entity.Class = observation.EntityClass
	}
	if entity.FirstSeen.IsZero() || observation.Timestamp.Before(entity.FirstSeen) {
		entity.FirstSeen = observation.Timestamp
	}
	if observation.Timestamp.After(entity.LastSeen) {
		entity.LastSeen = observation.Timestamp
		entity.Location = observation.Zone
	}
	entity.Confidence = max(entity.Confidence, observation.Confidence)
	if observation.IdentityCandidate != "" {
		identityConfidence := observation.Confidence
		if observation.IdentityConfidence != nil {
			identityConfidence = *observation.IdentityConfidence
		}
		upsertIdentityCandidate(&entity.IdentityCandidates, observation.IdentityCandidate, identityConfidence)
		if identityConfidence >= m.cfg.IdentityThreshold && entity.Identity == "" {
			entity.Identity = observation.IdentityCandidate
		}
	}
	if observation.Timestamp.Add(m.cfg.ActiveWindow).Before(entity.LastSeen) {
		entity.Status = "lost"
	} else {
		entity.Status = "active"
	}
	if boolValue(observation.Metadata, "departed") {
		entity.Status = "departed"
	}
	ref := contract.HomeTrackRef{SourceID: observation.SourceID, LocalTrackID: observation.LocalTrackID, ObservationID: observation.ObservationID}
	if !containsTrack(entity.Tracks, ref) {
		entity.Tracks = append(entity.Tracks, ref)
	}
	if assessment.Evidence != nil {
		entity.Evidence = append(entity.Evidence, assessment.Evidence...)
	}
	m.rebuildTrajectoryLocked(entity)
}

func (m *Model) rebuildTrajectoryLocked(entity *contract.HomeEntity) {
	obs := make([]contract.HomeObservation, 0)
	for _, ref := range entity.Tracks {
		if value, ok := m.observations[ref.ObservationID]; ok {
			obs = append(obs, value)
		}
	}
	sort.Slice(obs, func(i, j int) bool {
		if obs[i].Timestamp.Equal(obs[j].Timestamp) {
			return obs[i].ObservationID < obs[j].ObservationID
		}
		return obs[i].Timestamp.Before(obs[j].Timestamp)
	})
	entity.Trajectory = nil
	for _, observation := range obs {
		zone := strings.TrimSpace(observation.Zone)
		if zone == "" {
			zone = "UNKNOWN"
		}
		if len(entity.Trajectory) == 0 || entity.Trajectory[len(entity.Trajectory)-1].Zone != zone {
			if len(entity.Trajectory) > 0 {
				left := observation.Timestamp
				entity.Trajectory[len(entity.Trajectory)-1].LeftAt = &left
			}
			entity.Trajectory = append(entity.Trajectory, contract.HomeTrajectorySegment{Zone: zone, EnteredAt: observation.Timestamp, Confidence: observation.Confidence, SupportingObservations: []string{observation.ObservationID}})
		} else {
			segment := &entity.Trajectory[len(entity.Trajectory)-1]
			segment.Confidence = max(segment.Confidence, observation.Confidence)
			segment.SupportingObservations = append(segment.SupportingObservations, observation.ObservationID)
		}
	}
	entity.LikelyPath = nil
	for i := 1; i < len(entity.Trajectory); i++ {
		from, to := entity.Trajectory[i-1].Zone, entity.Trajectory[i].Zone
		if from == "UNKNOWN" || to == "UNKNOWN" {
			continue
		}
		path := m.shortestPath(from, to)
		if len(path) > 2 {
			entity.LikelyPath = append(entity.LikelyPath, path[1:len(path)-1]...)
		}
	}
}

func (m *Model) rebuildKnowledgeLocked(items []contract.HomeObservation, reference time.Time) {
	m.facts = nil
	m.hypotheses = nil
	for _, entity := range sortedEntityPointers(m.entities) {
		for _, segment := range entity.Trajectory {
			m.facts = append(m.facts, contract.HomeFact{ID: deterministicID("fact-location", entity.ID, segment.EnteredAt.Format(time.RFC3339Nano), segment.Zone), Type: "entity.located", Value: map[string]any{"entity_id": entity.ID, "zone": segment.Zone}, Confidence: segment.Confidence, ObservationIDs: append([]string(nil), segment.SupportingObservations...), Evidence: []contract.HomeEvidence{{ObservationIDs: append([]string(nil), segment.SupportingObservations...), Reason: "semantic trajectory", Confidence: segment.Confidence, ObservedAt: segment.EnteredAt}}})
		}
		for i := 1; i < len(entity.Trajectory); i++ {
			from, to := entity.Trajectory[i-1].Zone, entity.Trajectory[i].Zone
			status := m.transitionStatus(from, to)
			if status == TransitionImpossible {
				m.hypotheses = append(m.hypotheses, contract.HomeHypothesis{ID: deterministicID("hypothesis-transition", entity.ID, from, to), Type: "topology_transition", Status: "contradicted", Confidence: .9, ObservationIDs: append([]string(nil), entity.Trajectory[i].SupportingObservations...), Evidence: []contract.HomeEvidence{{ObservationIDs: append([]string(nil), entity.Trajectory[i].SupportingObservations...), Reason: string(status), Confidence: .9}}})
			}
			if status == TransitionUnknown {
				m.hypotheses = append(m.hypotheses, contract.HomeHypothesis{ID: deterministicID("hypothesis-path", entity.ID, from, to), Type: "likely_path", Status: "open", Confidence: .25, ObservationIDs: append([]string(nil), entity.Trajectory[i].SupportingObservations...), Evidence: []contract.HomeEvidence{{ObservationIDs: append([]string(nil), entity.Trajectory[i].SupportingObservations...), Reason: string(status), Confidence: .25}}})
			}
		}
		if len(entity.IdentityCandidates) > 1 {
			m.hypotheses = append(m.hypotheses, contract.HomeHypothesis{ID: deterministicID("hypothesis-identity", entity.ID), Type: "identity_conflict", Status: "open", Confidence: entity.IdentityCandidates[0].Confidence, Evidence: entity.Evidence})
		}
	}
	for _, observation := range items {
		if observation.IdentityCandidate != "" && observation.IdentityConfidence != nil && *observation.IdentityConfidence < m.cfg.IdentityThreshold {
			m.hypotheses = append(m.hypotheses, contract.HomeHypothesis{ID: deterministicID("hypothesis-identity-candidate", observation.ObservationID), Type: "identity_candidate", Status: "open", Confidence: *observation.IdentityConfidence, ObservationIDs: []string{observation.ObservationID}, Evidence: []contract.HomeEvidence{{ObservationIDs: []string{observation.ObservationID}, SourceIDs: []string{observation.SourceID}, Reason: "candidate below certainty threshold", Confidence: *observation.IdentityConfidence, ObservedAt: observation.Timestamp}}})
		}
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if !items[i].Timestamp.Equal(items[j].Timestamp) {
				break
			}
			if items[i].Zone == items[j].Zone && items[i].IdentityCandidate != items[j].IdentityCandidate {
				m.hypotheses = append(m.hypotheses, contract.HomeHypothesis{ID: deterministicID("hypothesis-sensor-disagreement", items[i].ObservationID, items[j].ObservationID), Type: "sensor_disagreement", Status: "open", Confidence: min(items[i].Confidence, items[j].Confidence), ObservationIDs: []string{items[i].ObservationID, items[j].ObservationID}, Evidence: []contract.HomeEvidence{{ObservationIDs: []string{items[i].ObservationID, items[j].ObservationID}, SourceIDs: []string{items[i].SourceID, items[j].SourceID}, Reason: "same-time observations disagree", Confidence: min(items[i].Confidence, items[j].Confidence), ObservedAt: items[i].Timestamp}}})
			}
		}
	}
	m.facts = trimFacts(m.facts, m.cfg.MaxEvidence)
	m.hypotheses = trimHypotheses(m.hypotheses, m.cfg.MaxEvidence)
	_ = reference
}

func (m *Model) snapshotLocked(at time.Time) *contract.HomeModelSnapshot {
	snapshot := &contract.HomeModelSnapshot{Version: Version, GeneratedAt: at.UTC(), Topology: cloneHomeTopology(m.topology), Entities: sortedEntities(m.entities), Facts: cloneFacts(m.facts), Hypotheses: cloneHypotheses(m.hypotheses), WorkingSet: contract.HomeWorkingSet{ObservationCount: len(m.observations), EntityCount: len(m.entities)}}
	snapshot.Presence, snapshot.Situations = m.presenceAndSituationsLocked(at)
	snapshot.WorkingSet.EvidenceCount = evidenceCount(snapshot)
	return snapshot
}

func (m *Model) presenceAndSituationsLocked(at time.Time) ([]contract.HomePresence, []contract.HomeSituation) {
	entities := sortedEntityPointers(m.entities)
	presence := make([]contract.HomePresence, 0, len(entities))
	situations := make([]contract.HomeSituation, 0)
	for _, entity := range entities {
		state := "unknown"
		if entity.Status == "departed" {
			state = "absence"
		} else if !entity.LastSeen.IsZero() && at.Sub(entity.LastSeen) <= m.cfg.ActiveWindow {
			state = "observed_presence"
		} else if entity.Identity != "" {
			state = "unknown"
		}
		presence = append(presence, contract.HomePresence{EntityID: entity.ID, Identity: entity.Identity, State: state, Location: entity.Location, Confidence: entity.Confidence, LastObservedAt: entity.LastSeen})
		if entity.LastSeen.IsZero() {
			continue
		}
		for i := 1; i < len(entity.Trajectory); i++ {
			before, current := entity.Trajectory[i-1], entity.Trajectory[i]
			if before.Zone == "UNKNOWN" || current.Zone == "UNKNOWN" {
				continue
			}
			fromExterior, toExterior := m.isExterior(before.Zone), m.isExterior(current.Zone)
			typeName := "property_transition"
			if fromExterior && !toExterior {
				typeName = "resident_returned"
				if entity.Identity == "" {
					typeName = "unknown_approaching"
				}
			}
			if !fromExterior && toExterior {
				typeName = "entity_departed"
			}
			confidence := min(before.Confidence, current.Confidence)
			situations = append(situations, contract.HomeSituation{ID: deterministicID("situation", typeName, entity.ID, current.EnteredAt.Format(time.RFC3339Nano)), Type: typeName, EntityIDs: []string{entity.ID}, Zones: []string{before.Zone, current.Zone}, StartedAt: current.EnteredAt, UpdatedAt: current.EnteredAt, Confidence: confidence, Evidence: []contract.HomeEvidence{{ObservationIDs: append([]string(nil), current.SupportingObservations...), Reason: "semantic transition", Confidence: confidence, ObservedAt: current.EnteredAt}}})
		}
		if entity.Identity == "" && entity.Status == "active" {
			situations = append(situations, contract.HomeSituation{ID: deterministicID("situation", "unknown_remaining", entity.ID), Type: "unknown_remaining", EntityIDs: []string{entity.ID}, Zones: []string{entity.Location}, StartedAt: entity.FirstSeen, UpdatedAt: entity.LastSeen, Confidence: entity.Confidence, Evidence: entity.Evidence})
		}
	}
	sort.Slice(situations, func(i, j int) bool {
		if situations[i].Type == situations[j].Type {
			return situations[i].ID < situations[j].ID
		}
		return situations[i].Type < situations[j].Type
	})
	return presence, situations
}

func (m *Model) transitionStatus(from, to string) TransitionStatus {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == "UNKNOWN" || to == "UNKNOWN" {
		return TransitionUnknown
	}
	if from == to {
		return TransitionExpected
	}
	if len(m.topology.Zones) == 0 {
		return TransitionUnknown
	}
	known := map[string]bool{}
	for _, zone := range m.topology.Zones {
		known[zone.ID] = true
	}
	if !known[from] || !known[to] {
		return TransitionUnknown
	}
	distance, reachable := m.pathDistance(from, to)
	if !reachable {
		return TransitionImpossible
	}
	if distance == 1 {
		return TransitionExpected
	}
	if distance == 2 {
		return TransitionPossible
	}
	return TransitionUnlikely
}

func (m *Model) pathDistance(from, to string) (int, bool) {
	ids := map[string]bool{}
	for _, zone := range m.topology.Zones {
		ids[zone.ID] = true
	}
	if !ids[from] || !ids[to] {
		return 0, false
	}
	neighbors := map[string][]string{}
	for _, edge := range m.topology.Transitions {
		neighbors[edge.FromZone] = append(neighbors[edge.FromZone], edge.ToZone)
		if !edge.Directed {
			neighbors[edge.ToZone] = append(neighbors[edge.ToZone], edge.FromZone)
		}
	}
	for id := range neighbors {
		sort.Strings(neighbors[id])
	}
	type step struct {
		id       string
		distance int
	}
	queue := []step{{from, 0}}
	seen := map[string]bool{from: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.id == to {
			return current.distance, true
		}
		for _, next := range neighbors[current.id] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, step{next, current.distance + 1})
			}
		}
	}
	return 0, false
}

func (m *Model) shortestPath(from, to string) []string {
	if from == to {
		return []string{from}
	}
	ids := map[string]bool{}
	for _, zone := range m.topology.Zones {
		ids[zone.ID] = true
	}
	if !ids[from] || !ids[to] {
		return nil
	}
	neighbors := map[string][]string{}
	for _, edge := range m.topology.Transitions {
		neighbors[edge.FromZone] = append(neighbors[edge.FromZone], edge.ToZone)
		if !edge.Directed {
			neighbors[edge.ToZone] = append(neighbors[edge.ToZone], edge.FromZone)
		}
	}
	for id := range neighbors {
		sort.Strings(neighbors[id])
	}
	queue := []string{from}
	previous := map[string]string{from: ""}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range neighbors[current] {
			if _, ok := previous[next]; ok {
				continue
			}
			previous[next] = current
			if next == to {
				path := []string{to}
				for path[len(path)-1] != "" {
					parent := previous[path[len(path)-1]]
					if parent != "" {
						path = append(path, parent)
					} else {
						break
					}
				}
				reverseStrings(path)
				return path
			}
			queue = append(queue, next)
		}
	}
	return nil
}

func (m *Model) isExterior(zone string) bool {
	for _, value := range m.topology.Zones {
		if value.ID == zone {
			return value.Exterior
		}
	}
	return false
}

func (m *Model) refreshCoverageLocked() {
	for source, zones := range m.coverage {
		found := false
		for i := range m.topology.SensorCoverages {
			if m.topology.SensorCoverages[i].SourceID == source {
				m.topology.SensorCoverages[i].ZoneIDs = nil
				for zone := range zones {
					m.topology.SensorCoverages[i].ZoneIDs = append(m.topology.SensorCoverages[i].ZoneIDs, zone)
				}
				sort.Strings(m.topology.SensorCoverages[i].ZoneIDs)
				found = true
			}
		}
		if !found {
			ids := make([]string, 0, len(zones))
			for zone := range zones {
				ids = append(ids, zone)
			}
			sort.Strings(ids)
			m.topology.SensorCoverages = append(m.topology.SensorCoverages, contract.HomeSensorCoverage{SourceID: source, ZoneIDs: ids})
		}
	}
	sort.Slice(m.topology.SensorCoverages, func(i, j int) bool {
		return m.topology.SensorCoverages[i].SourceID < m.topology.SensorCoverages[j].SourceID
	})
	m.topology.Relations = topologyRelations(m.topology)
}

func topologyFromLegacy(value *topology.Topology) contract.HomeTopology {
	result := contract.HomeTopology{Zones: []contract.HomeZone{}, Transitions: []contract.HomeTransition{}, SensorCoverages: []contract.HomeSensorCoverage{}}
	if value == nil {
		return result
	}
	ids := make([]string, 0, len(value.Nodes))
	for id := range value.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	zoneIDs := map[string]bool{}
	for _, id := range ids {
		node := value.Nodes[id]
		if node == nil || node.Type == topology.NodeDevice {
			continue
		}
		kind := string(node.Type)
		exterior := strings.Contains(strings.ToLower(kind), "exterior")
		if configured, ok := node.Metadata["exterior"].(bool); ok {
			exterior = configured
		}
		parentID := ""
		if node.Parent != nil {
			parentID = node.Parent.ID
		}
		result.Zones = append(result.Zones, contract.HomeZone{ID: id, Name: node.Name, ParentID: parentID, Kind: kind, Exterior: exterior})
		zoneIDs[id] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		node := value.Nodes[id]
		if node == nil {
			continue
		}
		links := append([]string(nil), node.Connect...)
		if node.Parent != nil {
			links = append(links, node.Parent.ID)
		}
		for _, target := range links {
			if !zoneIDs[target] {
				continue
			}
			a, b := id, target
			if a > b {
				a, b = b, a
			}
			key := a + "\x00" + b
			if seen[key] || a == b {
				continue
			}
			seen[key] = true
			result.Transitions = append(result.Transitions, contract.HomeTransition{ID: deterministicID("transition", a, b), FromZone: a, ToZone: b})
		}
	}
	sort.Slice(result.Transitions, func(i, j int) bool { return result.Transitions[i].ID < result.Transitions[j].ID })
	result.Relations = topologyRelations(result)
	return result
}

func topologyRelations(value contract.HomeTopology) []contract.HomeRelation {
	result := make([]contract.HomeRelation, 0, len(value.Zones)+len(value.Transitions)+len(value.SensorCoverages))
	for _, zone := range value.Zones {
		if zone.ParentID != "" {
			result = append(result, contract.HomeRelation{Type: "inside", From: zone.ID, To: zone.ParentID})
		}
	}
	for _, transition := range value.Transitions {
		result = append(result, contract.HomeRelation{Type: "adjacent_to", From: transition.FromZone, To: transition.ToZone})
		result = append(result, contract.HomeRelation{Type: "connects_to", From: transition.FromZone, To: transition.ToZone})
	}
	for _, coverage := range value.SensorCoverages {
		for _, zone := range coverage.ZoneIDs {
			result = append(result, contract.HomeRelation{Type: "covers", From: coverage.SourceID, To: zone})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Type == result[j].Type {
			if result[i].From == result[j].From {
				return result[i].To < result[j].To
			}
			return result[i].From < result[j].From
		}
		return result[i].Type < result[j].Type
	})
	return result
}

func (m *Model) marshalLocked() json.RawMessage {
	data, err := json.Marshal(persisted{Version: Version, Topology: cloneHomeTopology(m.topology), Observations: sortedObservations(m.observations), Entities: sortedEntities(m.entities), Facts: cloneFacts(m.facts), Hypotheses: cloneHypotheses(m.hypotheses)})
	if err != nil {
		return nil
	}
	return data
}

func deterministicID(prefix string, values ...string) string {
	h := sha256.New()
	h.Write([]byte(prefix))
	for _, value := range values {
		h.Write([]byte{0})
		h.Write([]byte(value))
	}
	return prefix + "-" + hex.EncodeToString(h.Sum(nil))[:12]
}
func clamp(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func floatPointer(value float64) *float64 { return &value }
func floatValue(value any) float64 {
	switch value := value.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		v, _ := value.Float64()
		return v
	}
	return 0
}
func floatValuePointer(m map[string]any, key string) *float64 {
	if value, ok := m[key]; ok {
		return floatPointer(clamp(floatValue(value)))
	}
	return nil
}
func stringValue(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func mapValue(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return cloneMap(result)
	}
	return nil
}
func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
func positionValue(value any) *contract.HomePosition {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return &contract.HomePosition{X: floatValue(m["x"]), Y: floatValue(m["y"]), Z: floatValue(m["z"])}
}
func cloneObservation(value contract.HomeObservation) contract.HomeObservation {
	value.Features = cloneMap(value.Features)
	value.Metadata = cloneMap(value.Metadata)
	if value.Position != nil {
		p := *value.Position
		value.Position = &p
	}
	if value.IdentityConfidence != nil {
		v := *value.IdentityConfidence
		value.IdentityConfidence = &v
	}
	return value
}
func cloneHomeTopology(value contract.HomeTopology) contract.HomeTopology {
	value.Sites = append([]contract.HomeSite(nil), value.Sites...)
	value.Zones = append([]contract.HomeZone(nil), value.Zones...)
	value.Transitions = append([]contract.HomeTransition(nil), value.Transitions...)
	value.SensorCoverages = append([]contract.HomeSensorCoverage(nil), value.SensorCoverages...)
	value.Relations = append([]contract.HomeRelation(nil), value.Relations...)
	for i := range value.SensorCoverages {
		value.SensorCoverages[i].ZoneIDs = append([]string(nil), value.SensorCoverages[i].ZoneIDs...)
	}
	return value
}
func cloneEntity(value contract.HomeEntity) contract.HomeEntity {
	value.IdentityCandidates = append([]contract.HomeIdentityCandidate(nil), value.IdentityCandidates...)
	value.Tracks = append([]contract.HomeTrackRef(nil), value.Tracks...)
	value.Trajectory = append([]contract.HomeTrajectorySegment(nil), value.Trajectory...)
	value.LikelyPath = append([]string(nil), value.LikelyPath...)
	value.Evidence = append([]contract.HomeEvidence(nil), value.Evidence...)
	for i := range value.Trajectory {
		value.Trajectory[i].SupportingObservations = append([]string(nil), value.Trajectory[i].SupportingObservations...)
	}
	return value
}
func sortedObservations(value map[string]contract.HomeObservation) []contract.HomeObservation {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]contract.HomeObservation, 0, len(keys))
	for _, key := range keys {
		out = append(out, cloneObservation(value[key]))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].ObservationID < out[j].ObservationID
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}
func sortedEntities(value map[string]*contract.HomeEntity) []contract.HomeEntity {
	out := make([]contract.HomeEntity, 0, len(value))
	for _, item := range sortedEntityPointers(value) {
		out = append(out, cloneEntity(*item))
	}
	return out
}
func sortedEntityPointers(value map[string]*contract.HomeEntity) []*contract.HomeEntity {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]*contract.HomeEntity, 0, len(keys))
	for _, key := range keys {
		if value[key] != nil {
			out = append(out, value[key])
		}
	}
	return out
}
func cloneFacts(value []contract.HomeFact) []contract.HomeFact {
	out := append([]contract.HomeFact(nil), value...)
	for i := range out {
		out[i].ObservationIDs = append([]string(nil), value[i].ObservationIDs...)
		out[i].Evidence = append([]contract.HomeEvidence(nil), value[i].Evidence...)
	}
	return out
}
func cloneHypotheses(value []contract.HomeHypothesis) []contract.HomeHypothesis {
	out := append([]contract.HomeHypothesis(nil), value...)
	for i := range out {
		out[i].ObservationIDs = append([]string(nil), value[i].ObservationIDs...)
		out[i].Evidence = append([]contract.HomeEvidence(nil), value[i].Evidence...)
	}
	return out
}
func trimFacts(value []contract.HomeFact, limit int) []contract.HomeFact {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
func trimHypotheses(value []contract.HomeHypothesis, limit int) []contract.HomeHypothesis {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
func (m *Model) trimEntitiesLocked(reference time.Time) {
	for id, entity := range m.entities {
		if !entity.LastSeen.IsZero() && reference.Sub(entity.LastSeen) > m.cfg.EntityRetention {
			delete(m.entities, id)
		}
	}
	if len(m.entities) <= m.cfg.MaxEntities {
		return
	}
	items := sortedEntityPointers(m.entities)
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastSeen.Equal(items[j].LastSeen) {
			return items[i].ID < items[j].ID
		}
		return items[i].LastSeen.Before(items[j].LastSeen)
	})
	for len(m.entities) > m.cfg.MaxEntities {
		delete(m.entities, items[0].ID)
		items = items[1:]
	}
}
func containsTrack(values []contract.HomeTrackRef, target contract.HomeTrackRef) bool {
	for _, value := range values {
		if value.SourceID == target.SourceID && value.LocalTrackID == target.LocalTrackID && value.ObservationID == target.ObservationID {
			return true
		}
	}
	return false
}
func upsertIdentityCandidate(values *[]contract.HomeIdentityCandidate, id string, confidence float64) {
	for i := range *values {
		if (*values)[i].ID == id {
			if confidence > (*values)[i].Confidence {
				(*values)[i].Confidence = confidence
			}
			return
		}
	}
	*values = append(*values, contract.HomeIdentityCandidate{ID: id, Confidence: clamp(confidence)})
	sort.Slice(*values, func(i, j int) bool { return (*values)[i].ID < (*values)[j].ID })
}
func (m *Model) featureSimilarityLocked(entity *contract.HomeEntity, observation contract.HomeObservation) float64 {
	if len(observation.Features) == 0 {
		return .5
	}
	if len(entity.Tracks) == 0 {
		return .5
	}
	previous, ok := m.observations[entity.Tracks[len(entity.Tracks)-1].ObservationID]
	if !ok || len(previous.Features) == 0 {
		return .5
	}
	keys, equal := 0, 0
	for key, value := range observation.Features {
		other, exists := previous.Features[key]
		if !exists {
			continue
		}
		keys++
		if fmt.Sprint(value) == fmt.Sprint(other) {
			equal++
		}
	}
	if keys == 0 {
		return .5
	}
	return float64(equal) / float64(keys)
}
func evidenceCount(snapshot *contract.HomeModelSnapshot) int {
	count := 0
	for _, entity := range snapshot.Entities {
		count += len(entity.Evidence)
	}
	for _, item := range snapshot.Facts {
		count += len(item.Evidence)
	}
	for _, item := range snapshot.Hypotheses {
		count += len(item.Evidence)
	}
	for _, item := range snapshot.Situations {
		count += len(item.Evidence)
	}
	return count
}
func reverseStrings(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}
