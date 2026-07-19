package deviation

import (
	"fmt"
	"sort"
	"time"

	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/routines"
)

// EvaluateOccurrence compares one occurrence to a caller-provided baseline.
// It performs no registry lookup, mutation, persistence, retry, or clock
// access. The caller must provide the baseline before learning the occurrence.
func EvaluateOccurrence(occurrence routines.Occurrence, candidates []routines.Snapshot, evaluatedAt time.Time, policy Policy) (Assessment, error) {
	if err := policy.Validate(); err != nil {
		return Assessment{}, err
	}
	if evaluatedAt.IsZero() {
		return Assessment{}, ErrInvalidTimestamp
	}
	if err := occurrence.Validate(); err != nil {
		return Assessment{}, fmt.Errorf("%w: %v", ErrInvalidDeviationOccurrence, err)
	}
	policyFingerprint, err := policy.Fingerprint()
	if err != nil {
		return Assessment{}, err
	}
	occurrenceFingerprintValue, err := occurrenceFingerprint(occurrence)
	if err != nil {
		return Assessment{}, err
	}
	assessment := Assessment{PolicyNamespace: policy.Namespace, PolicyVersion: policy.Version, PolicyFingerprint: policyFingerprint, OccurrenceFingerprint: occurrenceFingerprintValue, EvaluatedAt: evaluatedAt, OccurrenceID: occurrence.ID, RoutineID: occurrence.RoutineID, Kind: occurrence.Kind, Subject: occurrence.Subject, Band: BandUnknown}
	baseline := make([]RoutineReference, 0, len(candidates))
	seenCandidates := make(map[routines.RoutineID]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.Subject.Validate() != nil {
			return Assessment{}, ErrInvalidDeviationCandidate
		}
		if candidate.Subject != occurrence.Subject {
			return Assessment{}, ErrDeviationSubjectMismatch
		}
		if candidate.Kind != occurrence.Kind {
			return Assessment{}, ErrDeviationKindMismatch
		}
		if _, exists := seenCandidates[candidate.ID]; exists {
			return Assessment{}, ErrInvalidDeviationCandidate
		}
		seenCandidates[candidate.ID] = struct{}{}
		reference, err := referenceFromSnapshot(candidate)
		if err != nil {
			return Assessment{}, err
		}
		baseline = append(baseline, reference)
	}
	sort.Slice(baseline, func(i, j int) bool { return baseline[i].RoutineID < baseline[j].RoutineID })
	assessment.Baseline = baseline
	if len(candidates) > policy.MaxCandidateRoutines {
		return Assessment{}, fmt.Errorf("%w: %d", ErrCandidateLimitExceeded, len(candidates))
	}

	var alreadyInRoutine routines.RoutineID
	for _, candidate := range candidates {
		for _, reference := range candidate.Occurrences {
			if reference.ID != occurrence.ID {
				continue
			}
			if occurrenceRefEqual(reference, occurrence.Ref()) {
				if alreadyInRoutine != "" && alreadyInRoutine != candidate.ID {
					return Assessment{}, ErrDeviationOccurrenceCollision
				}
				alreadyInRoutine = candidate.ID
				assessment.Status = StatusAlreadyEvaluated
				assessment.ReasonCodes = []string{"deviation.already_evaluated"}
				continue
			}
			return Assessment{}, ErrDeviationOccurrenceCollision
		}
	}
	if alreadyInRoutine != "" {
		return finalizeAssessment(assessment)
	}

	if occurrence.ContextQuality == cgecontext.QualityPartial && !policy.AllowPartialContext {
		assessment.Status = StatusNotApplicable
		assessment.ReasonCodes = []string{"deviation.not_applicable", "context.partial"}
		return finalizeAssessment(assessment)
	}
	if occurrence.ContextQuality == cgecontext.QualityUnknown && !policy.AllowPartialContext {
		assessment.Status = StatusNotApplicable
		assessment.ReasonCodes = []string{"deviation.not_applicable", "context.unknown"}
		return finalizeAssessment(assessment)
	}

	eligible := make([]routines.Snapshot, 0, len(candidates))
	for _, candidate := range candidates {
		readiness, err := EvaluateRoutineReadiness(candidate, policy)
		if err != nil {
			return Assessment{}, err
		}
		if readiness.Eligible {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		assessment.Status = StatusInsufficientHistory
		assessment.ReasonCodes = []string{"deviation.insufficient_history", "baseline.no_eligible_routine"}
		return finalizeAssessment(assessment)
	}
	for _, candidate := range eligible {
		match, err := evaluateCandidate(occurrence, candidate, policy)
		if err != nil {
			return Assessment{}, err
		}
		assessment.Candidates = append(assessment.Candidates, match)
	}
	sortCandidates(assessment.Candidates)
	for i := range assessment.Candidates {
		assessment.Candidates[i].ReasonCodes = uniqueCodes(append(assessment.Candidates[i].ReasonCodes, "pattern.closest_match"))
	}
	assessment.BestMatch = copyCandidatePointer(assessment.Candidates[0])
	assessment.Score = assessment.BestMatch.TotalScore
	assessment.Coverage = assessment.BestMatch.Coverage
	assessment.Band = statusBand(assessment.Score, policy)
	assessment.Status = StatusEvaluated
	if occurrence.ContextQuality != cgecontext.QualityComplete || assessment.BestMatch.Structural.EffectiveWeight < assessment.BestMatch.Structural.Weight || assessment.BestMatch.Temporal.EffectiveWeight < assessment.BestMatch.Temporal.Weight || assessment.BestMatch.Interval.EffectiveWeight < assessment.BestMatch.Interval.Weight {
		assessment.Status = StatusPartial
		assessment.ReasonCodes = append(assessment.ReasonCodes, "deviation.partial")
	}
	if len(assessment.Candidates) > 1 && assessment.Candidates[1].TotalScore-assessment.Candidates[0].TotalScore <= policy.AmbiguityMargin {
		assessment.Status = StatusAmbiguous
		assessment.ReasonCodes = append(assessment.ReasonCodes, "deviation.ambiguous")
		if len(assessment.Candidates) > 2 {
			assessment.Candidates = assessment.Candidates[:2]
		}
	}
	assessment.ReasonCodes = append(assessment.ReasonCodes, formatReason(assessment.Status), bandReason(assessment.Band))
	return finalizeAssessment(assessment)
}

func evaluateCandidate(occurrence routines.Occurrence, candidate routines.Snapshot, policy Policy) (CandidateMatch, error) {
	structural, exactPatternValue, err := patternFactor(occurrence.Pattern, candidate.Pattern)
	if err != nil {
		return CandidateMatch{}, err
	}
	structural.Weight = policy.StructuralWeight
	if structural.Available {
		structural.EffectiveWeight = structural.Weight
	}
	temporal, err := EvaluateTemporal(occurrence, candidate, policy)
	if err != nil {
		return CandidateMatch{}, err
	}
	temporal.Weight = policy.TemporalWeight
	if temporal.Available {
		temporal.EffectiveWeight = temporal.Weight
	}
	interval, err := EvaluateInterval(occurrence, candidate, policy)
	if err != nil {
		return CandidateMatch{}, err
	}
	interval.Weight = policy.IntervalWeight
	if interval.Available {
		interval.EffectiveWeight = interval.Weight
	}
	readiness, err := EvaluateRoutineReadiness(candidate, policy)
	if err != nil {
		return CandidateMatch{}, err
	}
	contextFactor := Factor{Kind: FactorContextQuality, Available: true, Score: 0, ReasonCodes: []string{"context.complete"}}
	if occurrence.ContextQuality == cgecontext.QualityPartial {
		contextFactor.ReasonCodes = []string{"context.partial"}
	} else if occurrence.ContextQuality == cgecontext.QualityUnknown {
		contextFactor.ReasonCodes = []string{"context.unknown"}
	}
	historyFactor := Factor{Kind: FactorHistorySupport, Available: true, Score: 0, ReasonCodes: []string{readiness.ReasonCode}}
	coverage := multiplyScores(factorCoverage(structural, temporal, interval), contextCoverage(occurrence.ContextQuality))
	availableWeight := int64(structural.EffectiveWeight + temporal.EffectiveWeight + interval.EffectiveWeight)
	total := Score(0)
	if availableWeight > 0 {
		total = clampScore((weightedScore(structural.Score, structural.EffectiveWeight) + weightedScore(temporal.Score, temporal.EffectiveWeight) + weightedScore(interval.Score, interval.EffectiveWeight) + availableWeight/2) / availableWeight)
	}
	reasons := []string{}
	if exactPatternValue {
		reasons = append(reasons, "pattern.exact")
	}
	if occurrence.RoutineID == candidate.ID {
		reasons = append(reasons, "routine.exact_id")
	}
	if !structural.Available || !temporal.Available || !interval.Available || occurrence.ContextQuality != "complete" {
		reasons = append(reasons, "deviation.partial")
	}
	return CandidateMatch{Routine: routineReference(candidate), ExactRoutineID: occurrence.RoutineID == candidate.ID, ExactPattern: exactPatternValue, Structural: structural, Temporal: temporal, Interval: interval, ContextQuality: contextFactor, HistorySupport: historyFactor, TotalScore: total, Coverage: coverage, ReasonCodes: uniqueCodes(reasons)}, nil
}

func routineReference(snapshot routines.Snapshot) RoutineReference {
	fingerprint, _ := snapshot.Fingerprint()
	return RoutineReference{RoutineID: snapshot.ID, Revision: snapshot.Revision, SnapshotFingerprint: fingerprint, OccurrenceCount: snapshot.OccurrenceCount, DistinctLocalDays: snapshot.DistinctLocalDays, FirstSeenAt: snapshot.FirstSeenAt, LastSeenAt: snapshot.LastSeenAt}
}

func referenceFromSnapshot(snapshot routines.Snapshot) (RoutineReference, error) {
	fingerprint, err := snapshot.Fingerprint()
	if err != nil {
		return RoutineReference{}, fmt.Errorf("%w: %v", ErrInvalidDeviationCandidate, err)
	}
	return RoutineReference{RoutineID: snapshot.ID, Revision: snapshot.Revision, SnapshotFingerprint: fingerprint, OccurrenceCount: snapshot.OccurrenceCount, DistinctLocalDays: snapshot.DistinctLocalDays, FirstSeenAt: snapshot.FirstSeenAt, LastSeenAt: snapshot.LastSeenAt}, nil
}

func copyCandidatePointer(value CandidateMatch) *CandidateMatch {
	copy := copyCandidate(value)
	return &copy
}

func occurrenceRefEqual(a, b routines.OccurrenceRef) bool {
	if a.ID != b.ID || !a.ObservedAt.Equal(b.ObservedAt) || a.Weekday != b.Weekday || a.TimeBucket != b.TimeBucket || a.DayPart != b.DayPart || a.LocalDate != b.LocalDate || a.ContextQuality != b.ContextQuality || len(a.ObservationIDs) != len(b.ObservationIDs) || len(a.TopologyRevisions) != len(b.TopologyRevisions) {
		return false
	}
	for i := range a.ObservationIDs {
		if a.ObservationIDs[i] != b.ObservationIDs[i] {
			return false
		}
	}
	for i := range a.TopologyRevisions {
		if a.TopologyRevisions[i] != b.TopologyRevisions[i] {
			return false
		}
	}
	return true
}

func bandReason(band Band) string { return "score." + string(band) }

func finalizeAssessment(assessment Assessment) (Assessment, error) {
	assessment.ReasonCodes = uniqueCodes(assessment.ReasonCodes)
	fingerprint, err := assessmentFingerprint(assessment)
	if err != nil {
		return Assessment{}, err
	}
	assessment.Fingerprint = fingerprint
	if err := assessment.Validate(); err != nil {
		return Assessment{}, err
	}
	return assessment, nil
}

// EvaluateLearningPlan compares the already extracted occurrences in plan to
// baselines supplied by the caller. It never extracts, learns, or mutates.
func EvaluateLearningPlan(plan routines.LearningPlan, candidates map[routines.OccurrenceID][]routines.Snapshot, evaluatedAt time.Time, policy Policy) (PlanAssessment, error) {
	if err := policy.Validate(); err != nil {
		return PlanAssessment{}, err
	}
	if evaluatedAt.IsZero() {
		return PlanAssessment{}, ErrInvalidTimestamp
	}
	policyFingerprint, err := policy.Fingerprint()
	if err != nil {
		return PlanAssessment{}, err
	}
	result := PlanAssessment{ChainID: string(plan.ChainID), TargetObservationID: plan.TargetObservationID, PolicyFingerprint: policyFingerprint, EvaluatedAt: evaluatedAt}
	occurrences := append([]routines.Occurrence(nil), plan.Occurrences...)
	sort.SliceStable(occurrences, func(i, j int) bool {
		rank := func(kind routines.Kind) int {
			if kind == routines.KindPresence {
				return 0
			}
			return 1
		}
		if rank(occurrences[i].Kind) != rank(occurrences[j].Kind) {
			return rank(occurrences[i].Kind) < rank(occurrences[j].Kind)
		}
		return occurrences[i].ID < occurrences[j].ID
	})
	for _, occurrence := range occurrences {
		assessment, err := EvaluateOccurrence(occurrence, candidates[occurrence.ID], evaluatedAt, policy)
		if err != nil {
			return PlanAssessment{}, err
		}
		result.Assessments = append(result.Assessments, assessment)
	}
	for _, skipped := range plan.Skipped {
		result.Skipped = append(result.Skipped, AssessmentSkip{Kind: skipped.Kind, ObservationID: skipped.ObservationID, Code: string(skipped.Code)})
	}
	sort.SliceStable(result.Skipped, func(i, j int) bool {
		if result.Skipped[i].Kind != result.Skipped[j].Kind {
			return result.Skipped[i].Kind < result.Skipped[j].Kind
		}
		if result.Skipped[i].ObservationID != result.Skipped[j].ObservationID {
			return result.Skipped[i].ObservationID < result.Skipped[j].ObservationID
		}
		return result.Skipped[i].Code < result.Skipped[j].Code
	})
	fingerprint, err := planAssessmentFingerprint(result)
	if err != nil {
		return PlanAssessment{}, err
	}
	result.Fingerprint = fingerprint
	return result, nil
}
