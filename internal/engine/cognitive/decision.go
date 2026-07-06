package cognitive

import (
	"math"
	"time"

	"synora/internal/engine/contracts"
	"synora/internal/engine/guidelines"
)

func ComputeDecision(
	divergence SimilarityResult,
	outcome *contracts.Outcome,
	event *contracts.Event,
) contracts.DecisionResult {

	var outcomeValue float64

	var graphTrust float64

	if outcome != nil {

		outcomeValue =
			clamp(
				outcome.Value,
				0.0,
				1.0,
			)

		trust :=
			ComputeGraphTrust(
				outcome,
			)

		graphTrust =
			trust.TrustScore
	}

	guidelineValue :=
		guidelines.Score(
			event.Type,
		)

	guidelineTrust :=
		1.0 - graphTrust

	// ---------------------------------------------------------------------
	// SAFETY FLOOR
	// ---------------------------------------------------------------------

	if guidelineValue >= 0.80 {

		graphTrust =
			math.Min(
				graphTrust,
				0.50,
			)

		guidelineTrust =
			1.0 - graphTrust
	}

	// ---------------------------------------------------------------------
	// BASE DECISION
	//
	// Option B:
	// 0.0 = safe
	// 1.0 = dangerous
	// ---------------------------------------------------------------------

	decision :=
		outcomeValue*graphTrust +
			guidelineValue*guidelineTrust

	// ---------------------------------------------------------------------
	// DIVERGENCE PENALTY
	//
	// unexpected behaviour should increase risk
	// ---------------------------------------------------------------------

	decision +=
		divergence.Divergence * 0.25

	// ---------------------------------------------------------------------
	// STRONG DIVERGENCE BONUS
	// ---------------------------------------------------------------------

	if divergence.Divergence > 0.70 {

		decision += 0.10
	}

	if divergence.Divergence > 0.90 {

		decision += 0.10
	}

	decision =
		clamp(
			decision,
			0.0,
			1.0,
		)

	return contracts.DecisionResult{
		DivergenceScore:
			divergence.Divergence,

		DecisionScore:
			decision,

		GraphTrust:
			graphTrust,

		GuidelineTrust:
			guidelineTrust,

		OutcomeValue:
			outcomeValue,

		GuidelineValue:
			guidelineValue,

		Outcome:
			outcome,

		Level:
			SeverityFromDecision(
				decision,
			),

		Reasons:
			buildReasons(
				divergence,
				decision,
			),

		Timestamp:
			time.Now(),
	}
}

func SeverityFromDecision(
	score float64,
) contracts.Severity {

	switch {

	case score < 0.20:

		return contracts.SeverityInfo

	case score < 0.40:

		return contracts.SeverityLow

	case score < 0.60:

		return contracts.SeverityMedium

	case score < 0.80:

		return contracts.SeverityHigh

	default:

		return contracts.SeverityCritical
	}
}

func buildReasons(
	divergence SimilarityResult,
	score float64,
) []string {

	reasons :=
		make([]string, 0)

	if divergence.Divergence > 0.50 {

		reasons = append(
			reasons,
			"high_divergence",
		)
	}

	if divergence.EventSimilarity < 0.50 {

		reasons = append(
			reasons,
			"unexpected_event",
		)
	}

	if divergence.TopologySimilarity < 0.50 {

		reasons = append(
			reasons,
			"unexpected_location",
		)
	}

	if divergence.TimeSimilarity < 0.30 {

		reasons = append(
			reasons,
			"unusual_time",
		)
	}

	if score > 0.80 {

		reasons = append(
			reasons,
			"critical_decision",
		)
	}

	return reasons
}