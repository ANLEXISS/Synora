package graph

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"synora/internal/configfile"
	"synora/internal/engine/contracts"
)

const (
	OriginReal                     = "real"
	OriginCriticalSeed             = "critical_seed"
	OriginSimulation               = "simulation"
	CriticalSeedVersion            = 1
	MinimumCriticalSeedDangerScore = 0.65
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
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}
	return NormalizeCriticalSeeds(file.CriticalChains, false)
}

func normalizeCriticalSeed(seed contracts.CriticalSeed) (contracts.CriticalSeed, error) {
	return normalizeCriticalSeedWithOptions(seed, false, time.Now().UTC())
}

func normalizeCriticalSeedWithOptions(seed contracts.CriticalSeed, allowLowScore bool, now time.Time) (contracts.CriticalSeed, error) {
	seed.ID = strings.TrimSpace(seed.ID)
	seed.Name = strings.TrimSpace(seed.Name)
	seed.Description = strings.TrimSpace(seed.Description)
	seed.RiskLevel = strings.TrimSpace(seed.RiskLevel)
	seed.ExpectedState = strings.TrimSpace(seed.ExpectedState)
	if seed.ID == "" {
		return seed, errors.New("critical seed id is required")
	}
	if seed.Name == "" {
		return seed, errors.New("critical seed name is required")
	}
	if math.IsNaN(seed.DangerScore) || math.IsInf(seed.DangerScore, 0) || seed.DangerScore < 0 || seed.DangerScore > 1 {
		return seed, errors.New("critical seed danger_score must be between 0 and 1")
	}
	if seed.DangerScore < MinimumCriticalSeedDangerScore && !allowLowScore && !seed.AllowLowScore {
		return seed, fmt.Errorf("critical seed danger_score must be at least %.2f", MinimumCriticalSeedDangerScore)
	}
	if !validRiskLevel(seed.RiskLevel) {
		return seed, fmt.Errorf("invalid critical seed risk_level %q", seed.RiskLevel)
	}
	if !validExpectedState(seed.ExpectedState) {
		return seed, fmt.Errorf("invalid critical seed expected_state %q", seed.ExpectedState)
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
	if err := validateSeedActions(seed); err != nil {
		return seed, err
	}
	if seed.Context == nil {
		seed.Context = map[string]any{}
	}
	if seed.Version <= 0 {
		seed.Version = CriticalSeedVersion
	}
	if seed.CreatedAt.IsZero() {
		seed.CreatedAt = now
	}
	if seed.UpdatedAt.IsZero() {
		seed.UpdatedAt = seed.CreatedAt
	}
	if seed.DeletedAt != nil {
		deletedAt := seed.DeletedAt.UTC()
		seed.DeletedAt = &deletedAt
		seed.Enabled = false
	}
	return seed, nil
}

func NormalizeCriticalSeeds(seeds []contracts.CriticalSeed, allowLowScore bool) ([]contracts.CriticalSeed, error) {
	now := time.Now().UTC()
	out := make([]contracts.CriticalSeed, 0, len(seeds))
	seen := make(map[string]bool, len(seeds))
	for _, seed := range seeds {
		normalized, err := normalizeCriticalSeedWithOptions(seed, allowLowScore, now)
		if err != nil {
			return nil, err
		}
		if seen[normalized.ID] {
			return nil, fmt.Errorf("duplicate critical seed id %q", normalized.ID)
		}
		seen[normalized.ID] = true
		out = append(out, normalized)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func SaveCriticalSeeds(path string, seeds []contracts.CriticalSeed) error {
	normalized, err := NormalizeCriticalSeeds(seeds, false)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(criticalSeedFile{CriticalChains: normalized})
	if err != nil {
		return err
	}
	return configfile.WriteAtomicWithBackup(path, data, 0o640)
}

func (g *GraphMemory) ApplyCriticalSeeds(seeds []contracts.CriticalSeed) {
	_ = g.ReplaceCriticalSeeds(seeds, false)
}

func (g *GraphMemory) ReplaceCriticalSeeds(seeds []contracts.CriticalSeed, allowLowScore bool) error {
	normalizedSeeds, err := NormalizeCriticalSeeds(seeds, allowLowScore)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	oldIDs := make(map[string]bool, len(g.criticalSeeds))
	for id := range g.criticalSeeds {
		oldIDs[id] = true
	}
	g.criticalSeeds = make(map[string]contracts.CriticalSeed, len(normalizedSeeds))
	for _, seed := range normalizedSeeds {
		g.criticalSeeds[seed.ID] = cloneCriticalSeed(seed)
		delete(oldIDs, seed.ID)
		if seed.Enabled && seed.DeletedAt == nil {
			g.upsertCriticalSeedLocked(seed)
		} else {
			g.deactivateCriticalSeedLocked(seed.ID, false)
		}
	}
	for id := range oldIDs {
		g.deactivateCriticalSeedLocked(id, true)
	}
	g.pruneInspectionLocked()
	return nil
}

func (g *GraphMemory) CriticalSeeds() []contracts.CriticalSeed {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]contracts.CriticalSeed, 0, len(g.criticalSeeds))
	for _, seed := range g.criticalSeeds {
		out = append(out, cloneCriticalSeed(seed))
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
	return cloneCriticalSeed(seed), ok
}

func (g *GraphMemory) upsertCriticalSeedLocked(seed contracts.CriticalSeed) {
	signature := seedSignature(seed)
	key := "critical_seed:" + seed.ID
	now := seed.UpdatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
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
	sequence.Confidence = maxFloat(seed.DangerScore, confidence(sequence.EffectiveCount))
	sequence.Origin = OriginCriticalSeed
	sequence.CriticalSeedID = seed.ID
	sequence.DangerScore = seed.DangerScore
	sequence.RiskLevel = seed.RiskLevel
	sequence.ExpectedState = seed.ExpectedState
	if sequence.FirstSeen.IsZero() {
		sequence.FirstSeen = seed.CreatedAt
		if sequence.FirstSeen.IsZero() {
			sequence.FirstSeen = now
		}
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
			CreatedAt:                seed.CreatedAt,
		}
		g.learnedBehaviors[behaviorID] = behavior
	}
	behavior.Count = sequence.Count
	behavior.CriticalSeedCount = 1
	behavior.SeedCount = 1
	behavior.EffectiveCount = sequence.EffectiveCount
	behavior.ConfidenceBase = seed.DangerScore
	behavior.Confidence = maxFloat(seed.DangerScore, confidence(behavior.EffectiveCount))
	behavior.Origin = OriginCriticalSeed
	behavior.CriticalSeedID = seed.ID
	behavior.DangerScore = seed.DangerScore
	behavior.RiskLevel = seed.RiskLevel
	behavior.ExpectedState = seed.ExpectedState
	behavior.Status = "observing"
	behavior.RequiresValidation = seed.RequiresValidation
	behavior.ProposedActions = proposedActionMaps(seed.ProposedActions, seed.ForbiddenActions)
	behavior.ForbiddenActions = append([]string(nil), seed.ForbiddenActions...)
	behavior.Enabled = true
	behavior.Forgotten = false
	behavior.UserNotes = ""
	behavior.ConfidenceOverride = nil
	behavior.RiskOverride = nil
	behavior.UserFeedback = contracts.UserFeedback{}
	behavior.UpdatedAt = now
	if behavior.CreatedAt.IsZero() {
		behavior.CreatedAt = seed.CreatedAt
		if behavior.CreatedAt.IsZero() {
			behavior.CreatedAt = now
		}
	}
	if behavior.Context == nil {
		behavior.Context = map[string]any{}
	}
	behavior.Context["origin"] = OriginCriticalSeed
	behavior.Context["critical_seed_id"] = seed.ID
	delete(behavior.Context, "seed_disabled")
	behavior.Evidence = appendLimited(behavior.Evidence, "critical_seed:"+seed.ID, CGEMaxEvidencePerBehavior)
	g.applyBehaviorOverrideLocked(behavior)
}

func (g *GraphMemory) deactivateCriticalSeedLocked(seedID string, removed bool) {
	behavior := g.learnedBehaviors["beh-seed-"+shortHash(seedID)]
	if behavior == nil {
		return
	}
	behavior.Enabled = false
	behavior.Status = "disabled"
	behavior.UpdatedAt = time.Now().UTC()
	if behavior.Context == nil {
		behavior.Context = map[string]any{}
	}
	behavior.Context["seed_disabled"] = true
	if removed {
		behavior.Context["critical_seed_removed"] = true
	}
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

func validRiskLevel(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "medium_high", "high", "critical":
		return true
	default:
		return false
	}
}

func validExpectedState(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "idle", "activity", "suspicious", "intrusion", "break_in", "emergency":
		return true
	default:
		return false
	}
}

