package cge

import (
	"context"
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
	if strings.TrimSpace(s.SituationID) == "" || len(s.Observations) == 0 || len(s.Observations) > 256 {
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
		confidence := 1.0
		if len(seed.Sequence) > 1 {
			confidence = 0.5 + 0.5*float64(len(seed.Sequence))/float64(len(seed.Sequence)+1)
		}
		result = append(result, CognitiveChainMatch{ChainID: seed.ID, MatchedObservationIDs: matched, MatchedSteps: len(matched), TotalSteps: len(seed.Sequence), MatchConfidence: confidence, Scope: "critical/" + seed.ID, MatchCodes: []string{"sequence_complete", "ordered", "within_window"}})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].MatchedSteps != result[j].MatchedSteps {
			return result[i].MatchedSteps > result[j].MatchedSteps
		}
		if result[i].Scope != result[j].Scope {
			return len(result[i].Scope) > len(result[j].Scope)
		}
		return result[i].ChainID < result[j].ChainID
	})
	return result, nil
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
	return CognitiveChainCatalogSnapshot{Revision: s.Revision, CriticalSeeds: append([]contracts.CriticalSeed(nil), s.CriticalSeeds...), LearnedBehaviors: append([]contracts.LearnedBehavior(nil), s.LearnedBehaviors...), LearnedSequences: append([]contracts.LearnedSequence(nil), s.LearnedSequences...), CapturedAt: s.CapturedAt}
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
	return p(ctx)
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
