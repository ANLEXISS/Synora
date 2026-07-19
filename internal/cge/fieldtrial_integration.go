package cge

import (
	"context"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/fieldtrial"
)

func fieldTrialMetadata(config ShadowConfig) fieldtrial.OpenMetadata {
	fingerprint, _ := CognitiveConfigurationFingerprintFor(config)
	return fieldtrial.OpenMetadata{
		CGEConfiguration: fieldtrial.ConfigurationSnapshot{
			ContextEnabled:       config.Context.Enabled,
			RoutineLearning:      config.Routines.Enabled,
			DeviationEnabled:     config.Deviation.Enabled,
			CognitiveShadow:      config.Cognitive.Enabled,
			ContextSchemaVersion: "context-v1",
			RoutinePolicyVersion: "routine-extraction-v1",
			DeviationPolicy:      config.Deviation.Policy.Version,
		},
		PolicyVersions: fieldtrial.PolicyVersions{
			Association: config.AssociationPolicy.Version,
			Evidence:    config.EvidencePolicy.Version,
			Context:     "context-v1",
			Routines:    "routine-extraction-v1",
			Deviation:   config.Deviation.Policy.Version,
		},
		CognitiveConfigurationFingerprint: fingerprint.CombinedFingerprint,
	}
}

// FieldTrialMetadataForConfig exposes the non-secret configuration metadata
// used by the runtime recorder to offline operational tools. It contains no
// paths, keys, payloads, or identities.
func FieldTrialMetadataForConfig(config ShadowConfig) fieldtrial.OpenMetadata {
	return fieldTrialMetadata(config)
}

func (e *ShadowEngine) recordTrialEvent(ctx context.Context, source Event, observation chains.ObservationRef, started time.Time, before MetricsSnapshot) {
	if e == nil || e.trialRecorder == nil || observation.ID == "" {
		return
	}
	defer func() {
		if recover() != nil {
			e.metrics.cognitive("field_trial_record_panics")
		}
	}()
	after := e.metrics.snapshot()
	result := e.LastOrchestrationResult()
	assessment, hasAssessment := e.LastDeviationAssessment()
	status := e.Status()
	input := fieldtrial.EventInput{
		ObservedAt:           observation.Timestamp,
		RecordedAt:           e.shadowNow(),
		EventID:              observation.ID,
		SubjectID:            observation.EntityID,
		ChainID:              string(result.ChainID),
		NodeID:               observation.NodeID,
		ContextQuality:       contextQuality(observation),
		NodeKind:             nodeKind(observation),
		EntryPoint:           contextEntry(observation),
		Exterior:             contextExterior(observation),
		AssociationDecision:  string(result.AssociationDecision),
		HypothesisAction:     string(result.HypothesisAction),
		EvidenceDecision:     string(result.EvidenceDecision),
		EvidenceApplied:      result.EvidenceApplied,
		CoordinatorState:     string(status.State),
		CognitiveWALSequence: status.JournalSequence,
		CognitiveWALHash:     status.JournalHeadHash,
		TotalLatency:         time.Since(started),
		ErrorCodes:           trialErrorCodes(result, source),
		PresencePlanned:      after.RoutinePresenceExtracted > before.RoutinePresenceExtracted,
		TransitionPlanned:    after.RoutineTransitionExtracted > before.RoutineTransitionExtracted,
		PresenceApplied:      after.RoutineCreated > before.RoutineCreated || after.RoutineOccurrenceAdded > before.RoutineOccurrenceAdded,
		TransitionApplied:    after.RoutineOccurrenceAdded > before.RoutineOccurrenceAdded,
		PresenceIdempotent:   after.RoutineOccurrenceIdempotent > before.RoutineOccurrenceIdempotent,
		TransitionIdempotent: after.RoutineOccurrenceIdempotent > before.RoutineOccurrenceIdempotent,
	}
	if observation.EntityID == "" {
		input.SubjectID = observation.SequenceKey
	}
	if hasAssessment {
		input.DeviationAttempted = true
		input.DeviationStatus = string(assessment.Status)
		input.DeviationBand = string(assessment.Band)
		input.DeviationScore = uint16(assessment.Score)
		input.DeviationCoverage = uint16(assessment.Coverage)
		input.DeviationFingerprint = assessment.Fingerprint
		input.BaselineRoutineCount = assessment.BaselineRoutineCount
		input.BestMatchExactRoutine = assessment.BestMatchExactRoutine
		input.BestMatchRevision = assessment.BestMatchRevision
		input.BestMatchOccurrences = assessment.BestMatchOccurrences
		input.BestMatchDistinctDays = assessment.BestMatchDistinctDays
	}
	if input.RecordedAt.IsZero() {
		input.RecordedAt = observation.Timestamp
	}
	beforeStats := e.trialRecorder.Stats()
	_, recordErr := e.trialRecorder.Record(ctx, input)
	afterStats := e.trialRecorder.Stats()
	e.metrics.fieldTrialDelta(beforeStats, afterStats)
	_ = recordErr
}

func trialErrorCodes(result ShadowOrchestrationResult, event Event) []string {
	var codes []string
	if result.ErrorCode != "" {
		codes = append(codes, result.ErrorCode)
	}
	if event.Type == "" {
		codes = append(codes, "event.type_missing")
	}
	return codes
}

func contextQuality(observation chains.ObservationRef) string {
	if observation.Context == nil {
		return "unknown"
	}
	return string(observation.Context.Quality)
}
func nodeKind(observation chains.ObservationRef) string {
	if observation.Context == nil {
		return ""
	}
	return string(observation.Context.NodeKind)
}
func contextEntry(observation chains.ObservationRef) bool {
	return observation.Context != nil && observation.Context.EntryPoint
}
func contextExterior(observation chains.ObservationRef) bool {
	return observation.Context != nil && observation.Context.Exterior
}
