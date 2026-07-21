package shadowworkflow

import (
	"sort"
	"time"

	"synora/internal/cge/cognitiverecommendation"
	"synora/internal/cge/cognitivesituation"
	"synora/internal/cge/durableworkflow"
)

type CognitiveSituationSnapshot struct {
	WorkflowRevision uint64
	Situations       []cognitivesituation.CognitiveSituation
	EpisodeIndex     map[string]int
	Digest           string
}

type CognitiveRecommendationSnapshot struct {
	WorkflowRevision        uint64
	SituationSnapshotDigest string
	RecommendationSets      []cognitiverecommendation.CognitiveRecommendationSet
	EpisodeIndex            map[string]int
	Digest                  string
}

type CognitiveProjectionSnapshot struct {
	WorkflowRevision uint64
	Situations       CognitiveSituationSnapshot
	Recommendations  CognitiveRecommendationSnapshot
	Digest           string
}

type cognitiveProjectionCache struct {
	snapshot CognitiveProjectionSnapshot
}

func (s CognitiveSituationSnapshot) Clone() CognitiveSituationSnapshot {
	out := s
	out.Situations = make([]cognitivesituation.CognitiveSituation, len(s.Situations))
	for i, value := range s.Situations {
		out.Situations[i] = value.Clone()
	}
	out.EpisodeIndex = make(map[string]int, len(s.EpisodeIndex))
	for key, value := range s.EpisodeIndex {
		out.EpisodeIndex[key] = value
	}
	return out
}

func (s CognitiveRecommendationSnapshot) Clone() CognitiveRecommendationSnapshot {
	out := s
	out.RecommendationSets = make([]cognitiverecommendation.CognitiveRecommendationSet, len(s.RecommendationSets))
	for i, value := range s.RecommendationSets {
		out.RecommendationSets[i] = value.Clone()
	}
	out.EpisodeIndex = make(map[string]int, len(s.EpisodeIndex))
	for key, value := range s.EpisodeIndex {
		out.EpisodeIndex[key] = value
	}
	return out
}

func (s CognitiveProjectionSnapshot) Clone() CognitiveProjectionSnapshot {
	return CognitiveProjectionSnapshot{WorkflowRevision: s.WorkflowRevision, Situations: s.Situations.Clone(), Recommendations: s.Recommendations.Clone(), Digest: s.Digest}
}

func (r *Runtime) expectedSituationDepth() cognitivesituation.ExpectedPipelineDepth {
	switch r.cfg.PipelineDepth {
	case DepthEpisode:
		return cognitivesituation.DepthEpisode
	case DepthSituationFacts:
		return cognitivesituation.DepthSituationFacts
	case DepthSituationHypotheses:
		return cognitivesituation.DepthSituationHypotheses
	case DepthEvidenceDiscrimination:
		return cognitivesituation.DepthEvidenceDiscrimination
	case DepthAdvisoryRequests:
		return cognitivesituation.DepthAdvisoryRequests
	case DepthCapabilityMapping:
		return cognitivesituation.DepthCapabilityMapping
	case DepthAuthorizationBoundary:
		return cognitivesituation.DepthAuthorizationBoundary
	default:
		return cognitivesituation.DepthEpisode
	}
}

func (r *Runtime) rebuildCognitiveSituations(state durableworkflow.WorkflowState) error {
	return r.rebuildCognitiveProjection(state)
}

func (r *Runtime) rebuildCognitiveProjection(state durableworkflow.WorkflowState) error {
	started := time.Now()
	policy := cognitivesituation.DefaultPolicy()
	recommendationPolicy := cognitiverecommendation.DefaultPolicy()
	situations := make([]cognitivesituation.CognitiveSituation, 0, len(state.Episodes))
	for _, episode := range state.Episodes {
		value, err := cognitivesituation.Build(cognitivesituation.BuildInput{Workflow: state, EpisodeID: episode.EpisodeID, ExpectedDepth: r.expectedSituationDepth()}, policy)
		if err != nil {
			r.metrics.add("recommendation_build_failures")
			return err
		}
		situations = append(situations, value.Clone())
	}
	sort.Slice(situations, func(i, j int) bool { return situations[i].EpisodeID < situations[j].EpisodeID })
	situationSnapshot := CognitiveSituationSnapshot{WorkflowRevision: state.Revision, Situations: situations, EpisodeIndex: indexSituations(situations)}
	situationSnapshot.Digest = cognitiveSnapshotDigest(situationSnapshot)
	recommendations := make([]cognitiverecommendation.CognitiveRecommendationSet, 0, len(situations))
	for _, situation := range situations {
		set, err := cognitiverecommendation.Plan(cognitiverecommendation.PlanInput{Situation: situation}, recommendationPolicy)
		if err != nil {
			r.metrics.add("recommendation_build_failures")
			return err
		}
		recommendations = append(recommendations, set)
	}
	sort.Slice(recommendations, func(i, j int) bool { return recommendations[i].EpisodeID < recommendations[j].EpisodeID })
	recommendationSnapshot := CognitiveRecommendationSnapshot{WorkflowRevision: state.Revision, SituationSnapshotDigest: situationSnapshot.Digest, RecommendationSets: recommendations, EpisodeIndex: indexRecommendationSets(recommendations)}
	recommendationSnapshot.Digest = recommendationSnapshotDigest(recommendationSnapshot)
	projection := CognitiveProjectionSnapshot{WorkflowRevision: state.Revision, Situations: situationSnapshot, Recommendations: recommendationSnapshot}
	projection.Digest = projectionSnapshotDigest(projection)
	r.mu.Lock()
	r.projection.snapshot = projection
	r.mu.Unlock()
	r.recordRecommendationMetrics(recommendations)
	r.metrics.addN("recommendation_build_duration_ns", uint64(time.Since(started).Nanoseconds()))
	return nil
}

