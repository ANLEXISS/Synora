package shadowworkflow

import (
	"sort"

	"synora/internal/cge/cognitivesituation"
	"synora/internal/cge/durableworkflow"
)

type CognitiveSituationSnapshot struct {
	WorkflowRevision uint64
	Situations       []cognitivesituation.CognitiveSituation
	EpisodeIndex     map[string]int
	Digest           string
}

type cognitiveSituationCache struct {
	snapshot CognitiveSituationSnapshot
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
	policy := cognitivesituation.DefaultPolicy()
	values := make([]cognitivesituation.CognitiveSituation, 0, len(state.Episodes))
	for _, episode := range state.Episodes {
		value, err := cognitivesituation.Build(cognitivesituation.BuildInput{
			Workflow:      state,
			EpisodeID:     episode.EpisodeID,
			ExpectedDepth: r.expectedSituationDepth(),
		}, policy)
		if err != nil {
			return err
		}
		values = append(values, value.Clone())
	}
	sort.Slice(values, func(i, j int) bool { return values[i].EpisodeID < values[j].EpisodeID })
	snapshot := CognitiveSituationSnapshot{
		WorkflowRevision: state.Revision,
		Situations:       values,
		EpisodeIndex:     make(map[string]int, len(values)),
	}
	for index, value := range values {
		snapshot.EpisodeIndex[value.EpisodeID] = index
	}
	snapshot.Digest = cognitiveSnapshotDigest(snapshot)
	r.mu.Lock()
	r.situations.snapshot = snapshot
	r.mu.Unlock()
	return nil
}

func (r *Runtime) refreshCognitiveSituation(episodeID string) error {
	if r == nil || r.coordinator == nil {
		return nil
	}
	state := r.coordinator.Snapshot()
	var previous *cognitivesituation.CognitiveSituation
	r.mu.RLock()
	if index, ok := r.situations.snapshot.EpisodeIndex[episodeID]; ok && index >= 0 && index < len(r.situations.snapshot.Situations) {
		value := r.situations.snapshot.Situations[index]
		previous = &value
	}
	r.mu.RUnlock()
	value, err := cognitivesituation.Build(cognitivesituation.BuildInput{
		Workflow:      state,
		EpisodeID:     episodeID,
		ExpectedDepth: r.expectedSituationDepth(),
		Previous:      previous,
	}, cognitivesituation.DefaultPolicy())
	if err != nil {
		return err
	}
	r.mu.Lock()
	current := r.situations.snapshot.Clone()
	if index, ok := current.EpisodeIndex[episodeID]; ok {
		current.Situations[index] = value
	} else {
		current.Situations = append(current.Situations, value)
		sort.Slice(current.Situations, func(i, j int) bool { return current.Situations[i].EpisodeID < current.Situations[j].EpisodeID })
	}
	current.WorkflowRevision = state.Revision
	current.EpisodeIndex = make(map[string]int, len(current.Situations))
	for index, item := range current.Situations {
		current.EpisodeIndex[item.EpisodeID] = index
	}
	current.Digest = cognitiveSnapshotDigest(current)
	r.situations.snapshot = current
	r.mu.Unlock()
	return nil
}

func cognitiveSnapshotDigest(value CognitiveSituationSnapshot) string {
	derived := cognitivesituation.CognitiveSituationSnapshot{
		WorkflowRevision: value.WorkflowRevision,
		Situations:       value.Situations,
		EpisodeIndex:     value.EpisodeIndex,
	}
	return cognitivesituation.SnapshotFingerprint(derived)
}

func (r *Runtime) CognitiveSituation(episodeID string) (cognitivesituation.CognitiveSituation, bool) {
	if r == nil {
		return cognitivesituation.CognitiveSituation{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	index, ok := r.situations.snapshot.EpisodeIndex[episodeID]
	if !ok || index < 0 || index >= len(r.situations.snapshot.Situations) {
		return cognitivesituation.CognitiveSituation{}, false
	}
	return r.situations.snapshot.Situations[index].Clone(), true
}

func (r *Runtime) CognitiveSituations() CognitiveSituationSnapshot {
	if r == nil {
		return CognitiveSituationSnapshot{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.situations.snapshot.Clone()
}
