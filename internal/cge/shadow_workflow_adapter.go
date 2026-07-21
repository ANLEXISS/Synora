package cge

import (
	"synora/internal/cge/chains"
	"synora/internal/cge/episodes"
	"synora/internal/cge/shadowworkflow"
)

func (e *ShadowEngine) submitWorkflow(observation chains.ObservationRef) {
	defer func() {
		if recover() != nil && e != nil {
			e.safeLog("workflow_submit_panic_recovered")
		}
	}()
	if e == nil || e.workflow == nil || e.coordinator == nil {
		return
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
	}
	input := shadowworkflow.ShadowWorkflowInput{EventID: observation.ID, ObservedAt: observed, ReceivedAt: observed, Observation: value, SourceShadowRevision: status.JournalSequence, SourceShadowFingerprint: status.JournalHeadHash}
	// TrySubmit is intentionally fire-and-forget from the historical path.
	_ = e.workflow.TrySubmit(input)
}
