package episodes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

type IngestDecision string

const (
	DecisionAttachExisting IngestDecision = "attach_existing"
	DecisionCreateEpisode  IngestDecision = "create_episode"
	DecisionAmbiguous      IngestDecision = "ambiguous"
	DecisionDuplicate      IngestDecision = "duplicate"
	DecisionRejected       IngestDecision = "rejected"
)

type IngestPlan struct {
	Decision           IngestDecision
	ObservationEventID string
	SelectedEpisodeID  EpisodeID
	Candidates         []CandidateAssessment
	ReasonCodes        []string
	SourceRevision     uint64
	PolicyFingerprint  string
}

func (p IngestPlan) Clone() IngestPlan {
	out := p
	out.Candidates = make([]CandidateAssessment, len(p.Candidates))
	for i, candidate := range p.Candidates {
		out.Candidates[i] = candidate
		out.Candidates[i].Reasons = append([]FactorAssessment(nil), candidate.Reasons...)
	}
	out.ReasonCodes = append([]string(nil), p.ReasonCodes...)
	return out
}

func (p IngestPlan) Validate() error {
	if p.Decision == "" || p.ObservationEventID == "" || p.PolicyFingerprint == "" {
		return ErrInvalidPlan
	}
	if p.Decision == DecisionAttachExisting && p.SelectedEpisodeID == "" {
		return ErrInvalidPlan
	}
	if p.Decision != DecisionAttachExisting && p.SelectedEpisodeID != "" {
		return ErrInvalidPlan
	}
	return nil
}

func PlanIngest(snapshot Snapshot, observation ObservationRef, topology TopologyView, policy Policy) (IngestPlan, error) {
	if err := policy.Validate(); err != nil {
		return IngestPlan{}, err
	}
	if err := observation.Validate(); err != nil {
		return IngestPlan{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return IngestPlan{}, err
	}
	if snapshot.PolicyFingerprint != "" && snapshot.PolicyFingerprint != policy.Fingerprint() {
		return IngestPlan{}, fmt.Errorf("%w: policy fingerprint", ErrInvalidSnapshot)
	}
	plan := IngestPlan{Decision: DecisionCreateEpisode, ObservationEventID: observation.EventID, SourceRevision: snapshot.Revision, PolicyFingerprint: policy.Fingerprint()}
	if _, exists := snapshot.EventIndex[observation.EventID]; exists {
		plan.Decision = DecisionDuplicate
		plan.ReasonCodes = []string{"observation.duplicate"}
		return plan, nil
	}
	candidates := make([]CandidateAssessment, 0, len(snapshot.Episodes))
	for _, episode := range snapshot.Episodes {
		candidates = append(candidates, scoreCandidate(episode, observation, topology, policy))
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].EpisodeID < candidates[j].EpisodeID
	})
	if len(candidates) > policy.MaxCandidates {
		candidates = candidates[:policy.MaxCandidates]
	}
	plan.Candidates = candidates
	eligible := make([]CandidateAssessment, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Eligible {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		plan.ReasonCodes = []string{"episode.create_no_eligible_candidate"}
		return plan, nil
	}
	best := eligible[0]
	second := 0
	if len(eligible) > 1 {
		second = eligible[1].Score
	}
	margin := best.Score - second
	if best.Score >= policy.MinAttachScore && margin >= policy.MinDecisionMargin {
		plan.Decision = DecisionAttachExisting
		plan.SelectedEpisodeID = best.EpisodeID
		plan.ReasonCodes = []string{"episode.attach_score_threshold", "episode.attach_margin"}
		return plan, nil
	}
	plausible := 0
	threshold := policy.MinAttachScore - policy.MinDecisionMargin
	if threshold < 0 {
		threshold = 0
	}
	for _, candidate := range eligible {
		if candidate.Score >= threshold {
			plausible++
		}
	}
	if (best.Score >= policy.MinAttachScore && margin < policy.MinDecisionMargin) || plausible >= 2 {
		plan.Decision = DecisionAmbiguous
		plan.ReasonCodes = []string{"episode.ambiguous_margin"}
		if plausible >= 2 {
			plan.ReasonCodes = append(plan.ReasonCodes, "episode.multiple_plausible_candidates")
		}
		return plan, nil
	}
	plan.ReasonCodes = []string{"episode.create_below_attach_threshold"}
	return plan, nil
}

func subjectFingerprint(subject SubjectRef) string {
	subject = normalizeSubject(subject)
	return fmt.Sprintf("%s\x00%s\x00%v", subject.Kind, subject.EntityID, subject.CandidateEntityIDs)
}

func DeriveEpisodeID(policy Policy, observation ObservationRef) (EpisodeID, error) {
	if err := policy.Validate(); err != nil {
		return "", err
	}
	if err := observation.Validate(); err != nil {
		return "", err
	}
	canonical := fmt.Sprintf("%s\x00%s\x00%s\x00%s", policy.Fingerprint(), observation.EventID, observation.ObservedAt.UTC().Format(time.RFC3339Nano), subjectFingerprint(observation.Subject))
	digest := sha256.Sum256([]byte(canonical))
	return EpisodeID("episode-" + hex.EncodeToString(digest[:])), nil
}
