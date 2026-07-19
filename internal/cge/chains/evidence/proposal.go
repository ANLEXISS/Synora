package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"synora/internal/cge/chains"
)

// Decision is the only conclusion produced by the pure evaluator.
type Decision string

const (
	DecisionProposeSupport       Decision = "propose_support"
	DecisionProposeContradiction Decision = "propose_contradiction"
	DecisionProposeNeutral       Decision = "propose_neutral"
	DecisionInsufficientEvidence Decision = "insufficient_evidence"
	DecisionAmbiguous            Decision = "ambiguous"
	DecisionAlreadyEvaluated     Decision = "already_evaluated"
)

func (d Decision) Validate() error {
	switch d {
	case DecisionProposeSupport, DecisionProposeContradiction,
		DecisionProposeNeutral, DecisionInsufficientEvidence,
		DecisionAmbiguous, DecisionAlreadyEvaluated:
		return nil
	default:
		return fmt.Errorf("invalid evidence decision %q", d)
	}
}

// ContributionProposal is a detached bridge from pure evidence evaluation to
// the explicit contribution command. It performs no durable operation.
type ContributionProposal struct {
	ChainID        chains.ChainID
	SourceRevision uint64

	Contribution chains.ConfidenceContribution

	PolicyNamespace string
	PolicyVersion   string

	TargetObservationID string
	EvidenceFingerprint string
	ReasonCode          string
	Decision            Decision
}

func (p ContributionProposal) Validate() error {
	if _, err := chains.NewChainID(string(p.ChainID)); err != nil {
		return fmt.Errorf("%w: chain id: %v", ErrInvalidContributionProposal, err)
	}
	if p.SourceRevision == 0 {
		return fmt.Errorf("%w: source revision must be positive", ErrInvalidContributionProposal)
	}
	if err := p.Decision.Validate(); err != nil || (p.Decision != DecisionProposeSupport && p.Decision != DecisionProposeContradiction && p.Decision != DecisionProposeNeutral) {
		return fmt.Errorf("%w: decision must be contributive", ErrInvalidContributionProposal)
	}
	expectedKind := map[Decision]chains.ContributionKind{
		DecisionProposeSupport:       chains.ContributionSupport,
		DecisionProposeContradiction: chains.ContributionContradiction,
		DecisionProposeNeutral:       chains.ContributionNeutral,
	}[p.Decision]
	if strings.TrimSpace(p.PolicyNamespace) == "" || strings.ContainsAny(p.PolicyNamespace, "\r\n") || strings.TrimSpace(p.PolicyVersion) == "" || strings.ContainsAny(p.PolicyVersion, "\r\n") {
		return fmt.Errorf("%w: policy provenance is invalid", ErrInvalidContributionProposal)
	}
	if strings.TrimSpace(p.TargetObservationID) == "" || strings.ContainsAny(p.TargetObservationID, "\r\n") {
		return fmt.Errorf("%w: target observation is invalid", ErrInvalidContributionProposal)
	}
	if strings.TrimSpace(p.EvidenceFingerprint) == "" || strings.ContainsAny(p.EvidenceFingerprint, "\r\n") {
		return fmt.Errorf("%w: evidence fingerprint is invalid", ErrInvalidContributionProposal)
	}
	if err := p.Contribution.Validate(); err != nil {
		return fmt.Errorf("%w: contribution: %v", ErrInvalidContributionProposal, err)
	}
	if p.Contribution.Kind != expectedKind {
		return fmt.Errorf("%w: decision and contribution kind differ", ErrInvalidContributionProposal)
	}
	if p.Contribution.ID == "" || p.Contribution.Source == "" {
		return fmt.Errorf("%w: contribution identity is incomplete", ErrInvalidContributionProposal)
	}
	return nil
}

// Command returns a detached optimistic-concurrency command for explicit
// application by a caller. It does not invoke a coordinator.
func (p ContributionProposal) Command(mutation chains.MutationContext) (chains.AddContributionCommand, error) {
	if err := p.Validate(); err != nil {
		return chains.AddContributionCommand{}, err
	}
	command := chains.AddContributionCommand{
		ChainID:        p.ChainID,
		SourceRevision: p.SourceRevision,
		Contribution:   p.Contribution.Clone(),
		Mutation:       mutation,
	}
	if err := command.Validate(); err != nil {
		return chains.AddContributionCommand{}, fmt.Errorf("%w: command: %v", ErrInvalidContributionProposal, err)
	}
	return command.Clone(), nil
}

type fingerprintMaterial struct {
	Namespace string   `json:"namespace"`
	ChainID   string   `json:"chain_id"`
	TargetID  string   `json:"target_observation_id"`
	Context   []string `json:"context_observation_ids"`
}

func evidenceFingerprint(namespace string, chainID chains.ChainID, targetID string, contextIDs []string) (string, error) {
	material, err := json.Marshal(fingerprintMaterial{
		Namespace: namespace,
		ChainID:   string(chainID),
		TargetID:  targetID,
		Context:   append([]string(nil), contextIDs...),
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(material)
	encoded := hex.EncodeToString(digest[:])
	return "sha256:" + encoded, nil
}

func contributionID(fingerprint string) string {
	return "cge-evidence-" + strings.TrimPrefix(fingerprint, "sha256:")
}

func sourceFor(policy Policy) string { return "cge-evidence/" + policy.Version }

// ContributionIDForFingerprint exposes the stable identity of an evidence
// unit without exposing the evaluator's internal proposal implementation.
func ContributionIDForFingerprint(fingerprint string) string { return contributionID(fingerprint) }

// ContributionSource returns the immutable provenance source captured by an
// evidence evaluation.
func ContributionSource(policyVersion string) string { return "cge-evidence/" + policyVersion }

func proposalReason(decision Decision, support, contradiction int64, version string) string {
	label := string(decision)
	switch decision {
	case DecisionProposeSupport:
		label = "support"
	case DecisionProposeContradiction:
		label = "contradiction"
	case DecisionProposeNeutral:
		label = "neutral"
	}
	return fmt.Sprintf("evidence.%s support=%d contradiction=%d policy=%s", label, support, contradiction, version)
}

func sameContribution(left, right chains.ConfidenceContribution) bool {
	if left.ID != right.ID || left.Source != right.Source || left.Kind != right.Kind || left.Value != right.Value || left.Reason != right.Reason {
		return false
	}
	if len(left.ObservationIDs) != len(right.ObservationIDs) {
		return false
	}
	for i := range left.ObservationIDs {
		if left.ObservationIDs[i] != right.ObservationIDs[i] {
			return false
		}
	}
	return true
}
