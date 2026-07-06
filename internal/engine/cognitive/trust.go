package cognitive

import (
	"math"
	"time"

	"synora/internal/engine/contracts"
)

type TrustResult struct {
	ExperienceFactor  float64
	ReliabilityFactor float64
	FreshnessFactor   float64
	ConfidenceFactor  float64

	TrustScore float64
}

func ComputeGraphTrust(
	outcome *contracts.Outcome,
) TrustResult {

	if outcome == nil {

		return TrustResult{
			ExperienceFactor:  0.0,
			ReliabilityFactor: 0.0,
			FreshnessFactor:   0.0,
			ConfidenceFactor:  0.0,

			TrustScore: 0.0,
		}
	}

	experience :=
		computeExperienceFactor(
			outcome.SuccessCount,
			outcome.FailureCount,
		)

	reliability :=
		computeReliabilityFactor(
			outcome.SuccessCount,
			outcome.FailureCount,
		)

	freshness :=
		computeFreshnessFactor(
			outcome.LastValidated,
		)

	confidence :=
		clamp(
			outcome.Confidence,
			0.0,
			1.0,
		)

	// ---------------------------------------------------------------------
	// GLOBAL TRUST
	// ---------------------------------------------------------------------

	trust :=
		experience*0.25 +
			reliability*0.40 +
			freshness*0.15 +
			confidence*0.20

	trust =
		clamp(
			trust,
			0.0,
			1.0,
		)

	return TrustResult{
		ExperienceFactor:  experience,
		ReliabilityFactor: reliability,
		FreshnessFactor:   freshness,
		ConfidenceFactor:  confidence,

		TrustScore: trust,
	}
}

func computeExperienceFactor(
	success uint64,
	failure uint64,
) float64 {

	total :=
		float64(
			success +
				failure,
		)

	if total == 0 {
		return 0
	}

	// ---------------------------------------------------------------------
	// Sigmoid-like saturation
	//
	// 1 obs  -> ~0.05
	// 10 obs -> ~0.39
	// 25 obs -> ~0.71
	// 50 obs -> ~0.91
	// 100+   -> ~0.99
	// ---------------------------------------------------------------------

	score :=
		1.0 -
			math.Exp(
				-total/20.0,
			)

	return clamp(
		score,
		0.0,
		1.0,
	)
}

func computeReliabilityFactor(
	success uint64,
	failure uint64,
) float64 {

	total :=
		success + failure

	if total == 0 {
		return 0
	}

	// ---------------------------------------------------------------------
	// Bayesian smoothing
	//
	// prevents:
	// 1 success -> 100%
	// 2 success -> 100%
	// ---------------------------------------------------------------------

	return float64(success+1) /
		float64(total+2)
}

func computeFreshnessFactor(
	lastValidated time.Time,
) float64 {

	if lastValidated.IsZero() {
		return 0
	}

	now := time.Now()

	if lastValidated.After(now) {
		return 1.0
	}

	days :=
		now.Sub(
			lastValidated,
		).Hours() / 24.0

	// ---------------------------------------------------------------------
	// Decay over ~180 days
	// ---------------------------------------------------------------------

	score :=
		math.Exp(
			-days / 180.0,
		)

	return clamp(
		score,
		0.0,
		1.0,
	)
}