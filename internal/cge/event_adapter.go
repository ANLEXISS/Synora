package cge

import (
	"fmt"
	"strings"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/durableids"
	"synora/pkg/contract"
)

const (
	ReasonEventTypeNotAllowlisted = "event.type_not_allowlisted"
	ReasonEventTypeInvalid        = "event.type_invalid"
	ReasonEventIDMissing          = "event.observation_id_missing"
	ReasonEventTimestampInvalid   = "event.timestamp_invalid"
)

// AdaptationResult is detached and contains only scalar values accepted by
// the CGE association boundary.
type AdaptationResult struct {
	Eligible   bool
	Adapted    bool
	ReasonCode string
	Input      association.Input
}

// AdaptEvent applies the default allowlist.
func AdaptEvent(event Event) (AdaptationResult, error) {
	return AdaptEventWithPolicy(event, DefaultShadowEventAdmissionPolicy())
}

// AdaptEventWithAllowlist performs strict scalar adaptation without accessing
// the original contract event or its payload.
func AdaptEventWithAllowlist(event Event, allowlist []string) (AdaptationResult, error) {
	policy, err := NewShadowEventAdmissionPolicy(allowlist)
	if err != nil {
		return AdaptationResult{}, err
	}
	return AdaptEventWithPolicy(event, policy)
}

// AdaptEventWithPolicy separates policy rejection from malformed admitted
// input. An event outside the policy is a normal historical-only outcome.
func AdaptEventWithPolicy(event Event, policy ShadowEventAdmissionPolicy) (AdaptationResult, error) {
	eventType := contract.NormalizeEventType(event.Type)
	if strings.TrimSpace(event.Type) == "" || eventType == contract.EventSystemUnknown {
		return AdaptationResult{}, adaptationError(ReasonEventTypeInvalid)
	}
	if !policy.allows(eventType) {
		return AdaptationResult{Eligible: false, ReasonCode: ReasonEventTypeNotAllowlisted}, nil
	}
	if strings.TrimSpace(event.ID) == "" {
		return AdaptationResult{}, adaptationError(ReasonEventIDMissing)
	}
	if event.Timestamp.IsZero() {
		return AdaptationResult{}, adaptationError(ReasonEventTimestampInvalid)
	}
	for _, value := range []string{event.ID, event.DeviceID, event.Identity, event.ActivationID, event.ClipID, event.TrackID, event.SequenceKey} {
		if len([]rune(value)) > 256 || strings.ContainsAny(value, "\r\n") {
			return AdaptationResult{}, adaptationError("event.scalar_validation")
		}
	}
	input := association.Input{
		Observation: observationFromEvent(event, eventType), SituationKind: eventType,
	}
	if err := input.Validate(); err != nil {
		return AdaptationResult{}, adaptationError("event.scalar_validation")
	}
	return AdaptationResult{Eligible: true, Adapted: true, Input: input}, nil
}

func observationFromEvent(event Event, eventType string) (observation chains.ObservationRef) {
	return chains.ObservationRef{
		ID: durableids.Protect(durableids.KindObservation, event.ID), EventType: eventType, Timestamp: event.Timestamp,
		NodeID: event.NodeID, DeviceID: durableids.Protect(durableids.KindDevice, event.DeviceID), EntityID: durableids.Protect(durableids.KindEntity, event.Identity),
		ActivationID: durableids.Protect(durableids.KindActivation, event.ActivationID), ClipID: durableids.Protect(durableids.KindClip, event.ClipID), ClipIndex: event.ClipIndex,
		TrackID: durableids.Protect(durableids.KindTrack, event.TrackID), SequenceKey: durableids.Protect(durableids.KindSequence, event.SequenceKey),
	}
}

func adaptationError(code string) error {
	return fmt.Errorf("%w: %s", ErrShadowAdaptation, code)
}