func validateSeedActions(seed contracts.CriticalSeed) error {
	forbidden := make(map[string]bool, len(seed.ForbiddenActions))
	for _, action := range seed.ForbiddenActions {
		forbidden[canonicalActionName(action)] = true
	}
	for _, action := range seed.ProposedActions {
		canonical := canonicalActionName(action)
		if canonical == "" {
			continue
		}
		if forbidden[canonical] {
			return fmt.Errorf("%w: proposed action %q is forbidden by critical seed", ErrForbiddenAction, canonical)
		}
		if isAlwaysForbiddenAutomaticAction(canonical) {
			return fmt.Errorf("%w: automatic action %q is forbidden", ErrForbiddenAction, canonical)
		}
		if canonical == "siren.turn_on" && !seed.RequiresValidation {
			return fmt.Errorf("%w: siren.turn_on requires validation", ErrForbiddenAction)
		}
	}
	return nil
}

func cloneCriticalSeed(seed contracts.CriticalSeed) contracts.CriticalSeed {
	seed.Sequence = append([]contracts.CriticalSeedStep(nil), seed.Sequence...)
	seed.ProposedActions = append([]string(nil), seed.ProposedActions...)
	seed.ForbiddenActions = append([]string(nil), seed.ForbiddenActions...)
	seed.Context = cloneAnyMap(seed.Context)
	if seed.DeletedAt != nil {
		deletedAt := *seed.DeletedAt
		seed.DeletedAt = &deletedAt
	}
	return seed
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
