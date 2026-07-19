package deviation

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/context"
	"synora/internal/cge/routines"
)

type EvaluationStatus string

const (
	StatusEvaluated           EvaluationStatus = "evaluated"
	StatusPartial             EvaluationStatus = "partial"
	StatusInsufficientHistory EvaluationStatus = "insufficient_history"
	StatusAmbiguous           EvaluationStatus = "ambiguous"
	StatusAlreadyEvaluated    EvaluationStatus = "already_evaluated"
	StatusNotApplicable       EvaluationStatus = "not_applicable"
)

type Band string

const (
	BandUnknown  Band = "unknown"
	BandAligned  Band = "aligned"
	BandLow      Band = "low"
	BandModerate Band = "moderate"
	BandHigh     Band = "high"
)

type FactorKind string

const (
	FactorStructural     FactorKind = "structural"
	FactorTemporal       FactorKind = "temporal"
	FactorInterval       FactorKind = "interval"
	FactorContextQuality FactorKind = "context_quality"
	FactorHistorySupport FactorKind = "history_support"
)

type Factor struct {
	Kind            FactorKind
	Available       bool
	Score           Score
	Weight          Score
	EffectiveWeight Score
	ReasonCodes     []string
}

type RoutineReference struct {
	RoutineID           routines.RoutineID
	Revision            uint64
	SnapshotFingerprint string
	OccurrenceCount     uint64
	DistinctLocalDays   uint64
	FirstSeenAt         time.Time
	LastSeenAt          time.Time
}

type CandidateMatch struct {
	Routine RoutineReference

	ExactRoutineID bool
	ExactPattern   bool

	Structural     Factor
	Temporal       Factor
	Interval       Factor
	ContextQuality Factor
	HistorySupport Factor

	TotalScore Score
	Coverage   Score

	Rank        int
	ReasonCodes []string
}

type Assessment struct {
	PolicyNamespace   string
	PolicyVersion     string
	PolicyFingerprint string
	EvaluatedAt       time.Time

	OccurrenceID          routines.OccurrenceID
	OccurrenceFingerprint string
	RoutineID             routines.RoutineID

	Kind    routines.Kind
	Subject routines.Subject

	Status EvaluationStatus
	Band   Band

	Score    Score
	Coverage Score

	BestMatch  *CandidateMatch
	Candidates []CandidateMatch
	Baseline   []RoutineReference

	ReasonCodes []string
	Fingerprint string
}

type AssessmentSkip struct {
	Kind          routines.Kind
	ObservationID string
	Code          string
}

type PlanAssessment struct {
	ChainID             string
	TargetObservationID string
	PolicyFingerprint   string
	EvaluatedAt         time.Time
	Assessments         []Assessment
	Skipped             []AssessmentSkip
	Fingerprint         string
}

// Clone returns a detached assessment suitable for ephemeral storage.
func (a Assessment) Clone() Assessment {
	out := a
	out.Baseline = append([]RoutineReference(nil), a.Baseline...)
	out.Candidates = make([]CandidateMatch, len(a.Candidates))
	for i, candidate := range a.Candidates {
		out.Candidates[i] = copyCandidate(candidate)
	}
	if a.BestMatch != nil {
		best := copyCandidate(*a.BestMatch)
		out.BestMatch = &best
	}
	out.ReasonCodes = append([]string(nil), a.ReasonCodes...)
	return out
}

func validEvaluationStatus(value EvaluationStatus) bool {
	switch value {
	case StatusEvaluated, StatusPartial, StatusInsufficientHistory, StatusAmbiguous, StatusAlreadyEvaluated, StatusNotApplicable:
		return true
	default:
		return false
	}
}

func validBand(value Band) bool {
	switch value {
	case BandUnknown, BandAligned, BandLow, BandModerate, BandHigh:
		return true
	default:
		return false
	}
}

func validFactorKind(value FactorKind) bool {
	switch value {
	case FactorStructural, FactorTemporal, FactorInterval, FactorContextQuality, FactorHistorySupport:
		return true
	default:
		return false
	}
}

