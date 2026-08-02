package cge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	contracts "synora/internal/engine/contracts"
)

const cognitiveChainWindow = 15 * time.Minute

// CognitiveObservationSnapshot is the bounded observation history used by
// the CGE matcher. It deliberately contains no raw event or mutable Core
// object.
type CognitiveObservationSnapshot struct {
	ID          string
	EventType   string
	Timestamp   time.Time
	NodeID      string
	ZoneID      string
	ZoneRole    string
	EntityID    string
	DeviceID    string
	ClipID      string
	SequenceKey string
}

type CognitiveSituationSnapshot struct {
	SituationID          string
	EpisodeID            string
	CurrentObservationID string
	Observations         []CognitiveObservationSnapshot
	CapturedAt           time.Time
}

func (s CognitiveSituationSnapshot) Validate() error {
	if strings.TrimSpace(s.SituationID) == "" || len(s.Observations) == 0 || len(s.Observations) > 256 || s.CapturedAt.IsZero() || strings.TrimSpace(s.CurrentObservationID) == "" {
		return ErrInvalidChainCandidate
	}
	if len(s.SituationID) > 256 || len(s.EpisodeID) > 256 || len(s.CurrentObservationID) > 256 {
		return ErrInvalidChainCandidate
	}
	for i, observation := range s.Observations {
		if strings.TrimSpace(observation.ID) == "" || strings.TrimSpace(observation.EventType) == "" || observation.Timestamp.IsZero() || len(observation.ID) > 256 || len(observation.EventType) > 256 {
			return ErrInvalidChainCandidate
		}
		if i > 0 && observation.Timestamp.Before(s.Observations[i-1].Timestamp) {
			return fmt.Errorf("%w: cognitive observations are not ordered", ErrInvalidChainCandidate)
		}
	}
	currentFound := false
	for _, observation := range s.Observations {
		if observation.ID == s.CurrentObservationID {
			currentFound = true
			break
		}
	}
	if !currentFound {
		return ErrInvalidChainCandidate
	}
	return nil
}

func (s CognitiveSituationSnapshot) clone() CognitiveSituationSnapshot {
	s.Observations = append([]CognitiveObservationSnapshot(nil), s.Observations...)
	return s
}

type CognitiveChainMatch struct {
	ChainID               string
	MatchedObservationIDs []string
	MatchedSteps          int
	TotalSteps            int
	MatchConfidence       float64
	Scope                 string
	MatchCodes            []string
}

func (m CognitiveChainMatch) Validate() error {
	if strings.TrimSpace(m.ChainID) == "" || m.MatchedSteps <= 0 || m.TotalSteps <= 0 || m.MatchedSteps != m.TotalSteps || len(m.MatchedObservationIDs) != m.MatchedSteps || !boundedUnitFloat(m.MatchConfidence) || len(m.Scope) > 256 || len(m.MatchCodes) > 16 {
		return ErrInvalidChainCandidate
	}
	return nil
}

type CognitiveChainMatcher interface {
	Match(context.Context, CognitiveSituationSnapshot, []contracts.CriticalSeed) ([]CognitiveChainMatch, error)
}

type CriticalChainMatcher struct {
	Window time.Duration
}

