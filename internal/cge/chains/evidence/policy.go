package evidence

import (
	"math"
	"strings"
	"time"

	"synora/internal/cge/chains"
)

// Policy is the versioned, integer-scored evidence policy. Namespace is part
// of the durable evidence identity; Version is provenance and source metadata
// for the precise rule set that produced a proposal.
type Policy struct {
	Namespace string
	Version   string

	ContextWindow          time.Duration
	MaxContextObservations int

	MinimumSupportScore       int64
	MinimumContradictionScore int64
	MinimumDecisionMargin     int64

	SameAssignedEntityScore     int64
	SameObservedEntityScore     int64
	AssignedEntityConflictScore int64
	ObservedEntityConflictScore int64

	SameSequenceScore   int64
	SameActivationScore int64
	SameTrackScore      int64
	SameDeviceScore     int64
	SameNodeScore       int64
	TemporalCloseScore  int64

	UncertainEvidencePenalty int64

	SupportValue       float64
	ContradictionValue float64
	NeutralValue       float64
}

const (
	defaultNamespace = "synora.cge.evidence"
	defaultVersion   = "evidence-v1"
	maxPolicyNameLen = 128
	maxContextLimit  = 4096
)

// DefaultPolicy is intentionally conservative and is not a claim about a
// product threshold. It is a deterministic starting policy for explicit
// evaluation and must be calibrated separately before being used as product
// policy.
func DefaultPolicy() Policy {
	return Policy{
		Namespace:                   defaultNamespace,
		Version:                     defaultVersion,
		ContextWindow:               5 * time.Minute,
		MaxContextObservations:      16,
		MinimumSupportScore:         80,
		MinimumContradictionScore:   90,
		MinimumDecisionMargin:       25,
		SameAssignedEntityScore:     60,
		SameObservedEntityScore:     45,
		AssignedEntityConflictScore: 75,
		ObservedEntityConflictScore: 50,
		SameSequenceScore:           30,
		SameActivationScore:         25,
		SameTrackScore:              25,
		SameDeviceScore:             10,
		SameNodeScore:               10,
		TemporalCloseScore:          20,
		UncertainEvidencePenalty:    25,
		SupportValue:                0.10,
		ContradictionValue:          0.15,
		NeutralValue:                0,
	}
}

