package durableworkflow

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func PlanTransaction(current WorkflowState, mutation WorkflowMutation, transactionID WorkflowTransactionID, sequence uint64, createdAt time.Time, policy Policy) (WorkflowTransaction, WorkflowState, error) {
	if err := policy.Validate(); err != nil {
		return WorkflowTransaction{}, WorkflowState{}, err
	}
	if err := ValidateLayerGraph(); err != nil {
		return WorkflowTransaction{}, WorkflowState{}, err
	}
	if err := ValidateWorkflowState(current); err != nil {
		return WorkflowTransaction{}, WorkflowState{}, err
	}
	if transactionID == "" || len([]rune(string(transactionID))) > 128 || createdAt.IsZero() || createdAt.Location() != time.UTC {
		return WorkflowTransaction{}, WorkflowState{}, ErrInvalidTransaction
	}
	if mutation.EpisodeID == "" || len([]rune(mutation.EpisodeID)) > 256 {
		return WorkflowTransaction{}, WorkflowState{}, ErrInvalidMutation
	}
	if mutation.SourceWorkflowRevision != current.Revision {
		return WorkflowTransaction{}, WorkflowState{}, ErrSourceRevisionConflict
	}
	if mutation.SourceWorkflowDigest != current.Digest {
		return WorkflowTransaction{}, WorkflowState{}, ErrSourceDigestConflict
	}
	if sequence != current.LastSequence+1 {
		return WorkflowTransaction{}, WorkflowState{}, ErrSequenceRegression
	}
	if err := validateMutationShape(mutation); err != nil {
		return WorkflowTransaction{}, WorkflowState{}, err
	}
	mutation = canonicalMutation(mutation)

	result := current.Clone()
	episode, episodeIndex := findEpisode(&result, mutation.EpisodeID)
	if episode == nil {
		if len(result.Episodes) >= policy.MaxEpisodes {
			return WorkflowTransaction{}, WorkflowState{}, ErrInvalidMutation
		}
		result.Episodes = append(result.Episodes, EpisodeWorkflowState{EpisodeID: mutation.EpisodeID, Freshness: emptyFreshness()})
		episodeIndex = len(result.Episodes) - 1
		episode = &result.Episodes[episodeIndex]
	}
	changed := make(map[LayerKind]bool, len(layerOrder))
	if mutation.Episode != nil {
		episode.Episode = cloneEpisode(mutation.Episode)
		changed[LayerEpisode] = true
	}
	if mutation.Facts != nil {
		episode.Facts = cloneFacts(mutation.Facts)
		changed[LayerSituationFacts] = true
	}
	if mutation.Hypotheses != nil {
		episode.Hypotheses = cloneHypotheses(mutation.Hypotheses)
		changed[LayerSituationHypotheses] = true
	}
	if mutation.Discrimination != nil {
		episode.Discrimination = cloneDiscrimination(mutation.Discrimination)
		changed[LayerEvidenceDiscrimination] = true
	}
	if mutation.ReplaceAdvisoryRequestsSet {
		episode.AdvisoryRequests = cloneRequests(mutation.ReplaceAdvisoryRequests)
		changed[LayerAdvisoryRequests] = true
	}
	if mutation.ReplaceCapabilityMappingsSet {
		episode.CapabilityMappings = cloneMappings(mutation.ReplaceCapabilityMappings)
		changed[LayerCapabilityMapping] = true
	}
	if mutation.ReplaceAuthorizationAssessmentsSet {
		episode.AuthorizationAssessments = cloneAuthorizations(mutation.ReplaceAuthorizationAssessments)
		changed[LayerAuthorizationBoundary] = true
	}
	for _, layer := range mutation.ExplicitInvalidations {
		episode.Freshness[layer] = FreshnessInvalidated
		changed[layer] = true
	}
	for layer := range changed {
		markDescendantsStale(episode, layer)
	}
	if err := validateFreshReplacements(*episode, mutation); err != nil {
		return WorkflowTransaction{}, WorkflowState{}, err
	}
	for _, layer := range layerOrder {
		if !changed[layer] {
			continue
		}
		if isInvalidated(mutation.ExplicitInvalidations, layer) {
			continue
		}
		episode.Freshness[layer] = FreshnessFresh
	}
	if mutation.Episode == nil && episode.Episode == nil {
		episode.Freshness[LayerEpisode] = FreshnessAbsent
	}
	if episode.Episode == nil {
		for _, layer := range layerOrder[1:] {
			if changed[layer] && !isInvalidated(mutation.ExplicitInvalidations, layer) {
				return WorkflowTransaction{}, WorkflowState{}, ErrMissingUpstreamLayer
			}
		}
	}
	episode.Revision++
	episode.Fingerprint = EpisodeWorkflowStateFingerprint(*episode)
	result.Episodes[episodeIndex] = episode.Clone()
	sortWorkflowState(&result)
	result.Revision = current.Revision + 1
	result.LastSequence = sequence
	result.SchemaFingerprint = SchemaFingerprint()
	result.PolicyFingerprint = policy.Fingerprint()
	result.Digest = WorkflowStateFingerprint(result)
	if err := ValidateWorkflowState(result); err != nil {
		return WorkflowTransaction{}, WorkflowState{}, err
	}
	tx := WorkflowTransaction{ID: transactionID, Sequence: sequence, CreatedAt: createdAt.UTC(), EpisodeID: mutation.EpisodeID, SourceWorkflowRevision: current.Revision, SourceWorkflowDigest: current.Digest, Mutation: mutation.Clone(), ResultWorkflowRevision: result.Revision, ResultWorkflowDigest: result.Digest, SchemaFingerprint: result.SchemaFingerprint, PolicyFingerprint: result.PolicyFingerprint}
	tx.Fingerprint = WorkflowTransactionFingerprint(tx)
	return tx, result, nil
}