func validateFactor(factor Factor) error {
	if !validFactorKind(factor.Kind) || factor.Score.Validate() != nil || factor.Weight.Validate() != nil || factor.EffectiveWeight.Validate() != nil || factor.EffectiveWeight > factor.Weight || !factor.Available && factor.EffectiveWeight != 0 {
		return ErrInvalidDeviationAssessment
	}
	if err := validateCodes(factor.ReasonCodes); err != nil {
		return err
	}
	return nil
}

func validateCodes(codes []string) error {
	for i, code := range codes {
		if !validCode(code) || (i > 0 && codes[i-1] >= code) {
			return ErrInvalidDeviationAssessment
		}
	}
	return nil
}

func validCode(code string) bool {
	return code != "" && len([]rune(code)) <= 128 && strings.TrimSpace(code) == code && !strings.ContainsAny(code, "\r\n")
}

func statusBand(score Score, policy Policy) Band {
	switch {
	case score <= policy.AlignedMax:
		return BandAligned
	case score <= policy.LowMax:
		return BandLow
	case score <= policy.ModerateMax:
		return BandModerate
	default:
		return BandHigh
	}
}

func contextCoverage(quality context.ContextQuality) Score {
	switch quality {
	case context.QualityComplete:
		return MaxScore
	case context.QualityPartial:
		return 700
	default:
		return 400
	}
}

func factorCoverage(factors ...Factor) Score {
	var available, total int64
	for _, factor := range factors {
		if factor.Weight > 0 {
			total += int64(factor.Weight)
			if factor.Available {
				available += int64(factor.Weight)
			}
		}
	}
	if total == 0 {
		return 0
	}
	return roundedRatio(available, total)
}

func multiplyScores(left, right Score) Score {
	return clampScore((int64(left)*int64(right) + int64(MaxScore)/2) / int64(MaxScore))
}

func copyCandidate(value CandidateMatch) CandidateMatch {
	value.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	value.Structural.ReasonCodes = append([]string(nil), value.Structural.ReasonCodes...)
	value.Temporal.ReasonCodes = append([]string(nil), value.Temporal.ReasonCodes...)
	value.Interval.ReasonCodes = append([]string(nil), value.Interval.ReasonCodes...)
	value.ContextQuality.ReasonCodes = append([]string(nil), value.ContextQuality.ReasonCodes...)
	value.HistorySupport.ReasonCodes = append([]string(nil), value.HistorySupport.ReasonCodes...)
	return value
}

func sortCandidates(values []CandidateMatch) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].TotalScore != values[j].TotalScore {
			return values[i].TotalScore < values[j].TotalScore
		}
		if values[i].Coverage != values[j].Coverage {
			return values[i].Coverage > values[j].Coverage
		}
		if values[i].Routine.OccurrenceCount != values[j].Routine.OccurrenceCount {
			return values[i].Routine.OccurrenceCount > values[j].Routine.OccurrenceCount
		}
		if values[i].Routine.DistinctLocalDays != values[j].Routine.DistinctLocalDays {
			return values[i].Routine.DistinctLocalDays > values[j].Routine.DistinctLocalDays
		}
		return values[i].Routine.RoutineID < values[j].Routine.RoutineID
	})
	for i := range values {
		values[i].Rank = i + 1
	}
}

