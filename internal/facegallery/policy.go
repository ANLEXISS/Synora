package facegallery

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

const (
	CandidateThreshold = 0.65
	MatchThreshold     = 0.90
	MinimumQuality     = 0.70
	CandidateTTL       = 24 * time.Hour
	MaxCandidates      = 16
	MaxPerEpisode      = 4
	MaxPerDay          = 12
)

type Observation struct {
	Score      float64
	Quality    float64
	FaceCount  int
	EpisodeKey string
	TrackKey   string
	SampleKey  string
	Provenance string
	ObservedAt time.Time
}

type SemanticResult struct {
	Result         string `json:"result"`
	CandidateState string `json:"candidate_state,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type SyntheticObservation struct{ CaseID string }

type Backend interface {
	Status() string
	Score(context.Context, SyntheticObservation) (float64, error)
}

type UnavailableBackend struct{}

func (UnavailableBackend) Status() string { return "unavailable" }
func (UnavailableBackend) Score(context.Context, SyntheticObservation) (float64, error) {
	return 0, errors.New("face backend unavailable")
}

// DeterministicTestBackend maps named synthetic cases to fixed test scores;
// it accepts no images, paths, identities, or model files.
type DeterministicTestBackend struct{ Scores map[string]float64 }

func (DeterministicTestBackend) Status() string { return "simulated_test" }
func (b DeterministicTestBackend) Score(ctx context.Context, observation SyntheticObservation) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	score, ok := b.Scores[observation.CaseID]
	if !ok || math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
		return 0, errors.New("synthetic face case unavailable")
	}
	return score, nil
}

// DeterministicTestLifecycle cannot be constructed around a real backend and
// only retains synthetic opaque tokens in memory. It never writes to Vault.
type DeterministicTestLifecycle struct {
	candidates map[string]testCandidate
	anchors    map[string][]time.Time
	promoted   map[string]bool
	perDay     map[string]int
	perEpisode map[string]int
	now        func() time.Time
}

type testCandidate struct {
	episode string
	track   string
	samples map[string]struct{}
	expires time.Time
}

func NewDeterministicTestLifecycle(now func() time.Time) *DeterministicTestLifecycle {
	if now == nil {
		now = time.Now
	}
	return &DeterministicTestLifecycle{candidates: map[string]testCandidate{}, anchors: map[string][]time.Time{}, promoted: map[string]bool{}, perDay: map[string]int{}, perEpisode: map[string]int{}, now: now}
}

// Evaluate is the only score mapping in this inactive component. It must be
// called by synthetic tests; production processing remains unavailable.
func Evaluate(score float64) SemanticResult {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
		return SemanticResult{Result: "unavailable", Reason: "invalid_test_score"}
	}
	if score < CandidateThreshold {
		return SemanticResult{Result: "unknown"}
	}
	if score < MatchThreshold {
		return SemanticResult{Result: "candidate", CandidateState: "candidate_pending"}
	}
	return SemanticResult{Result: "ambiguous", Reason: "consensus_required"}
}

// Observe exercises candidate/anchor transitions only for test provenance.
// No score, identity, episode key, track key, or sample key leaves this object.
func (l *DeterministicTestLifecycle) Observe(observation Observation) SemanticResult {
	if l == nil || observation.Provenance != "simulated_test" {
		return SemanticResult{Result: "unavailable", Reason: "real_backend_unavailable"}
	}
	now := observation.ObservedAt.UTC()
	if now.IsZero() {
		now = l.now().UTC()
	}
	l.Expire(now)
	if observation.FaceCount != 1 || observation.Quality < MinimumQuality || math.IsNaN(observation.Quality) || observation.Quality > 1 ||
		strings.TrimSpace(observation.EpisodeKey) == "" || strings.TrimSpace(observation.TrackKey) == "" || strings.TrimSpace(observation.SampleKey) == "" {
		return SemanticResult{Result: "ambiguous", CandidateState: "candidate_rejected", Reason: "quality_or_provenance_rejected"}
	}
	if observation.Score < 0 || observation.Score > 1 || math.IsNaN(observation.Score) || math.IsInf(observation.Score, 0) {
		return SemanticResult{Result: "unavailable", Reason: "invalid_test_score"}
	}
	if observation.Score < CandidateThreshold {
		return SemanticResult{Result: "unknown"}
	}
	candidateKey := observation.EpisodeKey + "\x00" + observation.TrackKey
	if observation.Score < MatchThreshold {
		candidate, exists := l.candidates[candidateKey]
		if !exists {
			day := now.Format("2006-01-02")
			if len(l.candidates) >= MaxCandidates || l.perEpisode[observation.EpisodeKey] >= MaxPerEpisode || l.perDay[day] >= MaxPerDay {
				return SemanticResult{Result: "unknown", CandidateState: "candidate_rejected", Reason: "quota_exceeded"}
			}
			candidate = testCandidate{episode: observation.EpisodeKey, track: observation.TrackKey, samples: map[string]struct{}{}, expires: now.Add(CandidateTTL)}
			l.perEpisode[observation.EpisodeKey]++
			l.perDay[day]++
		}
		if _, duplicate := candidate.samples[observation.SampleKey]; duplicate {
			return SemanticResult{Result: "candidate", CandidateState: "candidate_pending", Reason: "duplicate_suppressed"}
		}
		candidate.samples[observation.SampleKey] = struct{}{}
		candidate.expires = now.Add(CandidateTTL)
		l.candidates[candidateKey] = candidate
		return SemanticResult{Result: "candidate", CandidateState: "candidate_pending"}
	}
	anchors := l.anchors[candidateKey]
	if len(anchors) == 0 || now.Sub(anchors[len(anchors)-1]) >= time.Second {
		anchors = append(anchors, now)
	}
	l.anchors[candidateKey] = anchors
	if len(anchors) < 2 || anchors[len(anchors)-1].Sub(anchors[0]) < 2*time.Second {
		return SemanticResult{Result: "ambiguous", Reason: "consensus_required"}
	}
	if _, exists := l.candidates[candidateKey]; !exists {
		return SemanticResult{Result: "ambiguous", Reason: "candidate_anchor_required"}
	}
	l.promoted[candidateKey] = true // synthetic in-memory test vault only
	return SemanticResult{Result: "recognized", CandidateState: "candidate_promoted"}
}

func (l *DeterministicTestLifecycle) Expire(now time.Time) int {
	if l == nil {
		return 0
	}
	expired := 0
	for key, candidate := range l.candidates {
		if !now.Before(candidate.expires) {
			delete(l.candidates, key)
			delete(l.anchors, key)
			delete(l.promoted, key)
			expired++
		}
	}
	return expired
}

func (l *DeterministicTestLifecycle) ExpireTransitions(now time.Time) []SemanticResult {
	count := l.Expire(now)
	results := make([]SemanticResult, count)
	for i := range results {
		results[i] = SemanticResult{Result: "unknown", CandidateState: "candidate_expired", Reason: "ttl_elapsed"}
	}
	return results
}

func (l *DeterministicTestLifecycle) PromotedCount() int {
	if l == nil {
		return 0
	}
	return len(l.promoted)
}

func (o Observation) Validate() error {
	if o.Provenance != "simulated_test" {
		return errors.New("only simulated test observations are accepted")
	}
	if o.FaceCount < 0 {
		return errors.New("invalid synthetic face count")
	}
	return nil
}
