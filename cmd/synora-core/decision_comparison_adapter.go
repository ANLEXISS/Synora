package main

import (
	"math"

	"synora/internal/cge/decisioncomparison"
	"synora/internal/engine"
	"synora/internal/state"
	"synora/pkg/contract"
)

// buildHistoricalDecisionRef copies only scalar, non-media decision facts to
// the optional Shadow boundary. Fields not represented by the historical
// engine remain at their explicit zero/unknown value.
func buildHistoricalDecisionRef(event *contract.Event, result *engine.Result, previous, current state.SystemState, stateChanged bool) (*decisioncomparison.HistoricalDecisionRef, error) {
	if event == nil || result == nil || result.Decision == nil {
		return nil, nil
	}
	decision := result.Decision
	currentState := current.LastState
	if currentState == "" {
		currentState = decision.State
	}
	decisionID := decision.ID
	if decisionID == "" {
		decisionID = event.ID
	}
	score := decision.EffectiveScore
	if score == 0 {
		score = decision.Score
	}
	ref := &decisioncomparison.HistoricalDecisionRef{
		ID: decisionID, SourceEventRef: event.ID,
		PreviousStateCode: previous.LastState, CurrentStateCode: currentState,
		StateChanged:                             stateChanged || previous.LastState != "" && previous.LastState != currentState,
		DecisionScorePermille:                    clampDecisionPermille(score),
		DecidedAtUnixNano:                        decision.Timestamp.UnixNano(),
		HistoricalDecisionHasProductionAuthority: true,
	}
	ref.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(*ref)
	if err := ref.Validate(decisioncomparison.DefaultPolicy()); err != nil {
		return nil, err
	}
	return ref, nil
}

func clampDecisionPermille(value float64) int {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1000
	}
	return int(math.Round(value * 1000))
}
