package durableworkflow

import (
	"fmt"
	"sort"

	"synora/internal/cge/advisoryrequests"
	"synora/internal/cge/authorizationboundary"
	"synora/internal/cge/capabilitymapping"
	"synora/internal/cge/episodes"
	"synora/internal/cge/evidencediscrimination"
	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func ValidateWorkflowState(state WorkflowState) error {
	if err := ValidateLayerGraph(); err != nil {
		return err
	}
	if state.SchemaFingerprint != SchemaFingerprint() || state.PolicyFingerprint == "" {
		return ErrInvalidWorkflowState
	}
	if len(state.Episodes) > 100000 {
		return ErrInvalidWorkflowState
	}
	copy := state.Clone()
	if state.Digest == "" || WorkflowStateFingerprint(copy) != state.Digest {
		return ErrInvalidWorkflowState
	}
	seen := make(map[string]struct{}, len(state.Episodes))
	for i := range state.Episodes {
		if err := validateEpisodeState(state.Episodes[i]); err != nil {
			return err
		}
		if _, ok := seen[state.Episodes[i].EpisodeID]; ok {
			return fmt.Errorf("%w: duplicate episode", ErrInvalidWorkflowState)
		}
		seen[state.Episodes[i].EpisodeID] = struct{}{}
	}
	return nil
}

func validateEpisodeState(value EpisodeWorkflowState) error {
	if value.EpisodeID == "" || value.Fingerprint == "" || value.Fingerprint != EpisodeWorkflowStateFingerprint(value) {
		return ErrInvalidWorkflowState
	}
	for _, layer := range layerOrder {
		freshness, ok := value.Freshness[layer]
		if !ok || !validFreshness(freshness) {
			return fmt.Errorf("%w: freshness", ErrInvalidWorkflowState)
		}
	}
	if value.Episode != nil && string(value.Episode.ID) != value.EpisodeID {
		return fmt.Errorf("%w: episode id", ErrInvalidLineage)
	}
	if value.Episode != nil {
		if err := value.Episode.Validate(); err != nil {
			return fmt.Errorf("%w: episode", ErrInvalidLineage)
		}
		if value.Freshness[LayerEpisode] == FreshnessFresh && episodes.EpisodeFingerprint(*value.Episode) == "" {
			return ErrInvalidLineage
		}
	}
	if value.Facts != nil {
		if string(value.Facts.EpisodeID) != value.EpisodeID {
			return fmt.Errorf("%w: facts episode", ErrInvalidLineage)
		}
		if value.Facts.Fingerprint == "" || situationfacts.FactSetFingerprint(*value.Facts) != value.Facts.Fingerprint {
			return fmt.Errorf("%w: facts fingerprint", ErrLayerFingerprintMismatch)
		}
		if value.Freshness[LayerSituationFacts] == FreshnessFresh && (value.Episode == nil || value.Facts.EpisodeRevision != value.Episode.Revision) {
			return fmt.Errorf("%w: facts episode revision", ErrInvalidLineage)
		}
	}
	if value.Hypotheses != nil {
		if value.Hypotheses.EpisodeID != value.EpisodeID || value.Hypotheses.Fingerprint == "" || situationhypotheses.CompetingHypothesisSetFingerprint(*value.Hypotheses) != value.Hypotheses.Fingerprint {
			return fmt.Errorf("%w: hypotheses", ErrInvalidLineage)
		}
		if value.Freshness[LayerSituationHypotheses] == FreshnessFresh && (value.Facts == nil || value.Hypotheses.FactSetFingerprint != value.Facts.Fingerprint) {
			return fmt.Errorf("%w: hypothesis facts", ErrInvalidLineage)
		}
	}
	if value.Discrimination != nil {
		assessment := value.Discrimination
		if assessment.EpisodeID != value.EpisodeID || assessment.Fingerprint == "" || evidencediscrimination.AssessmentFingerprint(*assessment) != assessment.Fingerprint {
			return fmt.Errorf("%w: discrimination", ErrInvalidLineage)
		}
		if value.Freshness[LayerEvidenceDiscrimination] == FreshnessFresh && (value.Facts == nil || value.Hypotheses == nil || assessment.SourceFactSetFingerprint != value.Facts.Fingerprint || assessment.SourceHypothesisSetFingerprint != value.Hypotheses.Fingerprint) {
			return fmt.Errorf("%w: discrimination sources", ErrInvalidLineage)
		}
	}
	for _, request := range value.AdvisoryRequests {
		if request.EpisodeID != value.EpisodeID || request.ID == "" || request.Key == "" || request.Fingerprint == "" || advisoryrequests.AdvisoryRequestFingerprint(request) != request.Fingerprint {
			return fmt.Errorf("%w: advisory request", ErrInvalidLineage)
		}
		if value.Freshness[LayerAdvisoryRequests] == FreshnessFresh && (value.Discrimination == nil || request.SourceAssessmentFingerprint != value.Discrimination.Fingerprint) {
			return fmt.Errorf("%w: advisory source", ErrInvalidLineage)
		}
	}
	requestIDs := make(map[advisoryrequests.AdvisoryRequestID]struct{}, len(value.AdvisoryRequests))
	for _, request := range value.AdvisoryRequests {
		requestIDs[request.ID] = struct{}{}
	}
	for _, mapping := range value.CapabilityMappings {
		if mapping.EpisodeID != value.EpisodeID || mapping.RequestID == "" || mapping.Fingerprint == "" || capabilitymapping.CapabilityMappingAssessmentFingerprint(mapping) != mapping.Fingerprint {
			return fmt.Errorf("%w: capability mapping", ErrInvalidLineage)
		}
		if value.Freshness[LayerCapabilityMapping] == FreshnessFresh {
			if _, ok := requestIDs[mapping.RequestID]; !ok {
				return fmt.Errorf("%w: missing request", ErrMissingUpstreamLayer)
			}
			for _, request := range value.AdvisoryRequests {
				if request.ID == mapping.RequestID && mapping.SourceRequestFingerprint != request.Fingerprint {
					return fmt.Errorf("%w: mapping request fingerprint", ErrLayerFingerprintMismatch)
				}
			}
		}
	}
	mappingIDs := make(map[string]capabilitymapping.CapabilityMappingAssessment, len(value.CapabilityMappings))
	for _, mapping := range value.CapabilityMappings {
		mappingIDs[string(mapping.RequestID)] = mapping
	}
	for _, assessment := range value.AuthorizationAssessments {
		if assessment.RequestID == "" || assessment.Fingerprint == "" || authorizationboundary.AuthorizationBoundaryAssessmentFingerprint(assessment) != assessment.Fingerprint {
			return fmt.Errorf("%w: authorization assessment", ErrInvalidLineage)
		}
		if value.Freshness[LayerAuthorizationBoundary] == FreshnessFresh {
			mapping, ok := mappingIDs[assessment.RequestID]
			if !ok {
				return fmt.Errorf("%w: missing mapping", ErrMissingUpstreamLayer)
			}
			if assessment.SourceMappingAssessmentFingerprint != mapping.Fingerprint {
				return fmt.Errorf("%w: authorization mapping fingerprint", ErrLayerFingerprintMismatch)
			}
		}
	}
	if value.Freshness[LayerEpisode] == FreshnessFresh && value.Episode == nil {
		return ErrMissingUpstreamLayer
	}
	if value.Freshness[LayerSituationFacts] == FreshnessFresh && value.Facts == nil {
		return ErrMissingUpstreamLayer
	}
	if value.Freshness[LayerSituationHypotheses] == FreshnessFresh && value.Hypotheses == nil {
		return ErrMissingUpstreamLayer
	}
	if value.Freshness[LayerEvidenceDiscrimination] == FreshnessFresh && value.Discrimination == nil {
		return ErrMissingUpstreamLayer
	}
	return nil
}

func emptyFreshness() map[LayerKind]LayerFreshness {
	result := make(map[LayerKind]LayerFreshness, len(layerOrder))
	for _, layer := range layerOrder {
		result[layer] = FreshnessAbsent
	}
	return result
}

func sortWorkflowState(state *WorkflowState) {
	sort.Slice(state.Episodes, func(i, j int) bool { return state.Episodes[i].EpisodeID < state.Episodes[j].EpisodeID })
}

func findEpisode(state *WorkflowState, id string) (*EpisodeWorkflowState, int) {
	for i := range state.Episodes {
		if state.Episodes[i].EpisodeID == id {
			return &state.Episodes[i], i
		}
	}
	return nil, -1
}

func markDescendantsStale(value *EpisodeWorkflowState, from LayerKind) {
	index := 0
	for i, layer := range layerOrder {
		if layer == from {
			index = i
			break
		}
	}
	for _, layer := range layerOrder[index+1:] {
		if value.Freshness[layer] != FreshnessInvalidated {
			value.Freshness[layer] = FreshnessStale
		}
	}
}
