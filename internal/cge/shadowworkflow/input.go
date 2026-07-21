package shadowworkflow

import (
	"strings"
	"time"

	"synora/internal/cge/decisioncomparison"
	"synora/internal/cge/episodes"
)

type ShadowWorkflowInput struct {
	EventID                 string
	ObservedAt              time.Time
	ReceivedAt              time.Time
	Observation             episodes.ObservationRef
	SourceShadowRevision    uint64
	SourceShadowFingerprint string
	HistoricalDecision      *decisioncomparison.HistoricalDecisionRef
}

func (i ShadowWorkflowInput) Validate() error {
	if strings.TrimSpace(i.EventID) == "" || len([]rune(i.EventID)) > 256 || i.ObservedAt.IsZero() || i.ReceivedAt.IsZero() || i.ObservedAt.Location() != time.UTC || i.ReceivedAt.Location() != time.UTC || i.Observation.EventID != i.EventID {
		return ErrInputRejected
	}
	if err := i.Observation.Validate(); err != nil {
		return ErrInputRejected
	}
	if len([]rune(i.SourceShadowFingerprint)) > 256 || strings.ContainsAny(i.SourceShadowFingerprint, "\r\n") {
		return ErrInputRejected
	}
	if i.HistoricalDecision != nil {
		if err := i.HistoricalDecision.Validate(decisioncomparison.DefaultPolicy()); err != nil {
			return ErrInputRejected
		}
	}
	return nil
}
