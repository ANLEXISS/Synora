package graph

import (
	"sort"
	"time"

	"synora/internal/engine/contracts"
)

type compactSequence struct {
	ID                string    `json:"id"`
	Signature         string    `json:"signature"`
	EventTypes        []string  `json:"event_types"`
	Count             int       `json:"count"`
	Origin            string    `json:"origin,omitempty"`
	CriticalSeedID    string    `json:"critical_seed_id,omitempty"`
	DangerScore       float64   `json:"danger_score,omitempty"`
	RiskLevel         string    `json:"risk_level,omitempty"`
	ExpectedState     string    `json:"expected_state,omitempty"`
	SimulatedCount    int       `json:"simulated_count"`
	RealCount         int       `json:"real_count"`
	CriticalSeedCount int       `json:"critical_seed_count,omitempty"`
	EffectiveCount    int       `json:"effective_count,omitempty"`
	Confidence        float64   `json:"confidence"`
	FirstSeen         time.Time `json:"first_seen"`
	LastSeen          time.Time `json:"last_seen"`
	AvgDeltaMs        int64     `json:"avg_delta_ms"`
	LastTestRunID     string    `json:"last_test_run_id,omitempty"`
	EvidenceCount     int       `json:"evidence_count"`
	ExampleCount      int       `json:"example_count"`
}

