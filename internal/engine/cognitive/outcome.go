package cognitive

import (
	"time"

	"synora/internal/engine/contracts"
)

func InferOutcome(
	seq *contracts.ActiveSequence,
) *contracts.Outcome {

	if seq == nil {

		return defaultSafeOutcome()
	}

	level :=
		detectHighestSeverity(
			seq,
		)

	switch level {

	case contracts.OutcomeEmergency:

		return &contracts.Outcome{
			Type: level,

			Value: 1.0,

			Confidence: 0.95,

			SuccessCount: 1,

			FailureCount: 0,

			LastValidated: time.Now(),
		}

	case contracts.OutcomeDanger:

		return &contracts.Outcome{
			Type: level,

			Value: 0.85,

			Confidence: 0.90,

			SuccessCount: 1,

			FailureCount: 0,

			LastValidated: time.Now(),
		}

	case contracts.OutcomeWarning:

		return &contracts.Outcome{
			Type: level,

			Value: 0.50,

			Confidence: 0.80,

			SuccessCount: 1,

			FailureCount: 0,

			LastValidated: time.Now(),
		}

	default:

		return defaultSafeOutcome()
	}
}

func detectHighestSeverity(
	seq *contracts.ActiveSequence,
) contracts.OutcomeType {

	level :=
		contracts.OutcomeSafe

	for _, event := range seq.Events {

		switch event.Type {

		// -------------------------------------------------------------
		// EMERGENCY
		// -------------------------------------------------------------

		case "vision.weapon.firearm":

			return contracts.OutcomeEmergency

		// -------------------------------------------------------------
		// DANGER
		// -------------------------------------------------------------

		case "vision.weapon.knife":

			if level != contracts.OutcomeEmergency {

				level =
					contracts.OutcomeDanger
			}

		case "vision.pose.fallen":

			if level != contracts.OutcomeEmergency {

				level =
					contracts.OutcomeDanger
			}

		case "vision.camera.tampered":

			if level != contracts.OutcomeEmergency {

				level =
					contracts.OutcomeDanger
			}

		case "vision.camera.occluded":

			if level != contracts.OutcomeEmergency {

				level =
					contracts.OutcomeDanger
			}

		// -------------------------------------------------------------
		// WARNING
		// -------------------------------------------------------------

		case "vision.id.uncertain":

			if level == contracts.OutcomeSafe {

				level =
					contracts.OutcomeWarning
			}

		case "vision.track.lost":

			if level == contracts.OutcomeSafe {

				level =
					contracts.OutcomeWarning
			}
		}
	}

	return level
}

func defaultSafeOutcome() *contracts.Outcome {

	return &contracts.Outcome{
		Type: contracts.OutcomeSafe,

		Value: 0.05,

		Confidence: 0.80,

		SuccessCount: 1,

		FailureCount: 0,

		LastValidated: time.Now(),
	}
}