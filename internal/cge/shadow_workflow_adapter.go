package cge

import (
	"crypto/sha256"
	"encoding/hex"

	"synora/internal/cge/chains"
	"synora/internal/cge/decisioncomparison"
	"synora/internal/cge/episodes"
	"synora/internal/cge/shadowworkflow"
	"synora/pkg/contract"
)

func (e *ShadowEngine) submitWorkflow(observation chains.ObservationRef, historical *decisioncomparison.HistoricalDecisionRef) (result ShadowAdmissionResult) {
	result = ShadowAdmissionResult{
		Code:                         ShadowAdmissionUnavailable,
		EventType:                    contract.NormalizeEventType(observation.EventType),
		Eligible:                     true,
		Adapted:                      true,
		HistoricalAuthorityUnchanged: true,
		NoActionProduced:             true,
	}
	defer func() {
		if recover() != nil {
			result.Code = ShadowAdmissionUnavailable
			result.Submitted = false
			if e != nil {
				e.safeLog("workflow_submit_panic_recovered")
			}
		}
	}()
	if e == nil {
		return result
	}
	if e.workflow == nil {
		result.Code = ShadowAdmissionDisabled
		return result
	}
	if e.coordinator == nil {
		return result
	}
	workflowStatus := e.workflow.Status()
	switch workflowStatus.State {
	case shadowworkflow.StateStopping:
		result.Code = ShadowAdmissionStopping
		return result
	case shadowworkflow.StateStopped:
		result.Code = ShadowAdmissionStopped
		return result
	case shadowworkflow.StateDisabled:
		result.Code = ShadowAdmissionDisabled
		return result
	}
	status := e.coordinator.Status()
	observed := observation.Timestamp.UTC()
	value := episodes.ObservationRef{EventID: observation.ID, ObservedAt: observed, ReceivedAt: observed, EventType: observation.EventType, NodeID: observation.NodeID, Subject: episodes.SubjectRef{Kind: episodes.SubjectUnknown}, ActivationID: observation.ActivationID, ClipID: observation.ClipID, TrackID: observation.TrackID, SequenceKey: observation.SequenceKey}
	if observation.EntityID != "" {
		value.Subject = episodes.SubjectRef{Kind: episodes.SubjectKnown, EntityID: observation.EntityID}
	}
	if observation.Context != nil {
		value.ZoneID = observation.Context.ZoneID
		value.HouseMode = string(observation.Context.HouseMode)
		value.Occupancy = string(observation.Context.Occupancy)
		value.ContextQuality = string(observation.Context.Quality)
		value.ContextSnapshotFingerprint = observation.Context.SnapshotFingerprint
		value.ContextFreshness = observation.Context.FreshnessCode
	}
	input := shadowworkflow.ShadowWorkflowInput{EventID: observation.ID, ObservedAt: observed, ReceivedAt: observed, Observation: value, SourceShadowRevision: status.JournalSequence, SourceShadowFingerprint: status.JournalHeadHash}
	if historical != nil {
		copy := historical.Clone()
		// The historical decision remains authoritative in Core. The Shadow
		// copy carries only a stable opaque reference, so cognitive projections
		// and calibration records cannot persist a raw event identifier.
		copy.SourceEventRef = redactedHistoricalEventRef(copy.SourceEventRef)
		copy.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(copy)
		input.HistoricalDecision = &copy
	}
	submit := e.workflow.TrySubmit(input)
	result.Code = mapSubmitStatus(submit.Status, workflowStatus.State)
	result.Submitted = result.Code == ShadowAdmissionAccepted
	return result
}

func redactedHistoricalEventRef(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "event-ref:sha256:" + hex.EncodeToString(digest[:])
}