func validateMutationShape(mutation WorkflowMutation) error {
	if !mutation.ReplaceAdvisoryRequestsSet && len(mutation.ReplaceAdvisoryRequests) != 0 || !mutation.ReplaceCapabilityMappingsSet && len(mutation.ReplaceCapabilityMappings) != 0 || !mutation.ReplaceAuthorizationAssessmentsSet && len(mutation.ReplaceAuthorizationAssessments) != 0 {
		return ErrInvalidMutation
	}
	seen := make(map[LayerKind]struct{}, len(mutation.ExplicitInvalidations))
	for _, layer := range mutation.ExplicitInvalidations {
		if !validLayer(layer) {
			return ErrInvalidLayer
		}
		if _, ok := seen[layer]; ok {
			return ErrInvalidMutation
		}
		seen[layer] = struct{}{}
	}
	if mutation.Episode != nil && isInvalidated(mutation.ExplicitInvalidations, LayerEpisode) || mutation.Facts != nil && isInvalidated(mutation.ExplicitInvalidations, LayerSituationFacts) || mutation.Hypotheses != nil && isInvalidated(mutation.ExplicitInvalidations, LayerSituationHypotheses) || mutation.Discrimination != nil && isInvalidated(mutation.ExplicitInvalidations, LayerEvidenceDiscrimination) || mutation.ReplaceAdvisoryRequestsSet && isInvalidated(mutation.ExplicitInvalidations, LayerAdvisoryRequests) || mutation.ReplaceCapabilityMappingsSet && isInvalidated(mutation.ExplicitInvalidations, LayerCapabilityMapping) || mutation.ReplaceAuthorizationAssessmentsSet && isInvalidated(mutation.ExplicitInvalidations, LayerAuthorizationBoundary) {
		return ErrInvalidMutation
	}
	if mutation.Episode != nil && string(mutation.Episode.ID) != mutation.EpisodeID || mutation.Facts != nil && string(mutation.Facts.EpisodeID) != mutation.EpisodeID || mutation.Hypotheses != nil && mutation.Hypotheses.EpisodeID != mutation.EpisodeID || mutation.Discrimination != nil && mutation.Discrimination.EpisodeID != mutation.EpisodeID {
		return ErrInvalidLineage
	}
	for _, reason := range mutation.ReasonCodes {
		if strings.TrimSpace(reason) != reason || reason == "" || len([]rune(reason)) > 128 {
			return ErrInvalidMutation
		}
	}
	return nil
}

