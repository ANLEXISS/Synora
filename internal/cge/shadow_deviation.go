package cge

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"synora/internal/cge/deviation"
	"synora/internal/cge/routines"
)

// ShadowDeviationResult is a bounded orchestration summary. It contains no
// subject, routine, occurrence, topology, or baseline identifiers.
type ShadowDeviationResult struct {
	Attempted       bool
	Completed       bool
	AssessmentCount int

	EvaluatedCount           int
	PartialCount             int
	InsufficientHistoryCount int
	AmbiguousCount           int
	AlreadyEvaluatedCount    int
	NotApplicableCount       int

	HighestBand    deviation.Band
	HighestScore   deviation.Score
	LowestCoverage deviation.Score

	ErrorCode string
}

// DeviationAssessmentSummary is the redacted read-only view retained by the
// ShadowEngine. It intentionally omits all identifiers and structural data.
type DeviationAssessmentSummary struct {
	EvaluatedAt           time.Time
	Kind                  routines.Kind
	Status                deviation.EvaluationStatus
	Band                  deviation.Band
	Score                 deviation.Score
	Coverage              deviation.Score
	CandidateCount        int
	BaselineRoutineCount  int
	BestMatchExactRoutine bool
	BestMatchRevision     uint64
	BestMatchOccurrences  uint64
	BestMatchDistinctDays uint64
	ReasonCodes           []string
	Fingerprint           string
}

// RecentDeviationStore is an in-memory bounded FIFO. It has no persistence,
// no background worker, and stores detached assessments only.
type RecentDeviationStore struct {
	mu          sync.RWMutex
	limit       int
	assessments []deviation.Assessment
}

func NewRecentDeviationStore(limit int) (*RecentDeviationStore, error) {
	if limit < 0 || limit > MaxShadowDeviationRecentAssessments {
		return nil, fmt.Errorf("%w: recent assessment limit", ErrInvalidShadowConfig)
	}
	return &RecentDeviationStore{limit: limit}, nil
}

func (s *RecentDeviationStore) Add(assessment deviation.Assessment) error {
	_, err := s.add(assessment)
	return err
}

func (s *RecentDeviationStore) add(assessment deviation.Assessment) (bool, error) {
	if s == nil {
		return false, nil
	}
	if err := assessment.Validate(); err != nil {
		return false, err
	}
	if s.limit == 0 {
		return false, nil
	}
	clone := assessment.Clone()
	s.mu.Lock()
	defer s.mu.Unlock()
	position := sort.Search(len(s.assessments), func(i int) bool {
		if !s.assessments[i].EvaluatedAt.Equal(clone.EvaluatedAt) {
			return !s.assessments[i].EvaluatedAt.Before(clone.EvaluatedAt)
		}
		return s.assessments[i].Fingerprint >= clone.Fingerprint
	})
	s.assessments = append(s.assessments, deviation.Assessment{})
	copy(s.assessments[position+1:], s.assessments[position:])
	s.assessments[position] = clone
	evicted := len(s.assessments) > s.limit
	if evicted {
		s.assessments[0] = deviation.Assessment{}
		s.assessments = s.assessments[1:]
	}
	return evicted, nil
}

func (s *RecentDeviationStore) List(limit int) []deviation.Assessment {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.assessments) {
		limit = len(s.assessments)
	}
	start := len(s.assessments) - limit
	out := make([]deviation.Assessment, 0, limit)
	for _, assessment := range s.assessments[start:] {
		out = append(out, assessment.Clone())
	}
	return out
}

func (s *RecentDeviationStore) Count() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.assessments)
}

func (s *RecentDeviationStore) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.assessments = nil
	s.mu.Unlock()
}

func deviationSummary(assessment deviation.Assessment) DeviationAssessmentSummary {
	summary := DeviationAssessmentSummary{
		EvaluatedAt:          assessment.EvaluatedAt,
		Kind:                 assessment.Kind,
		Status:               assessment.Status,
		Band:                 assessment.Band,
		Score:                assessment.Score,
		Coverage:             assessment.Coverage,
		CandidateCount:       len(assessment.Candidates),
		BaselineRoutineCount: len(assessment.Baseline),
		ReasonCodes:          append([]string(nil), assessment.ReasonCodes...),
		Fingerprint:          assessment.Fingerprint,
	}
	if assessment.BestMatch != nil {
		summary.BestMatchExactRoutine = assessment.BestMatch.ExactRoutineID
		summary.BestMatchRevision = assessment.BestMatch.Routine.Revision
		summary.BestMatchOccurrences = assessment.BestMatch.Routine.OccurrenceCount
		summary.BestMatchDistinctDays = assessment.BestMatch.Routine.DistinctLocalDays
	}
	return summary
}

func (e *ShadowEngine) RecentDeviationAssessments(limit int) []DeviationAssessmentSummary {
	if e == nil || e.deviationStore == nil {
		return nil
	}
	assessments := e.deviationStore.List(limit)
	out := make([]DeviationAssessmentSummary, 0, len(assessments))
	for _, assessment := range assessments {
		out = append(out, deviationSummary(assessment))
	}
	return out
}

// LastDeviationAssessmentDetailed returns a defensive copy for local
// demonstration and qualification tooling. The assessment store remains
// ephemeral and is never used to make a security decision.
func (e *ShadowEngine) LastDeviationAssessmentDetailed() (deviation.Assessment, bool) {
	if e == nil || e.deviationStore == nil {
		return deviation.Assessment{}, false
	}
	items := e.deviationStore.List(1)
	if len(items) == 0 {
		return deviation.Assessment{}, false
	}
	return items[len(items)-1].Clone(), true
}

// LastDeviationResult returns the latest identifier-free orchestration
// summary. It is diagnostic only and is never used to select a runtime path.
func (e *ShadowEngine) LastDeviationResult() ShadowDeviationResult {
	if e == nil {
		return ShadowDeviationResult{HighestBand: deviation.BandUnknown}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastDeviation
}