type compactTransition struct {
	ID             string    `json:"id"`
	FromEventType  string    `json:"from_event_type"`
	ToEventType    string    `json:"to_event_type"`
	Count          int       `json:"count"`
	SimulatedCount int       `json:"simulated_count"`
	RealCount      int       `json:"real_count"`
	Confidence     float64   `json:"confidence"`
	AvgDeltaMs     int64     `json:"avg_delta_ms"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
}

type compactBehavior struct {
	ID                       string           `json:"id"`
	TriggerSequenceSignature string           `json:"trigger_sequence_signature"`
	Status                   string           `json:"status"`
	RequiresValidation       bool             `json:"requires_validation"`
	Count                    int              `json:"count"`
	Confidence               float64          `json:"confidence"`
	Origin                   string           `json:"origin,omitempty"`
	CriticalSeedID           string           `json:"critical_seed_id,omitempty"`
	DangerScore              float64          `json:"danger_score,omitempty"`
	RiskLevel                string           `json:"risk_level,omitempty"`
	ExpectedState            string           `json:"expected_state,omitempty"`
	SimulatedCount           int              `json:"simulated_count"`
	RealCount                int              `json:"real_count"`
	CriticalSeedCount        int              `json:"critical_seed_count,omitempty"`
	EffectiveCount           int              `json:"effective_count,omitempty"`
	ProposedActions          []map[string]any `json:"proposed_actions"`
	ForbiddenActionsCount    int              `json:"forbidden_actions_count"`
	EvidenceCount            int              `json:"evidence_count"`
	LastMatchedAt            time.Time        `json:"last_matched_at,omitempty"`
	LastTriggeredAt          *time.Time       `json:"last_triggered_at,omitempty"`
}

func (g *GraphMemory) CompactInspection() map[string]any {
	g.mu.RLock()
	defer g.mu.RUnlock()

	sequences := sortedSequences(g.learnedSequences)
	transitions := sortedTransitions(g.learnedTransitions)
	behaviors := sortedBehaviors(g.learnedBehaviors)

	return map[string]any{
		"stats":             cgeStats(sequences, transitions, behaviors),
		"sequences":         compactSequences(limitSequences(sequences, defaultCGEPublicSequencesLimit)),
		"transitions":       compactTransitions(limitTransitions(transitions, defaultCGEPublicTransitionsLimit)),
		"learned_behaviors": compactBehaviors(limitBehaviors(behaviors, defaultCGEPublicBehaviorsLimit)),
	}
}

func (g *GraphMemory) Summary() map[string]any {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return cgeStats(
		sortedSequences(g.learnedSequences),
		sortedTransitions(g.learnedTransitions),
		sortedBehaviors(g.learnedBehaviors),
	)
}

func sortedSequences(items map[string]*contracts.LearnedSequence) []contracts.LearnedSequence {
	out := make([]contracts.LearnedSequence, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Signature < out[j].Signature
	})
	return out
}

func sortedTransitions(items map[string]*contracts.LearnedTransition) []contracts.LearnedTransition {
	out := make([]contracts.LearnedTransition, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedBehaviors(items map[string]*contracts.LearnedBehavior) []contracts.LearnedBehavior {
	out := make([]contracts.LearnedBehavior, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, *item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if !out[i].LastMatchedAt.Equal(out[j].LastMatchedAt) {
			return out[i].LastMatchedAt.After(out[j].LastMatchedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func compactSequences(items []contracts.LearnedSequence) []compactSequence {
	out := make([]compactSequence, 0, len(items))
	for _, item := range items {
		out = append(out, compactSequence{
			ID:                item.ID,
			Signature:         item.Signature,
			EventTypes:        append([]string(nil), item.EventTypes...),
			Count:             item.Count,
			Origin:            item.Origin,
			CriticalSeedID:    item.CriticalSeedID,
			DangerScore:       item.DangerScore,
			RiskLevel:         item.RiskLevel,
			ExpectedState:     item.ExpectedState,
			SimulatedCount:    item.SimulatedCount,
			RealCount:         item.RealCount,
			CriticalSeedCount: item.CriticalSeedCount,
			EffectiveCount:    item.EffectiveCount,
			Confidence:        item.Confidence,
			FirstSeen:         item.FirstSeen,
			LastSeen:          item.LastSeen,
			AvgDeltaMs:        item.AvgDeltaMs,
			LastTestRunID:     item.LastTestRunID,
			EvidenceCount:     len(item.Evidence),
			ExampleCount:      len(item.Examples),
		})
	}
	return out
}

func compactTransitions(items []contracts.LearnedTransition) []compactTransition {
	out := make([]compactTransition, 0, len(items))
	for _, item := range items {
		out = append(out, compactTransition{
			ID:             item.ID,
			FromEventType:  item.FromEventType,
			ToEventType:    item.ToEventType,
			Count:          item.Count,
			SimulatedCount: item.SimulatedCount,
			RealCount:      item.RealCount,
			Confidence:     item.Confidence,
			AvgDeltaMs:     item.AvgDeltaMs,
			FirstSeen:      item.FirstSeen,
			LastSeen:       item.LastSeen,
		})
	}
	return out
}

func compactBehaviors(items []contracts.LearnedBehavior) []compactBehavior {
	out := make([]compactBehavior, 0, len(items))
	for _, item := range items {
		out = append(out, compactBehavior{
			ID:                       item.ID,
			TriggerSequenceSignature: item.TriggerSequenceSignature,
			Status:                   item.Status,
			RequiresValidation:       item.RequiresValidation,
			Count:                    item.Count,
			Confidence:               item.Confidence,
			Origin:                   item.Origin,
			CriticalSeedID:           item.CriticalSeedID,
			DangerScore:              item.DangerScore,
			RiskLevel:                item.RiskLevel,
			ExpectedState:            item.ExpectedState,
			SimulatedCount:           item.SimulatedCount,
			RealCount:                item.RealCount,
			CriticalSeedCount:        item.CriticalSeedCount,
			EffectiveCount:           item.EffectiveCount,
			ProposedActions:          compactActions(item.ProposedActions),
			ForbiddenActionsCount:    len(item.ForbiddenActions),
			EvidenceCount:            len(item.Evidence),
			LastMatchedAt:            item.LastMatchedAt,
			LastTriggeredAt:          item.LastTriggeredAt,
		})
	}
	return out
}

func compactActions(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	allowed := map[string]bool{
		"id": true, "type": true, "action": true, "device_id": true,
		"command": true, "status": true,
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		compact := map[string]any{}
		for key, value := range item {
			if allowed[key] {
				compact[key] = value
			}
		}
		out = append(out, compact)
	}
	return out
}

func cgeStats(sequences []contracts.LearnedSequence, transitions []contracts.LearnedTransition, behaviors []contracts.LearnedBehavior) map[string]any {
	realSequences := 0
	simulatedSequences := 0
	criticalSeeds := 0
	lastUpdated := time.Time{}
	for _, item := range sequences {
		if item.RealCount > 0 {
			realSequences++
		}
		if item.SimulatedCount > 0 {
			simulatedSequences++
		}
		if item.CriticalSeedCount > 0 {
			criticalSeeds += item.CriticalSeedCount
		}
		if item.LastSeen.After(lastUpdated) {
			lastUpdated = item.LastSeen
		}
	}
	for _, item := range transitions {
		if item.LastSeen.After(lastUpdated) {
			lastUpdated = item.LastSeen
		}
	}
	for _, item := range behaviors {
		if item.LastMatchedAt.After(lastUpdated) {
			lastUpdated = item.LastMatchedAt
		}
		if item.LastTriggeredAt != nil && item.LastTriggeredAt.After(lastUpdated) {
			lastUpdated = *item.LastTriggeredAt
		}
	}
	var last any
	if !lastUpdated.IsZero() {
		last = lastUpdated
	}
	return map[string]any{
		"sequence_count":           len(sequences),
		"transition_count":         len(transitions),
		"learned_behavior_count":   len(behaviors),
		"real_sequence_count":      realSequences,
		"simulated_sequence_count": simulatedSequences,
		"critical_seed_count":      criticalSeeds,
		"last_updated_at":          last,
	}
}

func limitSequences(items []contracts.LearnedSequence, limit int) []contracts.LearnedSequence {
	if limit < 0 || limit >= len(items) {
		return items
	}
	return items[:limit]
}

func limitTransitions(items []contracts.LearnedTransition, limit int) []contracts.LearnedTransition {
	if limit < 0 || limit >= len(items) {
		return items
	}
	return items[:limit]
}

func limitBehaviors(items []contracts.LearnedBehavior, limit int) []contracts.LearnedBehavior {
	if limit < 0 || limit >= len(items) {
		return items
	}
	return items[:limit]
}