// Validate rejects invalid and structurally unusable policies. In particular,
// every single scoring criterion remains below its corresponding threshold so
// one weak index cannot produce a contribution by itself.
func (p Policy) Validate() error {
	if strings.TrimSpace(p.Namespace) == "" || p.Namespace != strings.TrimSpace(p.Namespace) {
		return invalidPolicy("namespace", formatPolicyError("namespace must be non-empty and trimmed"))
	}
	if strings.TrimSpace(p.Version) == "" || p.Version != strings.TrimSpace(p.Version) {
		return invalidPolicy("version", formatPolicyError("version must be non-empty and trimmed"))
	}
	for name, value := range map[string]string{"namespace": p.Namespace, "version": p.Version} {
		if len([]rune(value)) > maxPolicyNameLen {
			return invalidPolicy(name, formatPolicyError("value exceeds %d characters", maxPolicyNameLen))
		}
		if strings.ContainsAny(value, "\r\n") {
			return invalidPolicy(name, formatPolicyError("value must be a single line"))
		}
	}
	if p.ContextWindow <= 0 {
		return invalidPolicy("context_window", formatPolicyError("context window must be positive"))
	}
	if p.MaxContextObservations <= 0 || p.MaxContextObservations > maxContextLimit {
		return invalidPolicy("max_context_observations", formatPolicyError("context limit must be between 1 and %d", maxContextLimit))
	}
	if p.MinimumSupportScore <= 0 || p.MinimumContradictionScore <= 0 {
		return invalidPolicy("threshold", formatPolicyError("support and contradiction thresholds must be positive"))
	}
	if p.MinimumDecisionMargin < 0 {
		return invalidPolicy("margin", formatPolicyError("decision margin must not be negative"))
	}

	scores := map[string]int64{
		"same_assigned_entity":       p.SameAssignedEntityScore,
		"same_observed_entity":       p.SameObservedEntityScore,
		"assigned_entity_conflict":   p.AssignedEntityConflictScore,
		"observed_entity_conflict":   p.ObservedEntityConflictScore,
		"same_sequence":              p.SameSequenceScore,
		"same_activation":            p.SameActivationScore,
		"same_track":                 p.SameTrackScore,
		"same_device":                p.SameDeviceScore,
		"same_node":                  p.SameNodeScore,
		"temporal_close":             p.TemporalCloseScore,
		"uncertain_evidence_penalty": p.UncertainEvidencePenalty,
	}
	for name, value := range scores {
		if value < 0 {
			return invalidPolicy(name, formatPolicyError("score must not be negative"))
		}
	}
	perContextSupport := p.SameObservedEntityScore + p.SameSequenceScore +
		p.SameActivationScore + p.SameTrackScore + p.SameDeviceScore +
		p.SameNodeScore + p.TemporalCloseScore
	supportMaximum := p.SameAssignedEntityScore + int64(p.MaxContextObservations)*perContextSupport
	contradictionMaximum := p.AssignedEntityConflictScore + int64(p.MaxContextObservations)*p.ObservedEntityConflictScore
	if supportMaximum < p.MinimumSupportScore {
		return invalidPolicy("support_threshold", formatPolicyError("threshold %d exceeds attainable score %d", p.MinimumSupportScore, supportMaximum))
	}
	if contradictionMaximum < p.MinimumContradictionScore {
		return invalidPolicy("contradiction_threshold", formatPolicyError("threshold %d exceeds attainable score %d", p.MinimumContradictionScore, contradictionMaximum))
	}
	for name, value := range map[string]int64{
		"same_assigned_entity": p.SameAssignedEntityScore,
		"same_observed_entity": p.SameObservedEntityScore,
		"same_sequence":        p.SameSequenceScore,
		"same_activation":      p.SameActivationScore,
		"same_track":           p.SameTrackScore,
		"same_device":          p.SameDeviceScore,
		"same_node":            p.SameNodeScore,
		"temporal_close":       p.TemporalCloseScore,
	} {
		if value >= p.MinimumSupportScore && value > 0 {
			return invalidPolicy(name, formatPolicyError("one criterion can reach the support threshold alone"))
		}
	}
	for name, value := range map[string]int64{
		"assigned_entity_conflict": p.AssignedEntityConflictScore,
		"observed_entity_conflict": p.ObservedEntityConflictScore,
	} {
		if value >= p.MinimumContradictionScore && value > 0 {
			return invalidPolicy(name, formatPolicyError("one criterion can reach the contradiction threshold alone"))
		}
	}
	if p.MinimumDecisionMargin > supportMaximum && p.MinimumDecisionMargin > contradictionMaximum {
		return invalidPolicy("margin", formatPolicyError("decision margin cannot exceed both attainable scores"))
	}
	for name, value := range map[string]float64{
		"support_value":       p.SupportValue,
		"contradiction_value": p.ContradictionValue,
		"neutral_value":       p.NeutralValue,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return invalidPolicy(name, formatPolicyError("contribution value must be between 0 and 1"))
		}
	}
	if p.NeutralValue != 0 {
		return invalidPolicy("neutral_value", formatPolicyError("this policy version requires a zero neutral value"))
	}
	return nil
}

// IsEvidenceEvaluationAllowed deliberately mirrors contribution admission for
// the states that may be explicitly examined. Replacement and invalidated
// chains are never evaluated by this package.
func IsEvidenceEvaluationAllowed(status chains.Status) bool {
	switch status {
	case chains.StatusCandidate, chains.StatusActive, chains.StatusConfirmed,
		chains.StatusDeclining, chains.StatusDormant, chains.StatusArchived,
		chains.StatusReactivated:
		return true
	default:
		return false
	}
}