func validateFreshReplacements(state EpisodeWorkflowState, mutation WorkflowMutation) error {
	if mutation.Episode != nil {
		if err := mutation.Episode.Validate(); err != nil {
			return fmt.Errorf("%w: episode", ErrInvalidLineage)
		}
	}
	if mutation.Facts != nil && (state.Episode == nil || state.Freshness[LayerEpisode] != FreshnessFresh && mutation.Episode == nil || mutation.Facts.EpisodeRevision != state.Episode.Revision) {
		return fmt.Errorf("%w: facts episode revision", ErrInvalidLineage)
	}
	if mutation.Hypotheses != nil {
		if state.Facts == nil || state.Freshness[LayerSituationFacts] != FreshnessFresh && mutation.Facts == nil || mutation.Hypotheses.FactSetFingerprint != state.Facts.Fingerprint {
			return fmt.Errorf("%w: hypotheses facts", ErrInvalidLineage)
		}
	}
	if mutation.Discrimination != nil {
		if state.Facts == nil || state.Hypotheses == nil || state.Freshness[LayerSituationFacts] != FreshnessFresh && mutation.Facts == nil || state.Freshness[LayerSituationHypotheses] != FreshnessFresh && mutation.Hypotheses == nil || mutation.Discrimination.SourceFactSetFingerprint != state.Facts.Fingerprint || mutation.Discrimination.SourceHypothesisSetFingerprint != state.Hypotheses.Fingerprint {
			return fmt.Errorf("%w: discrimination source", ErrInvalidLineage)
		}
	}
	if mutation.ReplaceAdvisoryRequestsSet {
		if state.Discrimination == nil || state.Freshness[LayerEvidenceDiscrimination] != FreshnessFresh && mutation.Discrimination == nil {
			return ErrMissingUpstreamLayer
		}
		for _, request := range state.AdvisoryRequests {
			if request.SourceAssessmentFingerprint != state.Discrimination.Fingerprint {
				return fmt.Errorf("%w: advisory source", ErrInvalidLineage)
			}
		}
	}
	if mutation.ReplaceCapabilityMappingsSet {
		if state.Freshness[LayerAdvisoryRequests] != FreshnessFresh && !mutation.ReplaceAdvisoryRequestsSet {
			return ErrLayerStale
		}
		requests := make(map[string]string, len(state.AdvisoryRequests))
		for _, request := range state.AdvisoryRequests {
			requests[string(request.ID)] = request.Fingerprint
		}
		for _, mapping := range state.CapabilityMappings {
			fp, ok := requests[string(mapping.RequestID)]
			if !ok || fp != mapping.SourceRequestFingerprint {
				return fmt.Errorf("%w: mapping request", ErrInvalidLineage)
			}
		}
	}
	if mutation.ReplaceAuthorizationAssessmentsSet {
		if state.Freshness[LayerCapabilityMapping] != FreshnessFresh && !mutation.ReplaceCapabilityMappingsSet {
			return ErrLayerStale
		}
		mappings := make(map[string]string, len(state.CapabilityMappings))
		for _, mapping := range state.CapabilityMappings {
			mappings[string(mapping.RequestID)] = mapping.Fingerprint
		}
		for _, assessment := range state.AuthorizationAssessments {
			fp, ok := mappings[assessment.RequestID]
			if !ok || fp != assessment.SourceMappingAssessmentFingerprint {
				return fmt.Errorf("%w: authorization mapping", ErrInvalidLineage)
			}
		}
	}
	return nil
}

func isInvalidated(values []LayerKind, target LayerKind) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func canonicalMutation(mutation WorkflowMutation) WorkflowMutation {
	out := mutation.Clone()
	sort.Strings(out.ReasonCodes)
	sort.Slice(out.ExplicitInvalidations, func(i, j int) bool { return out.ExplicitInvalidations[i] < out.ExplicitInvalidations[j] })
	sort.Slice(out.ReplaceAdvisoryRequests, func(i, j int) bool { return out.ReplaceAdvisoryRequests[i].ID < out.ReplaceAdvisoryRequests[j].ID })
	sort.Slice(out.ReplaceCapabilityMappings, func(i, j int) bool {
		return out.ReplaceCapabilityMappings[i].RequestID < out.ReplaceCapabilityMappings[j].RequestID
	})
	sort.Slice(out.ReplaceAuthorizationAssessments, func(i, j int) bool {
		return out.ReplaceAuthorizationAssessments[i].RequestID < out.ReplaceAuthorizationAssessments[j].RequestID
	})
	return out
}
