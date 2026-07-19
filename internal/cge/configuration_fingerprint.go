package cge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"synora/internal/cge/routines"
)

// CognitiveConfigurationFingerprint is the non-secret identity of the
// configured cognitive policies. It is operational metadata only.
type CognitiveConfigurationFingerprint struct {
	ContextSchemaVersion   string `json:"context_schema_version"`
	RoutinePolicyVersion   string `json:"routine_policy_version"`
	DeviationPolicyVersion string `json:"deviation_policy_version"`

	AssociationPolicyFingerprint string `json:"association_policy_fingerprint"`
	EvidencePolicyFingerprint    string `json:"evidence_policy_fingerprint"`
	ContextPolicyFingerprint     string `json:"context_policy_fingerprint"`
	RoutinePolicyFingerprint     string `json:"routine_policy_fingerprint"`
	DeviationPolicyFingerprint   string `json:"deviation_policy_fingerprint"`

	CombinedFingerprint string `json:"combined_fingerprint"`
}

// CognitiveConfigurationFingerprintFor returns a stable fingerprint from the
// actual ShadowConfig. No key, path, identity, or payload is included.
func CognitiveConfigurationFingerprintFor(config ShadowConfig) (CognitiveConfigurationFingerprint, error) {
	if err := config.Validate(); err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	associationFingerprint, err := fingerprintValue(config.AssociationPolicy)
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	evidenceFingerprint, err := fingerprintValue(config.EvidencePolicy)
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	contextPolicy, err := fingerprintValue(struct {
		SchemaVersion string
		Enabled       bool
		Timezone      string
		AllowPartial  bool
	}{"context-v1", config.Context.Enabled, config.Context.Timezone, config.Context.AllowPartial})
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	routinePolicy := routines.ExtractionPolicy{Namespace: "synora.cge.routines", Version: "routine-extraction-v1", TemporalBucketMinutes: config.Routines.TemporalBucketMinutes, AllowPartialContext: config.Routines.AllowPartialContext, MaxTransitionGap: config.Routines.MaxTransitionGap, RequireSameTopologyRevision: config.Routines.RequireSameTopologyRevision}
	routineFingerprint, err := fingerprintValue(struct {
		Enabled bool
		Policy  routines.ExtractionPolicy
	}{config.Routines.Enabled, routinePolicy})
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	deviationPolicyFingerprint, err := config.Deviation.Policy.Fingerprint()
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	deviationFingerprint, err := fingerprintValue(struct {
		Enabled                      bool
		RecentAssessmentLimit        int
		MaxAssessmentsPerObservation int
		PolicyFingerprint            string
	}{config.Deviation.Enabled, config.Deviation.RecentAssessmentLimit, config.Deviation.MaxAssessmentsPerObservation, deviationPolicyFingerprint})
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	runtimeFingerprint, err := fingerprintValue(config.Cognitive)
	if err != nil {
		return CognitiveConfigurationFingerprint{}, err
	}
	result := CognitiveConfigurationFingerprint{ContextSchemaVersion: "context-v1", RoutinePolicyVersion: routinePolicy.Version, DeviationPolicyVersion: config.Deviation.Policy.Version, AssociationPolicyFingerprint: associationFingerprint, EvidencePolicyFingerprint: evidenceFingerprint, ContextPolicyFingerprint: contextPolicy, RoutinePolicyFingerprint: routineFingerprint, DeviationPolicyFingerprint: deviationFingerprint}
	result.CombinedFingerprint, err = fingerprintValue(struct {
		ContextSchemaVersion                                        string
		RoutinePolicyVersion                                        string
		DeviationPolicyVersion                                      string
		Association, Evidence, Context, Routine, Deviation, Runtime string
	}{result.ContextSchemaVersion, result.RoutinePolicyVersion, result.DeviationPolicyVersion, result.AssociationPolicyFingerprint, result.EvidencePolicyFingerprint, result.ContextPolicyFingerprint, result.RoutinePolicyFingerprint, result.DeviationPolicyFingerprint, runtimeFingerprint})
	return result, err
}

func fingerprintValue(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
