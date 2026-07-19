package deviation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/routines"
)

// Policy is the versioned, deterministic policy for descriptive deviation.
// Temporal buckets use the v1 routine convention of 15 minutes unless a
// different explicitly configured bucket size is supplied.
type Policy struct {
	Namespace string
	Version   string

	MinOccurrences       uint64
	MinDistinctLocalDays uint64
	MinSpan              time.Duration

	MaxCandidateRoutines int

	TemporalBucketMinutes    int
	TemporalToleranceBuckets int

	StructuralWeight Score
	TemporalWeight   Score
	IntervalWeight   Score

	AmbiguityMargin Score

	AlignedMax  Score
	LowMax      Score
	ModerateMax Score

	AllowPartialContext bool

	EligibleStatuses []routines.Status
}

func DefaultPolicy() Policy {
	return Policy{
		Namespace:                "synora.cge.deviation",
		Version:                  "deviation-v1",
		MinOccurrences:           3,
		MinDistinctLocalDays:     2,
		MinSpan:                  6 * time.Hour,
		MaxCandidateRoutines:     64,
		TemporalBucketMinutes:    15,
		TemporalToleranceBuckets: 16,
		StructuralWeight:         600,
		TemporalWeight:           300,
		IntervalWeight:           100,
		AmbiguityMargin:          50,
		AlignedMax:               200,
		LowMax:                   400,
		ModerateMax:              700,
		AllowPartialContext:      true,
		EligibleStatuses:         []routines.Status{routines.StatusCandidate, routines.StatusActive, routines.StatusDeclining, routines.StatusDormant},
	}
}

// DefaultDeviationPolicy is an explicit alias for callers that prefer the
// domain-qualified constructor name.
func DefaultDeviationPolicy() Policy { return DefaultPolicy() }

func (p Policy) Validate() error {
	if !validText(p.Namespace, 128) || !validText(p.Version, 128) || p.MinOccurrences == 0 || p.MinDistinctLocalDays == 0 || p.MinSpan <= 0 || p.MaxCandidateRoutines <= 0 || p.MaxCandidateRoutines > 4096 || p.TemporalBucketMinutes < 5 || p.TemporalBucketMinutes > 120 || 1440%p.TemporalBucketMinutes != 0 || p.TemporalToleranceBuckets <= 0 || p.TemporalToleranceBuckets > 7*(1440/5) || p.StructuralWeight == 0 || p.TemporalWeight == 0 || p.IntervalWeight == 0 || p.StructuralWeight+p.TemporalWeight+p.IntervalWeight != MaxScore || p.AlignedMax > p.LowMax || p.LowMax > p.ModerateMax || p.ModerateMax > MaxScore || p.AmbiguityMargin > MaxScore {
		return ErrInvalidDeviationPolicy
	}
	seen := map[routines.Status]struct{}{}
	for _, status := range p.EligibleStatuses {
		if !validRoutineStatus(status) {
			return fmt.Errorf("%w: status", ErrInvalidDeviationPolicy)
		}
		if _, exists := seen[status]; exists {
			return fmt.Errorf("%w: duplicate status", ErrInvalidDeviationPolicy)
		}
		seen[status] = struct{}{}
	}
	if len(seen) == 0 {
		return ErrInvalidDeviationPolicy
	}
	return nil
}

// Fingerprint returns the canonical identity of all policy parameters. The
// eligible status list is sorted because its order has no semantic meaning.
func (p Policy) Fingerprint() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	copy := p
	copy.EligibleStatuses = sortedStatuses(p.EligibleStatuses)
	payload, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validText(value string, max int) bool {
	return value != "" && len([]rune(value)) <= max && value == trimText(value)
}

func trimText(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\n' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\n' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}

func validRoutineStatus(status routines.Status) bool {
	switch status {
	case routines.StatusCandidate, routines.StatusActive, routines.StatusDeclining, routines.StatusDormant, routines.StatusArchived, routines.StatusInvalidated:
		return true
	default:
		return false
	}
}

// RoutineReadiness describes whether a routine has enough descriptive history
// to be used as a baseline. It never changes the routine status.
type RoutineReadiness struct {
	Eligible bool

	OccurrenceCount uint64
	DistinctDays    uint64
	Span            time.Duration

	OccurrenceRequirementMet bool
	DaysRequirementMet       bool
	SpanRequirementMet       bool
	StatusEligible           bool

	ReasonCode string
}

func EvaluateRoutineReadiness(routine routines.Snapshot, policy Policy) (RoutineReadiness, error) {
	if err := policy.Validate(); err != nil {
		return RoutineReadiness{}, err
	}
	if _, err := routine.Fingerprint(); err != nil {
		return RoutineReadiness{}, fmt.Errorf("%w: %v", ErrInvalidDeviationCandidate, err)
	}
	result := RoutineReadiness{
		OccurrenceCount:          routine.OccurrenceCount,
		DistinctDays:             routine.DistinctLocalDays,
		Span:                     routine.LastSeenAt.Sub(routine.FirstSeenAt),
		OccurrenceRequirementMet: routine.OccurrenceCount >= policy.MinOccurrences,
		DaysRequirementMet:       routine.DistinctLocalDays >= policy.MinDistinctLocalDays,
		SpanRequirementMet:       routine.LastSeenAt.Sub(routine.FirstSeenAt) >= policy.MinSpan,
		StatusEligible:           containsStatus(policy.EligibleStatuses, routine.Status),
	}
	result.Eligible = result.OccurrenceRequirementMet && result.DaysRequirementMet && result.SpanRequirementMet && result.StatusEligible
	if result.Eligible {
		result.ReasonCode = "routine_ready"
	} else if !result.StatusEligible {
		result.ReasonCode = "routine_status_ineligible"
	} else if !result.OccurrenceRequirementMet {
		result.ReasonCode = "routine_insufficient_occurrences"
	} else if !result.DaysRequirementMet {
		result.ReasonCode = "routine_insufficient_days"
	} else {
		result.ReasonCode = "routine_insufficient_span"
	}
	return result, nil
}

func containsStatus(values []routines.Status, wanted routines.Status) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sortedStatuses(values []routines.Status) []routines.Status {
	out := append([]routines.Status(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
