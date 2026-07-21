package cognitivesituation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/durableworkflow"
)

func fingerprint(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(sum[:])
}

func situationFingerprint(value CognitiveSituation) string {
	copy := cloneSituation(value)
	copy.Fingerprint = ""
	copy.Revision = 0
	copy.PreviousSituationID = ""
	copy.PreviousFingerprint = ""
	copy.SourceFingerprints.AdvisoryRequests = append([]string(nil), copy.SourceFingerprints.AdvisoryRequests...)
	copy.SourceFingerprints.CapabilityMappings = append([]string(nil), copy.SourceFingerprints.CapabilityMappings...)
	copy.SourceFingerprints.AuthorizationAssessments = append([]string(nil), copy.SourceFingerprints.AuthorizationAssessments...)
	sort.Strings(copy.SourceFingerprints.AdvisoryRequests)
	sort.Strings(copy.SourceFingerprints.CapabilityMappings)
	sort.Strings(copy.SourceFingerprints.AuthorizationAssessments)
	sort.Slice(copy.Knowledge.LayerStates, func(i, j int) bool { return copy.Knowledge.LayerStates[i].Layer < copy.Knowledge.LayerStates[j].Layer })
	sort.Slice(copy.Hypotheses.Alternatives, func(i, j int) bool { return copy.Hypotheses.Alternatives[i].ID < copy.Hypotheses.Alternatives[j].ID })
	return fingerprint("cognitive-situation-v1:", copy)
}

func readinessFingerprint(value RecommendationReadiness) string {
	copy := cloneReadiness(value)
	copy.Fingerprint = ""
	sort.Slice(copy.RequiredFreshLayers, func(i, j int) bool { return copy.RequiredFreshLayers[i] < copy.RequiredFreshLayers[j] })
	sort.Slice(copy.MissingFreshLayers, func(i, j int) bool { return copy.MissingFreshLayers[i] < copy.MissingFreshLayers[j] })
	return fingerprint("cognitive-situation-readiness-v1:", copy)
}

func diffFingerprint(value CognitiveSituationDiff) string {
	copy := value
	copy.Fingerprint = ""
	sort.Strings(copy.ReasonCodes)
	return fingerprint("cognitive-situation-diff-v1:", copy)
}

func snapshotFingerprint(value CognitiveSituationSnapshot) string {
	copy := value.Clone()
	copy.Digest = ""
	sort.Slice(copy.Situations, func(i, j int) bool { return copy.Situations[i].EpisodeID < copy.Situations[j].EpisodeID })
	copy.EpisodeIndex = make(map[string]int, len(copy.Situations))
	for index, situation := range copy.Situations {
		copy.EpisodeIndex[situation.EpisodeID] = index
	}
	return fingerprint("cognitive-situation-snapshot-v1:", copy)
}

func SituationFingerprint(value CognitiveSituation) string        { return situationFingerprint(value) }
func ReadinessFingerprint(value RecommendationReadiness) string   { return readinessFingerprint(value) }
func DiffFingerprint(value CognitiveSituationDiff) string         { return diffFingerprint(value) }
func SnapshotFingerprint(value CognitiveSituationSnapshot) string { return snapshotFingerprint(value) }

func (s CognitiveSituation) Validate(policy Policy) error {
	if err := policy.Validate(); err != nil {
		return ErrInvalidPolicy
	}
	if s.ID == "" || s.EpisodeID == "" || !validPhase(s.Phase) || !validDepth(s.ExpectedDepth) {
		return ErrInvalidSituation
	}
	if s.WorkflowDigest == "" || s.Fingerprint == "" || s.Fingerprint != situationFingerprint(s) {
		return ErrInvalidSituation
	}
	if !s.Markers.NotADecision || !s.Markers.NotAProbability || !s.Markers.NotAuthorization ||
		!s.Markers.NotACommand || !s.Markers.NotAnAction || !s.Markers.NoSecurityMeaning ||
		!s.Markers.DerivedFromCommittedState {
		return ErrInvalidSituation
	}
	if err := validateKnowledge(s.Knowledge, policy); err != nil {
		return err
	}
	sourceCount := len(s.SourceFingerprints.AdvisoryRequests) + len(s.SourceFingerprints.CapabilityMappings) + len(s.SourceFingerprints.AuthorizationAssessments)
	for _, value := range []string{s.SourceFingerprints.Episode, s.SourceFingerprints.Facts, s.SourceFingerprints.Hypotheses, s.SourceFingerprints.Discrimination} {
		if value != "" {
			sourceCount++
		}
	}
	if sourceCount > policy.MaxSourceFingerprints {
		return ErrInvalidSituation
	}
	if len(s.Hypotheses.Alternatives) > policy.MaxHypothesisAlternatives || len(s.Evidence.MissingRequirementCodes) > policy.MaxReasonCodes {
		return ErrInvalidSituation
	}
	if err := validateReadiness(s.RecommendationReadiness, policy); err != nil {
		return err
	}
	return nil
}

func (s CognitiveSituationSnapshot) Validate(policy Policy) error {
	if err := policy.Validate(); err != nil {
		return ErrInvalidPolicy
	}
	if s.Digest == "" || s.Digest != snapshotFingerprint(s) {
		return ErrInvalidSituation
	}
	seen := map[string]struct{}{}
	if len(s.EpisodeIndex) != len(s.Situations) {
		return ErrInvalidSituation
	}
	for i, value := range s.Situations {
		if _, ok := seen[value.EpisodeID]; ok || s.EpisodeIndex[value.EpisodeID] != i {
			return ErrInvalidSituation
		}
		seen[value.EpisodeID] = struct{}{}
		if err := value.Validate(policy); err != nil {
			return err
		}
	}
	return nil
}

func validPhase(value CognitivePhase) bool {
	switch value {
	case PhaseObserving, PhaseBuilding, PhaseCoherent, PhaseAmbiguous,
		PhaseIncomplete, PhaseAwaitingEvidence, PhaseCapabilityUnavailable,
		PhaseAuthorizationConstrained, PhaseStale, PhaseInvalidated:
		return true
	default:
		return false
	}
}

func validateKnowledge(value KnowledgeSummary, policy Policy) error {
	if value.OverallCoveragePermille < 0 || value.OverallCoveragePermille > 1000 ||
		value.FreshLayers < 0 || value.StaleLayers < 0 || value.AbsentExpectedLayers < 0 ||
		value.InvalidatedLayers < 0 || len(value.LayerStates) > len(durableworkflow.Layers()) {
		return ErrInvalidKnowledgeSummary
	}
	if value.OverallCoveragePermille < 0 || value.OverallCoveragePermille > 1000 {
		return ErrInvalidKnowledgeSummary
	}
	return nil
}

func validateReadiness(value RecommendationReadiness, policy Policy) error {
	if value.Fingerprint == "" || value.Fingerprint != readinessFingerprint(value) || value.Ready && value.Status == ReadinessNotReady ||
		len(value.BlockingReasonCodes) > policy.MaxReasonCodes || len(value.SupportingReasonCodes) > policy.MaxReasonCodes {
		return ErrInvalidRecommendationReadiness
	}
	return nil
}