func (r *Runtime) refreshCognitiveSituation(episodeID string) error {
	return r.refreshCognitiveProjection(episodeID)
}

func (r *Runtime) refreshCognitiveProjection(episodeID string) error {
	if r == nil || r.coordinator == nil {
		return nil
	}
	started := time.Now()
	state := r.coordinator.Snapshot()
	var previousSituation *cognitivesituation.CognitiveSituation
	var previousRecommendation *cognitiverecommendation.CognitiveRecommendationSet
	r.mu.RLock()
	projection := r.projection.snapshot.Clone()
	if index, ok := projection.Situations.EpisodeIndex[episodeID]; ok && index >= 0 && index < len(projection.Situations.Situations) {
		value := projection.Situations.Situations[index]
		previousSituation = &value
	}
	if index, ok := projection.Recommendations.EpisodeIndex[episodeID]; ok && index >= 0 && index < len(projection.Recommendations.RecommendationSets) {
		value := projection.Recommendations.RecommendationSets[index]
		previousRecommendation = &value
	}
	r.mu.RUnlock()
	currentSituation, err := cognitivesituation.Build(cognitivesituation.BuildInput{Workflow: state, EpisodeID: episodeID, ExpectedDepth: r.expectedSituationDepth(), Previous: previousSituation}, cognitivesituation.DefaultPolicy())
	if err != nil {
		r.metrics.add("recommendation_build_failures")
		return err
	}
	var situationDiff *cognitivesituation.CognitiveSituationDiff
	if previousSituation != nil {
		diff, diffErr := cognitivesituation.Compare(*previousSituation, currentSituation)
		if diffErr != nil {
			r.metrics.add("recommendation_build_failures")
			return diffErr
		}
		situationDiff = &diff
	}
	currentRecommendation, err := cognitiverecommendation.Plan(cognitiverecommendation.PlanInput{Situation: currentSituation, SituationDiff: situationDiff, Previous: previousRecommendation}, cognitiverecommendation.DefaultPolicy())
	if err != nil {
		r.metrics.add("recommendation_build_failures")
		return err
	}
	if index, ok := projection.Situations.EpisodeIndex[episodeID]; ok {
		projection.Situations.Situations[index] = currentSituation
	} else {
		projection.Situations.Situations = append(projection.Situations.Situations, currentSituation)
		sort.Slice(projection.Situations.Situations, func(i, j int) bool {
			return projection.Situations.Situations[i].EpisodeID < projection.Situations.Situations[j].EpisodeID
		})
	}
	projection.Situations.WorkflowRevision = state.Revision
	projection.Situations.EpisodeIndex = indexSituations(projection.Situations.Situations)
	projection.Situations.Digest = cognitiveSnapshotDigest(projection.Situations)
	if index, ok := projection.Recommendations.EpisodeIndex[episodeID]; ok {
		projection.Recommendations.RecommendationSets[index] = currentRecommendation
	} else {
		projection.Recommendations.RecommendationSets = append(projection.Recommendations.RecommendationSets, currentRecommendation)
		sort.Slice(projection.Recommendations.RecommendationSets, func(i, j int) bool {
			return projection.Recommendations.RecommendationSets[i].EpisodeID < projection.Recommendations.RecommendationSets[j].EpisodeID
		})
	}
	projection.Recommendations.WorkflowRevision = state.Revision
	projection.Recommendations.SituationSnapshotDigest = projection.Situations.Digest
	projection.Recommendations.EpisodeIndex = indexRecommendationSets(projection.Recommendations.RecommendationSets)
	projection.Recommendations.Digest = recommendationSnapshotDigest(projection.Recommendations)
	projection.WorkflowRevision = state.Revision
	projection.Digest = projectionSnapshotDigest(projection)
	r.mu.Lock()
	r.projection.snapshot = projection
	r.mu.Unlock()
	r.recordRecommendationMetrics([]cognitiverecommendation.CognitiveRecommendationSet{currentRecommendation})
	r.metrics.addN("recommendation_build_duration_ns", uint64(time.Since(started).Nanoseconds()))
	return nil
}