func (m CriticalChainMatcher) Match(ctx context.Context, situation CognitiveSituationSnapshot, seeds []contracts.CriticalSeed) ([]CognitiveChainMatch, error) {
	if err := situation.Validate(); err != nil {
		return nil, err
	}
	window := m.Window
	if window <= 0 || window > cognitiveChainWindow {
		window = cognitiveChainWindow
	}
	observations := append([]CognitiveObservationSnapshot(nil), situation.Observations...)
	result := make([]CognitiveChainMatch, 0)
	type rankedMatch struct {
		match       CognitiveChainMatch
		specificity int
		scopeDepth  int
		dangerScore float64
	}
	ranked := make([]rankedMatch, 0)
	for _, seed := range seeds {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if !seed.Enabled || seed.DeletedAt != nil || len(seed.Sequence) == 0 {
			continue
		}
		matched := make([]string, 0, len(seed.Sequence))
		matchedObservations := make([]CognitiveObservationSnapshot, 0, len(seed.Sequence))
		stepIndex := 0
		for _, observation := range observations {
			if stepIndex >= len(seed.Sequence) {
				break
			}
			step := seed.Sequence[stepIndex]
			zoneRole := observation.ZoneRole
			if zoneRole == "" {
				zoneRole = inferredZoneRole(observation)
			}
			if observation.EventType != step.EventType || (step.ZoneRole != "" && zoneRole != step.ZoneRole) {
				continue
			}
			matched = append(matched, observation.ID)
			matchedObservations = append(matchedObservations, observation)
			stepIndex++
		}
		if stepIndex != len(seed.Sequence) {
			continue
		}
		first, last := observations[0].Timestamp, observations[0].Timestamp
		byID := make(map[string]CognitiveObservationSnapshot, len(observations))
		for _, observation := range observations {
			byID[observation.ID] = observation
		}
		first = byID[matched[0]].Timestamp
		last = byID[matched[len(matched)-1]].Timestamp
		if last.Before(first) || last.Sub(first) > window {
			continue
		}
		if matched[len(matched)-1] != situation.CurrentObservationID {
			continue
		}
		if !continuousObservations(matchedObservations) || !criticalContextMatches(seed, matchedObservations) {
			continue
		}
		confidence := 1.0
		if len(seed.Sequence) > 1 {
			confidence = 0.5 + 0.5*float64(len(seed.Sequence))/float64(len(seed.Sequence)+1)
		}
		scope := criticalSeedScope(seed)
		codes := []string{"sequence_complete", "ordered", "within_window", "continuity_satisfied"}
		if specificity := criticalSeedSpecificity(seed); specificity > 0 {
			codes = append(codes, "specificity_"+fmt.Sprint(specificity))
		}
		ranked = append(ranked, rankedMatch{
			match:       CognitiveChainMatch{ChainID: seed.ID, MatchedObservationIDs: matched, MatchedSteps: len(matched), TotalSteps: len(seed.Sequence), MatchConfidence: confidence, Scope: scope, MatchCodes: codes},
			specificity: criticalSeedSpecificity(seed), scopeDepth: scopeDepth(scope), dangerScore: seed.DangerScore,
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].match.MatchedSteps != ranked[j].match.MatchedSteps {
			return ranked[i].match.MatchedSteps > ranked[j].match.MatchedSteps
		}
		if ranked[i].specificity != ranked[j].specificity {
			return ranked[i].specificity > ranked[j].specificity
		}
		if ranked[i].scopeDepth != ranked[j].scopeDepth {
			return ranked[i].scopeDepth > ranked[j].scopeDepth
		}
		if ranked[i].dangerScore != ranked[j].dangerScore {
			return ranked[i].dangerScore > ranked[j].dangerScore
		}
		return ranked[i].match.ChainID < ranked[j].match.ChainID
	})
	for _, value := range ranked {
		result = append(result, value.match)
	}
	return result, nil
}

func continuousObservations(observations []CognitiveObservationSnapshot) bool {
	if len(observations) < 2 {
		return true
	}
	sequenceKey, entityID := "", ""
	for _, observation := range observations {
		if observation.SequenceKey != "" {
			if sequenceKey == "" {
				sequenceKey = observation.SequenceKey
			} else if sequenceKey != observation.SequenceKey {
				return false
			}
		}
		if observation.EntityID != "" {
			if entityID == "" {
				entityID = observation.EntityID
			} else if entityID != observation.EntityID {
				return false
			}
		}
	}
	return true
}

func criticalContextMatches(seed contracts.CriticalSeed, observations []CognitiveObservationSnapshot) bool {
	if len(seed.Context) == 0 {
		return true
	}
	for key, raw := range seed.Context {
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		switch key {
		case "node_id", "node":
			for _, observation := range observations {
				if observation.NodeID != value {
					return false
				}
			}
		case "zone_id", "zone":
			for _, observation := range observations {
				if observation.ZoneID != value {
					return false
				}
			}
		case "entity_id", "entity":
			for _, observation := range observations {
				if observation.EntityID != value {
					return false
				}
			}
		case "device_id", "device":
			for _, observation := range observations {
				if observation.DeviceID != value {
					return false
				}
			}
		case "sequence_key":
			for _, observation := range observations {
				if observation.SequenceKey != value {
					return false
				}
			}
		}
	}
	return true
}

