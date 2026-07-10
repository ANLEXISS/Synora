package graph

import (
	"os"
	"strconv"
)

const (
	defaultCGEMaxSequences           = 1000
	defaultCGEMaxTransitions         = 2000
	defaultCGEMaxBehaviors           = 500
	defaultCGEMaxEvidencePerSequence = 10
	defaultCGEMaxExamplesPerSequence = 5
	defaultCGEMaxEvidencePerBehavior = 20
	defaultCGEPublicSequencesLimit   = 20
	defaultCGEPublicTransitionsLimit = 30
	defaultCGEPublicBehaviorsLimit   = 20
)

var (
	CGEMaxSequences           = envInt("CGE_MAX_SEQUENCES", defaultCGEMaxSequences)
	CGEMaxTransitions         = envInt("CGE_MAX_TRANSITIONS", defaultCGEMaxTransitions)
	CGEMaxBehaviors           = envInt("CGE_MAX_BEHAVIORS", defaultCGEMaxBehaviors)
	CGEMaxEvidencePerSequence = envInt("CGE_MAX_EVIDENCE_PER_SEQUENCE", defaultCGEMaxEvidencePerSequence)
	CGEMaxExamplesPerSequence = envInt("CGE_MAX_EXAMPLES_PER_SEQUENCE", defaultCGEMaxExamplesPerSequence)
	CGEMaxEvidencePerBehavior = envInt("CGE_MAX_EVIDENCE_PER_BEHAVIOR", defaultCGEMaxEvidencePerBehavior)
)

func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
