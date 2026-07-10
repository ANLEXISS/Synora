package graph

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"synora/internal/engine/contracts"
)

const (
	OriginReal         = "real"
	OriginCriticalSeed = "critical_seed"
	OriginSimulation   = "simulation"
)

type criticalSeedFile struct {
	CriticalChains []contracts.CriticalSeed `yaml:"critical_chains"`
}

func LoadCriticalSeeds(path string) ([]contracts.CriticalSeed, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file criticalSeedFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	seeds := make([]contracts.CriticalSeed, 0, len(file.CriticalChains))
	for _, seed := range file.CriticalChains {
		normalized, err := normalizeCriticalSeed(seed)
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, normalized)
	}
	return seeds, nil
}

func normalizeCriticalSeed(seed contracts.CriticalSeed) (contracts.CriticalSeed, error) {
	seed.ID = strings.TrimSpace(seed.ID)
	seed.Name = strings.TrimSpace(seed.Name)
	seed.RiskLevel = strings.TrimSpace(seed.RiskLevel)
	seed.ExpectedState = strings.TrimSpace(seed.ExpectedState)
	if seed.ID == "" {
		return seed, errors.New("critical seed id is required")
	}
	if len(seed.Sequence) == 0 {
		return seed, errors.New("critical seed sequence is required")
	}
	for i := range seed.Sequence {
		seed.Sequence[i].EventType = strings.TrimSpace(seed.Sequence[i].EventType)
		seed.Sequence[i].ZoneRole = strings.TrimSpace(seed.Sequence[i].ZoneRole)
		if seed.Sequence[i].EventType == "" {
			return seed, errors.New("critical seed sequence event_type is required")
		}
	}
	seed.ProposedActions = uniqueStrings(seed.ProposedActions)
	seed.ForbiddenActions = uniqueStrings(seed.ForbiddenActions)
	if seed.Context == nil {
		seed.Context = map[string]any{}
	}
	return seed, nil
}

func (g *GraphMemory) ApplyCriticalSeeds(seeds []contracts.CriticalSeed) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.criticalSeeds == nil {
		g.criticalSeeds = make(map[string]contracts.CriticalSeed)
	}
	for _, seed := range seeds {
		normalized, err := normalizeCriticalSeed(seed)
		if err != nil || !normalized.Enabled {
			continue
		}
		g.criticalSeeds[normalized.ID] = normalized
		g.upsertCriticalSeedLocked(normalized)
	}
	g.pruneInspectionLocked()
}

func (g *GraphMemory) CriticalSeeds() []contracts.CriticalSeed {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]contracts.CriticalSeed, 0, len(g.criticalSeeds))
	for _, seed := range g.criticalSeeds {
		out = append(out, seed)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *GraphMemory) CriticalSeed(id string) (contracts.CriticalSeed, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	seed, ok := g.criticalSeeds[strings.TrimSpace(id)]
	return seed, ok
}

func (g *GraphMemory) upsertCriticalSeedLocked(seed contracts.CriticalSeed) {
	signature := seedSignature(seed)
	key := "critical_seed:" + seed.ID
	now := time.Now().UTC()
	sequence := g.learnedSequences[key]
	if sequence == nil {
		sequence = &contracts.LearnedSequence{
			ID:          "seq-seed-" + shortHash(seed.ID),
			Signature:   signature,
			EventTypes:  seedEventTypes(seed),
			SourceTypes: []string{OriginCriticalSeed},
			Examples:    []string{},
			Evidence:    []string{},
		}
		g.learnedSequences[key] = sequence
	}
	sequence.Name = seed.Name
	sequence.Count = 1 + sequence.RealCount
	sequence.CriticalSeedCount = 1
	sequence.SeedCount = 1
	sequence.EffectiveCount = sequence.Count
	sequence.ConfidenceBase = seed.DangerScore
	sequence.Confidence = maxFloat(sequence.Confidence, seed.DangerScore)
	sequence.Origin = OriginCriticalSeed
	sequence.CriticalSeedID = seed.ID
	sequence.DangerScore = seed.DangerScore
	sequence.RiskLevel = seed.RiskLevel
	sequence.ExpectedState = seed.ExpectedState
	if sequence.FirstSeen.IsZero() {
		sequence.FirstSeen = now
	}
	if sequence.LastSeen.IsZero() {
		sequence.LastSeen = now
	}
	sequence.Evidence = appendLimited(sequence.Evidence, "critical_seed:"+seed.ID, CGEMaxEvidencePerSequence)

	behaviorID := "beh-seed-" + shortHash(seed.ID)
	behavior := g.learnedBehaviors[behaviorID]
	if behavior == nil {
		behavior = &contracts.LearnedBehavior{
			ID:                       behaviorID,
			TriggerSequenceSignature: signature,
			Context:                  map[string]any{"origin": OriginCriticalSeed, "critical_seed_id": seed.ID},
			Evidence:                 []string{},
		}
		g.learnedBehaviors[behaviorID] = behavior
	}
	behavior.Count = sequence.Count
	behavior.CriticalSeedCount = 1
	behavior.SeedCount = 1
	behavior.EffectiveCount = sequence.EffectiveCount
	behavior.ConfidenceBase = seed.DangerScore
	behavior.Confidence = maxFloat(behavior.Confidence, seed.DangerScore)
	behavior.Origin = OriginCriticalSeed
	behavior.CriticalSeedID = seed.ID
	behavior.DangerScore = seed.DangerScore
	behavior.RiskLevel = seed.RiskLevel
	behavior.ExpectedState = seed.ExpectedState
	behavior.Status = "observing"
	behavior.RequiresValidation = seed.RequiresValidation
	behavior.ProposedActions = proposedActionMaps(seed.ProposedActions, seed.ForbiddenActions)
	behavior.ForbiddenActions = append([]string(nil), seed.ForbiddenActions...)
	behavior.Evidence = appendLimited(behavior.Evidence, "critical_seed:"+seed.ID, CGEMaxEvidencePerBehavior)
}