func criticalSeedScope(seed contracts.CriticalSeed) string {
	if value, ok := seed.Context["scope"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return "critical/" + seed.ID
}

func criticalSeedSpecificity(seed contracts.CriticalSeed) int {
	specificity := 0
	for _, step := range seed.Sequence {
		if step.EventType != "" {
			specificity++
		}
		if step.ZoneRole != "" {
			specificity++
		}
	}
	for key := range seed.Context {
		if key != "scope" {
			specificity++
		}
	}
	return specificity
}

func scopeDepth(scope string) int {
	depth := 0
	for _, part := range strings.Split(strings.Trim(scope, "/"), "/") {
		if strings.TrimSpace(part) != "" {
			depth++
		}
	}
	return depth
}

func inferredZoneRole(observation CognitiveObservationSnapshot) string {
	value := strings.ToLower(observation.ZoneID + " " + observation.NodeID)
	switch {
	case strings.Contains(value, "entrance") || strings.Contains(value, "entry") || strings.Contains(value, "door"):
		return "entrance"
	case strings.Contains(value, "hall") || strings.Contains(value, "corridor"):
		return "hallway"
	case strings.Contains(value, "private") || strings.Contains(value, "bedroom"):
		return "private_room"
	case strings.Contains(value, "living") || strings.Contains(value, "salon"):
		return "living_area"
	default:
		return ""
	}
}

type CognitiveChainCatalogSnapshot struct {
	Revision         uint64
	CriticalSeeds    []contracts.CriticalSeed
	LearnedBehaviors []contracts.LearnedBehavior
	LearnedSequences []contracts.LearnedSequence
	CapturedAt       time.Time
}

func (s CognitiveChainCatalogSnapshot) Clone() CognitiveChainCatalogSnapshot {
	clone := CognitiveChainCatalogSnapshot{Revision: s.Revision, CapturedAt: s.CapturedAt}
	clone.CriticalSeeds = cloneCriticalSeeds(s.CriticalSeeds)
	clone.LearnedBehaviors = cloneLearnedBehaviors(s.LearnedBehaviors)
	clone.LearnedSequences = cloneLearnedSequences(s.LearnedSequences)
	return clone
}

func cloneCriticalSeeds(values []contracts.CriticalSeed) []contracts.CriticalSeed {
	return cloneContractSlice(values)
}

func cloneLearnedBehaviors(values []contracts.LearnedBehavior) []contracts.LearnedBehavior {
	return cloneContractSlice(values)
}

func cloneLearnedSequences(values []contracts.LearnedSequence) []contracts.LearnedSequence {
	return cloneContractSlice(values)
}

func cloneContractSlice[T any](values []T) []T {
	if values == nil {
		return nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return append([]T(nil), values...)
	}
	var clone []T
	if err := json.Unmarshal(data, &clone); err != nil {
		return append([]T(nil), values...)
	}
	return clone
}

func (s CognitiveChainCatalogSnapshot) Validate() error {
	if s.Revision == 0 || s.CapturedAt.IsZero() {
		return ErrInvalidChainCandidate
	}
	return nil
}

type CognitiveChainCatalogProvider interface {
	Snapshot(context.Context) (CognitiveChainCatalogSnapshot, error)
}

// FunctionalCognitiveChainCatalogProvider lets the Core expose its current
// detached catalog without giving CGE ownership of the engine.
type FunctionalCognitiveChainCatalogProvider func(context.Context) (CognitiveChainCatalogSnapshot, error)

func (p FunctionalCognitiveChainCatalogProvider) Snapshot(ctx context.Context) (CognitiveChainCatalogSnapshot, error) {
	if p == nil {
		return CognitiveChainCatalogSnapshot{}, ErrNoDecisionChain
	}
	snapshot, err := p(ctx)
	if err != nil {
		return CognitiveChainCatalogSnapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return CognitiveChainCatalogSnapshot{}, err
	}
	return snapshot.Clone(), nil
}

type StaticCognitiveChainCatalogProvider struct {
	mu       sync.RWMutex
	snapshot CognitiveChainCatalogSnapshot
}

func NewStaticCognitiveChainCatalogProvider(snapshot CognitiveChainCatalogSnapshot) (*StaticCognitiveChainCatalogProvider, error) {
	if snapshot.Revision == 0 {
		snapshot.Revision = 1
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	return &StaticCognitiveChainCatalogProvider{snapshot: snapshot.Clone()}, snapshot.Validate()
}

func (p *StaticCognitiveChainCatalogProvider) Snapshot(ctx context.Context) (CognitiveChainCatalogSnapshot, error) {
	if p == nil {
		return CognitiveChainCatalogSnapshot{}, ErrNoDecisionChain
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return CognitiveChainCatalogSnapshot{}, ctx.Err()
		default:
		}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshot.Clone(), nil
}

func (p *StaticCognitiveChainCatalogProvider) Replace(snapshot CognitiveChainCatalogSnapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if snapshot.Revision <= p.snapshot.Revision {
		return fmt.Errorf("%w: catalog revision is not increasing", ErrInvalidChainCandidate)
	}
	p.snapshot = snapshot.Clone()
	return nil
}