func indexSituations(values []cognitivesituation.CognitiveSituation) map[string]int {
	out := make(map[string]int, len(values))
	for index, value := range values {
		out[value.EpisodeID] = index
	}
	return out
}

func indexRecommendationSets(values []cognitiverecommendation.CognitiveRecommendationSet) map[string]int {
	out := make(map[string]int, len(values))
	for index, value := range values {
		out[value.EpisodeID] = index
	}
	return out
}

func cognitiveSnapshotDigest(value CognitiveSituationSnapshot) string {
	derived := cognitivesituation.CognitiveSituationSnapshot{WorkflowRevision: value.WorkflowRevision, Situations: value.Situations, EpisodeIndex: value.EpisodeIndex}
	return cognitivesituation.SnapshotFingerprint(derived)
}

func recommendationSnapshotDigest(value CognitiveRecommendationSnapshot) string {
	derived := cognitiverecommendation.CognitiveRecommendationSnapshot{WorkflowRevision: value.WorkflowRevision, SituationSnapshotDigest: value.SituationSnapshotDigest, RecommendationSets: value.RecommendationSets, EpisodeIndex: value.EpisodeIndex}
	return cognitiverecommendation.RecommendationSnapshotFingerprint(derived)
}

func projectionSnapshotDigest(value CognitiveProjectionSnapshot) string {
	return cognitiverecommendation.RecommendationSetFingerprint(cognitiverecommendation.CognitiveRecommendationSet{ID: "projection", SituationID: value.Situations.Digest, EpisodeID: "projection", SourceSituationFingerprint: value.Recommendations.Digest, Fingerprint: value.Recommendations.Digest})
}

func (r *Runtime) recordRecommendationMetrics(values []cognitiverecommendation.CognitiveRecommendationSet) {
	for _, set := range values {
		r.metrics.add("recommendation_sets_total")
		if set.Ambiguous {
			r.metrics.add("recommendations_ambiguous")
		}
		if set.HasCognitiveTransition {
			r.metrics.add("recommendations_transition_flags")
		}
		if set.PrimaryRecommendationID != "" {
			r.metrics.add("recommendation_primary_changes")
		}
		for _, value := range set.Recommendations {
			r.metrics.add("recommendations_total")
			if value.Status == cognitiverecommendation.RecommendationApplicable {
				r.metrics.add("recommendations_applicable")
			}
			if value.Kind == cognitiverecommendation.RecommendationAdditionalEvidence {
				r.metrics.add("recommendations_requesting_evidence")
			}
			if value.Status == cognitiverecommendation.RecommendationBlocked {
				r.metrics.add("recommendations_blocked")
			}
		}
	}
}

func (r *Runtime) CognitiveSituation(episodeID string) (cognitivesituation.CognitiveSituation, bool) {
	if r == nil {
		return cognitivesituation.CognitiveSituation{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	index, ok := r.projection.snapshot.Situations.EpisodeIndex[episodeID]
	if !ok || index < 0 || index >= len(r.projection.snapshot.Situations.Situations) {
		return cognitivesituation.CognitiveSituation{}, false
	}
	return r.projection.snapshot.Situations.Situations[index].Clone(), true
}

func (r *Runtime) CognitiveSituations() CognitiveSituationSnapshot {
	if r == nil {
		return CognitiveSituationSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.projection.snapshot.Situations.Clone()
}

func (r *Runtime) CognitiveRecommendation(episodeID string) (cognitiverecommendation.CognitiveRecommendationSet, bool) {
	if r == nil {
		return cognitiverecommendation.CognitiveRecommendationSet{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	index, ok := r.projection.snapshot.Recommendations.EpisodeIndex[episodeID]
	if !ok || index < 0 || index >= len(r.projection.snapshot.Recommendations.RecommendationSets) {
		return cognitiverecommendation.CognitiveRecommendationSet{}, false
	}
	return r.projection.snapshot.Recommendations.RecommendationSets[index].Clone(), true
}

func (r *Runtime) CognitiveRecommendations() CognitiveRecommendationSnapshot {
	if r == nil {
		return CognitiveRecommendationSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.projection.snapshot.Recommendations.Clone()
}
