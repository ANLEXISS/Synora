package cge

import (
	"context"
	"errors"
	"strings"
)

var ErrAmbiguousTarget = errors.New("ambiguous_target")

type CognitiveDecisionTargetResolver interface {
	ResolveTarget(context.Context, CognitiveSituationSnapshot, CognitiveChainCandidate) (DecisionTarget, error)
}

type DefaultCognitiveDecisionTargetResolver struct{}

func (DefaultCognitiveDecisionTargetResolver) ResolveTarget(ctx context.Context, situation CognitiveSituationSnapshot, chain CognitiveChainCandidate) (DecisionTarget, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return DecisionTarget{}, ctx.Err()
		default:
		}
	}
	if err := chain.Validate(); err != nil {
		return DecisionTarget{}, err
	}
	if chain.ExpectedState == "intrusion" || chain.ExpectedState == "break_in" {
		return DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, nil
	}
	if err := situation.Validate(); err != nil {
		return DecisionTarget{}, err
	}
	observation := CognitiveObservationSnapshot{}
	for _, value := range situation.Observations {
		if value.ID == situation.CurrentObservationID {
			observation = value
			break
		}
	}
	if observation.ID == "" && len(situation.Observations) > 0 {
		observation = situation.Observations[len(situation.Observations)-1]
	}
	for _, action := range chain.ProposedActions {
		action = strings.ToLower(strings.TrimSpace(action))
		if strings.Contains(action, "change_mode") || strings.Contains(action, "notify") || chain.ExpectedState == "intrusion" || chain.ExpectedState == "break_in" {
			// Any audience encoded in a notify action remains descriptive chain
			// evidence. The decision target is always the system.
			return DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, nil
		}
		if strings.Contains(action, "record_clip") || strings.Contains(action, "camera") {
			if observation.ClipID != "" {
				return DecisionTarget{Kind: DecisionTargetDevice, ID: observation.ClipID}, nil
			}
			if observation.DeviceID != "" {
				return DecisionTarget{Kind: DecisionTargetDevice, ID: observation.DeviceID}, nil
			}
			return DecisionTarget{}, ErrAmbiguousTarget
		}
		if strings.Contains(action, "light") || strings.Contains(action, "zone") {
			if observation.ZoneID != "" {
				return DecisionTarget{Kind: DecisionTargetZone, ID: observation.ZoneID}, nil
			}
			if observation.NodeID != "" {
				return DecisionTarget{Kind: DecisionTargetNode, ID: observation.NodeID}, nil
			}
			return DecisionTarget{}, ErrAmbiguousTarget
		}
		if strings.Contains(action, "resident") || strings.Contains(action, "person") {
			if observation.EntityID != "" {
				return DecisionTarget{Kind: DecisionTargetResident, ID: observation.EntityID}, nil
			}
			return DecisionTarget{}, ErrAmbiguousTarget
		}
		if strings.Contains(action, "device") || strings.Contains(action, "lock") || strings.Contains(action, "unlock") || strings.Contains(action, "turn_on") || strings.Contains(action, "turn_off") || strings.Contains(action, "set_") {
			if observation.DeviceID != "" {
				return DecisionTarget{Kind: DecisionTargetDevice, ID: observation.DeviceID}, nil
			}
			return DecisionTarget{}, ErrAmbiguousTarget
		}
	}
	return DecisionTarget{}, ErrAmbiguousTarget
}
