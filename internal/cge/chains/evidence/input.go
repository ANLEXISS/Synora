package evidence

import (
	"strings"

	"synora/internal/cge/chains"
)

// Input selects exactly one observation already present in one defensive
// chain snapshot. No external observation or registry lookup is accepted.
type Input struct {
	Chain               chains.Snapshot
	TargetObservationID string
}

func (in Input) Validate() error {
	if strings.TrimSpace(in.TargetObservationID) == "" || strings.ContainsAny(in.TargetObservationID, "\r\n") {
		return invalidInput(string(in.Chain.ID), in.TargetObservationID, "target_observation_id", nil)
	}
	restored, err := chains.Restore(in.Chain)
	if err != nil {
		return invalidInput(string(in.Chain.ID), in.TargetObservationID, "snapshot", err)
	}
	if !IsEvidenceEvaluationAllowed(in.Chain.Status) {
		return evidenceError(ErrEvidenceEvaluationNotAllowed, "input", string(in.Chain.ID), in.TargetObservationID, string(in.Chain.Status), nil)
	}
	if _, ok := findObservation(restored.Snapshot(), in.TargetObservationID); !ok {
		return evidenceError(ErrTargetObservationNotFound, "input", string(in.Chain.ID), in.TargetObservationID, "target", nil)
	}
	return nil
}

func findObservation(snapshot chains.Snapshot, id string) (chains.ObservationRef, bool) {
	for _, observation := range snapshot.Observations {
		if observation.ID == id {
			return observation, true
		}
	}
	return chains.ObservationRef{}, false
}
