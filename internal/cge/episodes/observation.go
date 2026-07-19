package episodes

import "time"

// ExistingCGEOutput is a detached, redacted adapter input. Callers can map
// current context, association, routine and deviation outputs into it without
// making the episode package depend on those packages.
type ExistingCGEOutput struct {
	EventID        string
	ObservedAt     time.Time
	ReceivedAt     time.Time
	EventType      string
	Subject        SubjectRef
	NodeID         string
	ZoneID         string
	HouseMode      string
	Occupancy      string
	ContextQuality string
	ActivationID   string
	ClipID         string
	TrackID        string
	SequenceKey    string
	ChainID        string
	RoutineIDs     []string
	Deviation      *DeviationRef
}

func BuildObservationRef(input ExistingCGEOutput) (ObservationRef, error) {
	value := ObservationRef{
		EventID: input.EventID, ObservedAt: input.ObservedAt, ReceivedAt: input.ReceivedAt,
		EventType: input.EventType, Subject: input.Subject, NodeID: input.NodeID, ZoneID: input.ZoneID,
		HouseMode: input.HouseMode, Occupancy: input.Occupancy, ContextQuality: input.ContextQuality,
		ActivationID: input.ActivationID, ClipID: input.ClipID, TrackID: input.TrackID,
		SequenceKey: input.SequenceKey, ChainID: input.ChainID, RoutineIDs: input.RoutineIDs, Deviation: input.Deviation,
	}.Clone()
	if err := value.Validate(); err != nil {
		return ObservationRef{}, err
	}
	return value, nil
}