func (a Assessment) Validate() error {
	if !validText(a.PolicyNamespace, 128) || !validText(a.PolicyVersion, 128) || !validFingerprint(a.PolicyFingerprint) || !validFingerprint(a.OccurrenceFingerprint) || a.EvaluatedAt.IsZero() || !validEvaluationStatus(a.Status) || !validBand(a.Band) || a.Score.Validate() != nil || a.Coverage.Validate() != nil {
		return ErrInvalidDeviationAssessment
	}
	if !validOccurrenceID(a.OccurrenceID) || !validKind(a.Kind) || a.Subject.Validate() != nil {
		return ErrInvalidDeviationAssessment
	}
	if err := validateCodes(a.ReasonCodes); err != nil {
		return err
	}
	for i, reference := range a.Baseline {
		if err := validateReference(reference); err != nil || (i > 0 && a.Baseline[i-1].RoutineID >= reference.RoutineID) {
			return ErrInvalidDeviationAssessment
		}
	}
	for i, candidate := range a.Candidates {
		if err := validateCandidate(candidate); err != nil || candidate.Rank != i+1 || (i > 0 && compareCandidateOrder(a.Candidates[i-1], candidate) > 0) {
			return ErrInvalidDeviationAssessment
		}
	}
	if a.Status == StatusEvaluated || a.Status == StatusPartial || a.Status == StatusAmbiguous {
		if len(a.Candidates) == 0 || a.BestMatch == nil || a.Score != a.BestMatch.TotalScore || a.Coverage != a.BestMatch.Coverage {
			return ErrInvalidDeviationAssessment
		}
		if a.Band == BandUnknown {
			return ErrInvalidDeviationAssessment
		}
	} else if a.BestMatch != nil || a.Score != 0 || a.Band != BandUnknown {
		return ErrInvalidDeviationAssessment
	}
	if len(a.Candidates) > 0 && a.BestMatch != nil && a.BestMatch.Routine.RoutineID != a.Candidates[0].Routine.RoutineID {
		return ErrInvalidDeviationAssessment
	}
	expectedFingerprint, err := assessmentFingerprint(a)
	if err != nil || a.Fingerprint != expectedFingerprint || !validFingerprint(a.Fingerprint) {
		return ErrInvalidDeviationFingerprint
	}
	return nil
}

func validateCandidate(candidate CandidateMatch) error {
	if err := validateReference(candidate.Routine); err != nil || candidate.TotalScore.Validate() != nil || candidate.Coverage.Validate() != nil {
		return ErrInvalidDeviationAssessment
	}
	for _, factor := range []Factor{candidate.Structural, candidate.Temporal, candidate.Interval, candidate.ContextQuality, candidate.HistorySupport} {
		if err := validateFactor(factor); err != nil {
			return err
		}
	}
	return validateCodes(candidate.ReasonCodes)
}

func validateReference(reference RoutineReference) error {
	if !validRoutineID(reference.RoutineID) || reference.Revision == 0 || !validFingerprint(reference.SnapshotFingerprint) || reference.OccurrenceCount == 0 || reference.FirstSeenAt.IsZero() || reference.LastSeenAt.IsZero() || reference.LastSeenAt.Before(reference.FirstSeenAt) {
		return ErrInvalidDeviationAssessment
	}
	return nil
}

func validFingerprint(value string) bool {
	return len(value) == len("sha256:")+64 && value[:len("sha256:")] == "sha256:" && isHex(value[len("sha256:"):])
}

func compareCandidateOrder(a, b CandidateMatch) int {
	copyValues := []CandidateMatch{a, b}
	sortCandidates(copyValues)
	if copyValues[0].Routine.RoutineID == a.Routine.RoutineID {
		return -1
	}
	return 1
}

func validRoutineID(id routines.RoutineID) bool {
	value := string(id)
	return len(value) == len("cge-routine-")+64 && strings.HasPrefix(value, "cge-routine-") && isHex(value[len("cge-routine-"):])
}

func validOccurrenceID(id routines.OccurrenceID) bool {
	value := string(id)
	return len(value) == len("cge-routine-occurrence-")+64 && strings.HasPrefix(value, "cge-routine-occurrence-") && isHex(value[len("cge-routine-occurrence-"):])
}

func isHex(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validKind(value routines.Kind) bool {
	return value == routines.KindPresence || value == routines.KindTransition
}

func formatReason(status EvaluationStatus) string {
	return "deviation." + string(status)
}

func fmtCode(format string, values ...any) string { return fmt.Sprintf(format, values...) }
