package association

import (
	"fmt"
	"strings"
	"time"

	"synora/internal/cge/chains"
)

// Policy controls all association thresholds and integer score weights.
// Values are intentionally explicit and versioned; they are provisional
// defaults for this pass, not learned confidence parameters.
type Policy struct {
	Version string

	MaxForwardGap  time.Duration
	MaxLateArrival time.Duration

	MinimumAttachScore int64
	MinimumScoreMargin int64

	SameEntityScore         int64
	SameSequenceScore       int64
	SameActivationScore     int64
	SameTrackScore          int64
	SameDeviceScore         int64
	SameLastNodeScore       int64
	NodeAlreadySeenScore    int64
	SameSituationScore      int64
	TemporalContinuityScore int64

	MaxRankedCandidates int
}

// IsAssociationEligible centralizes the lifecycle states examined by the
// planner. Historical and terminal states are never reactivated here.
func IsAssociationEligible(status chains.Status) bool { return status.CanAcceptObservation() }

// DefaultPolicy returns the documented deterministic policy for pass 15.
func DefaultPolicy() Policy {
	return Policy{
		Version:       "association-v1",
		MaxForwardGap: 5 * time.Minute, MaxLateArrival: 2 * time.Minute,
		MinimumAttachScore: 75, MinimumScoreMargin: 20,
		SameEntityScore: 55, SameSequenceScore: 35, SameActivationScore: 30,
		SameTrackScore: 30, SameDeviceScore: 20, SameLastNodeScore: 15,
		NodeAlreadySeenScore: 10, SameSituationScore: 15,
		TemporalContinuityScore: 30, MaxRankedCandidates: 16,
	}
}

// Validate checks both individual fields and the cross-field constraints that
// keep one weak criterion from independently crossing the attach threshold.
func (p Policy) Validate() error {
	if strings.TrimSpace(p.Version) == "" || strings.ContainsAny(p.Version, "\r\n") || len([]rune(p.Version)) > 64 {
		return fmt.Errorf("%w: version is invalid", ErrInvalidPolicy)
	}
	if p.MaxForwardGap <= 0 || p.MaxLateArrival <= 0 {
		return fmt.Errorf("%w: temporal windows must be positive", ErrInvalidPolicy)
	}
	if p.MinimumAttachScore <= 0 || p.MinimumScoreMargin < 0 || p.MaxRankedCandidates <= 0 {
		return fmt.Errorf("%w: thresholds and candidate limit are invalid", ErrInvalidPolicy)
	}
	weights := []struct {
		name  string
		value int64
	}{
		{"same entity", p.SameEntityScore}, {"same sequence", p.SameSequenceScore},
		{"same activation", p.SameActivationScore}, {"same track", p.SameTrackScore},
		{"same device", p.SameDeviceScore}, {"same last node", p.SameLastNodeScore},
		{"node already seen", p.NodeAlreadySeenScore}, {"same situation", p.SameSituationScore},
		{"temporal continuity", p.TemporalContinuityScore},
	}
	var maximum int64
	for _, weight := range weights {
		if weight.value < 0 {
			return fmt.Errorf("%w: %s score is negative", ErrInvalidPolicy, weight.name)
		}
		if weight.value >= p.MinimumAttachScore {
			return fmt.Errorf("%w: %s score alone reaches attach threshold", ErrInvalidPolicy, weight.name)
		}
		maximum += weight.value
	}
	if maximum < p.MinimumAttachScore {
		return fmt.Errorf("%w: maximum score cannot reach attach threshold", ErrInvalidPolicy)
	}
	if p.MinimumScoreMargin > maximum {
		return fmt.Errorf("%w: score margin exceeds maximum score", ErrInvalidPolicy)
	}
	return nil
}