func (g *GraphMemory) observeCriticalSeedLocked(scope string, current learnedEvent) *contracts.CriticalSeedMatch {
	if current.EventType == "" {
		return nil
	}
	if current.Simulated && current.TestRunID != "" {
		scope = "simulation:" + current.TestRunID
	}
	events := g.seedRecentEvents[scope]
	events = append(events, current)
	if maxLen := g.maxCriticalSeedLengthLocked(); maxLen > 0 && len(events) > maxLen {
		events = events[len(events)-maxLen:]
	}
	g.seedRecentEvents[scope] = events

	match := g.matchCriticalSeedLocked(events)
	if match == nil {
		return nil
	}
	if !current.Simulated {
		g.recordCriticalSeedRealMatchLocked(match.CriticalSeedID, current.Timestamp)
	}
	return match
}

func (g *GraphMemory) matchCriticalSeedLocked(events []learnedEvent) *contracts.CriticalSeedMatch {
	var best *contracts.CriticalSeed
	for _, seed := range g.criticalSeeds {
		if !seed.Enabled || len(seed.Sequence) == 0 || len(seed.Sequence) > len(events) {
			continue
		}
		if !criticalSeedMatchesEvents(seed, events[len(events)-len(seed.Sequence):]) {
			continue
		}
		current := seed
		if best == nil ||
			len(current.Sequence) > len(best.Sequence) ||
			(len(current.Sequence) == len(best.Sequence) && current.DangerScore > best.DangerScore) {
			best = &current
		}
	}
	if best == nil {
		return nil
	}
	return &contracts.CriticalSeedMatch{
		CriticalSeedID:      best.ID,
		Name:                best.Name,
		Signature:           seedSignature(*best),
		ExpectedState:       best.ExpectedState,
		ActualState:         best.ExpectedState,
		ExpectedDangerScore: best.DangerScore,
		RiskLevel:           best.RiskLevel,
		Passed:              true,
	}
}

func criticalSeedMatchesEvents(seed contracts.CriticalSeed, events []learnedEvent) bool {
	if len(seed.Sequence) != len(events) {
		return false
	}
	for i, step := range seed.Sequence {
		if step.EventType != events[i].EventType {
			return false
		}
		if step.ZoneRole != "" && step.ZoneRole != events[i].ZoneRole {
			return false
		}
	}
	return criticalSeedContextMatches(seed, events)
}

func criticalSeedContextMatches(seed contracts.CriticalSeed, events []learnedEvent) bool {
	if len(seed.Context) == 0 {
		return true
	}
	last := events[len(events)-1]
	for key, expected := range seed.Context {
		expectedText := strings.TrimSpace(toString(expected))
		if expectedText == "" {
			continue
		}
		switch key {
		case "house_state":
			if last.HouseState != expectedText {
				return false
			}
		case "time_window":
			if last.TimeWindow != expectedText {
				return false
			}
		case "zone_scope":
			if last.ZoneScope != expectedText {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (g *GraphMemory) recordCriticalSeedRealMatchLocked(seedID string, at time.Time) {
	_, ok := g.criticalSeeds[seedID]
	if !ok {
		return
	}
	key := "critical_seed:" + seedID
	sequence := g.learnedSequences[key]
	if sequence != nil {
		sequence.RealCount++
		sequence.Count = sequence.CriticalSeedCount + sequence.RealCount
		sequence.EffectiveCount = sequence.Count
		sequence.LastSeen = at
		sequence.Confidence = maxFloat(sequence.ConfidenceBase, confidence(sequence.EffectiveCount))
	}
	behavior := g.learnedBehaviors["beh-seed-"+shortHash(seedID)]
	if behavior != nil {
		behavior.RealCount++
		behavior.Count = behavior.CriticalSeedCount + behavior.RealCount
		behavior.EffectiveCount = behavior.Count
		behavior.LastMatchedAt = at
		behavior.Confidence = maxFloat(behavior.ConfidenceBase, confidence(behavior.EffectiveCount))
	}
}

func (g *GraphMemory) maxCriticalSeedLengthLocked() int {
	maxLen := 0
	for _, seed := range g.criticalSeeds {
		if seed.Enabled && len(seed.Sequence) > maxLen {
			maxLen = len(seed.Sequence)
		}
	}
	if maxLen < 3 {
		return 3
	}
	return maxLen
}

func seedSignature(seed contracts.CriticalSeed) string {
	return strings.Join(seedEventTypes(seed), " > ")
}

func seedEventTypes(seed contracts.CriticalSeed) []string {
	out := make([]string, 0, len(seed.Sequence))
	for _, step := range seed.Sequence {
		out = append(out, step.EventType)
	}
	return out
}

func proposedActionMaps(proposed []string, forbidden []string) []map[string]any {
	forbiddenSet := map[string]bool{}
	for _, action := range forbidden {
		forbiddenSet[action] = true
	}
	out := make([]map[string]any, 0, len(proposed))
	for _, action := range proposed {
		action = strings.TrimSpace(action)
		if action == "" || forbiddenSet[action] {
			continue
		}
		out = append(out, map[string]any{"action": action})
	}
	return out
}

func maxFloat(left float64, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func toString(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}
