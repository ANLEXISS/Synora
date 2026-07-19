package association

import "synora/internal/cge/chains"

const (
	maxSituationKindLength = 128
	maxAssociationText     = 256
)

// Input is the detached association input. Identity, device, activation,
// track, sequence, and node values are read from ObservationRef rather than
// duplicated here. SituationKind is optional context used by scoring.
type Input struct {
	Observation   chains.ObservationRef
	SituationKind string
}

// Validate rejects raw or unbounded association data without changing the
// domain ObservationRef format.
func (i Input) Validate() error {
	if err := i.Observation.Validate(); err != nil {
		return wrapInput(err)
	}
	if err := validBoundedText(i.SituationKind, "situation kind", maxSituationKindLength, false); err != nil {
		return wrapInput(err)
	}
	fields := []struct {
		name  string
		value string
	}{
		{"observation id", i.Observation.ID}, {"event type", i.Observation.EventType},
		{"node id", i.Observation.NodeID}, {"device id", i.Observation.DeviceID},
		{"entity id", i.Observation.EntityID}, {"activation id", i.Observation.ActivationID},
		{"clip id", i.Observation.ClipID}, {"track id", i.Observation.TrackID},
		{"sequence key", i.Observation.SequenceKey},
	}
	for _, field := range fields {
		name, value := field.name, field.value
		if err := validBoundedText(value, name, maxAssociationText, false); err != nil {
			return wrapInput(err)
		}
	}
	return nil
}

func wrapInput(err error) error { return joinError(ErrInvalidInput, err) }
