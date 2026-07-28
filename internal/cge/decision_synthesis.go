package cge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	contracts "synora/internal/engine/contracts"
)

var (
	ErrNoDecisionChain       = errors.New("no_governed_decision_chain")
	ErrInvalidChainCandidate = errors.New("invalid_cognitive_chain_candidate")
)

type ChainSource string

const (
	ChainSourceCriticalSeed    ChainSource = "critical_seed"
	ChainSourceLearnedBehavior ChainSource = "learned_behavior"
	ChainSourceLearnedSequence ChainSource = "learned_sequence"
)

func (s ChainSource) Validate() error {
	switch s {
	case ChainSourceCriticalSeed, ChainSourceLearnedBehavior, ChainSourceLearnedSequence:
		return nil
	default:
		return ErrInvalidChainCandidate
	}
}

// CognitiveDecisionInput is the closed, detached boundary between the
// cognitive projection and decision synthesis. It contains identifiers and
// fingerprints, never raw observation payloads.
type CognitiveDecisionInput struct {
	EventID           string
	ObservedEventType string
	SituationID       string
	CognitiveState    string
	// ChainID is retained as a compatibility alias for a CGE-produced chain
	// in callers that predate autonomous matching. It is never populated from
	// the historical engine.
	ChainID           string
	HistoricalChainID string
	CognitiveChainID  string
	Situation         CognitiveSituationSnapshot
	CoreRevision      uint64
	TargetRevision    uint64
	Target            DecisionTarget
	DangerScore       float64
	Confidence        float64
	EvidenceRefs      []string
	CreatedAt         time.Time
	ValidUntil        time.Time
}

func (i CognitiveDecisionInput) Validate() error {
	for name, value := range map[string]string{"event id": i.EventID, "situation id": i.SituationID, "event type": i.ObservedEventType} {
		if strings.TrimSpace(value) == "" || len(value) > 256 {
			return fmt.Errorf("%w: %s", ErrInvalidChainCandidate, name)
		}
	}
	if err := i.Target.Validate(); err != nil {
		return err
	}
	if i.CoreRevision == 0 || i.CreatedAt.IsZero() || i.ValidUntil.IsZero() || !i.ValidUntil.After(i.CreatedAt) {
		return ErrInvalidChainCandidate
	}
	if !boundedUnitFloat(i.DangerScore) || !boundedUnitFloat(i.Confidence) {
		return ErrInvalidChainCandidate
	}
	if len(i.EvidenceRefs) == 0 || len(i.EvidenceRefs) > maxDecisionEvidenceRefs || validateStringSet(i.EvidenceRefs, 256) != nil {
		return ErrInvalidChainCandidate
	}
	return nil
}

// CognitiveChainCandidate adapts the existing CriticalSeed/Learned contracts
// into the one closed selection shape consumed by the synthesizer.
type CognitiveChainCandidate struct {
	Reference        ChainReference
	Source           ChainSource
	Status           ChainStatus
	SituationID      string
	ExpectedState    string
	DangerScore      float64
	Confidence       float64
	ProposedActions  []string
	ForbiddenActions []string
	EvidenceRefs     []string
	Scope            string
}

func (c CognitiveChainCandidate) Validate() error {
	if err := c.Reference.Validate(); err != nil {
		return err
	}
	if err := c.Source.Validate(); err != nil {
		return err
	}
	if err := c.Status.Validate(); err != nil {
		return err
	}
	if c.Status != ChainStatusActive {
		return ErrInvalidChainCandidate
	}
	if c.Source == ChainSourceCriticalSeed && c.Reference.Class != ChainClassCritical {
		return ErrInvalidChainCandidate
	}
	if c.Source != ChainSourceCriticalSeed && c.Reference.Class != ChainClassLearned {
		return ErrInvalidChainCandidate
	}
	if strings.TrimSpace(c.SituationID) == "" || strings.TrimSpace(c.Scope) == "" || len([]rune(c.SituationID)) > 256 || len([]rune(c.Scope)) > 256 || strings.ContainsAny(c.SituationID+c.Scope, "\r\n") {
		return ErrInvalidChainCandidate
	}
	if len(c.ExpectedState) > 64 || !validCognitiveExpectedState(c.ExpectedState) {
		return ErrInvalidChainCandidate
	}
	if !boundedUnitFloat(c.DangerScore) || !boundedUnitFloat(c.Confidence) {
		return ErrInvalidChainCandidate
	}
	if len(c.ProposedActions) > 16 || len(c.ForbiddenActions) > 16 || len(c.EvidenceRefs) == 0 || len(c.EvidenceRefs) > maxDecisionEvidenceRefs {
		return ErrInvalidChainCandidate
	}
	if err := validateStringSet(c.ProposedActions, 128); err != nil {
		return err
	}
	if err := validateStringSet(c.ForbiddenActions, 128); err != nil {
		return err
	}
	proposed := make(map[string]struct{}, len(c.ProposedActions))
	for _, action := range c.ProposedActions {
		proposed[action] = struct{}{}
	}
	for _, action := range c.ForbiddenActions {
		if _, ok := proposed[action]; ok {
			return ErrInvalidChainCandidate
		}
	}
	if err := validateStringSet(c.EvidenceRefs, 256); err != nil {
		return err
	}
	return nil
}

func validateStringSet(values []string, max int) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > max {
			return ErrInvalidChainCandidate
		}
		if _, ok := seen[value]; ok {
			return ErrInvalidChainCandidate
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validCognitiveExpectedState(value string) bool {
	switch value {
	case "idle", "activity", "suspicious", "intrusion", "break_in", "emergency":
		return true
	default:
		return false
	}
}

type ChainSelector interface {
	SelectDecisionChain(context.Context, CognitiveDecisionInput) (CognitiveChainCandidate, error)
}

// ContractChainSelector reads the existing engine contracts. It does not
// deserialize or create a second critical-chain format.
type ContractChainSelector struct {
	critical  []contracts.CriticalSeed
	behaviors []contracts.LearnedBehavior
	sequences []contracts.LearnedSequence
	registry  *ChainRegistry
	catalog   CognitiveChainCatalogProvider
	matcher   CognitiveChainMatcher
}

func (s *ContractChainSelector) SetContracts(critical []contracts.CriticalSeed, behaviors []contracts.LearnedBehavior, sequences []contracts.LearnedSequence) {
	if s == nil {
		return
	}
	s.critical = append([]contracts.CriticalSeed(nil), critical...)
	s.behaviors = append([]contracts.LearnedBehavior(nil), behaviors...)
	s.sequences = append([]contracts.LearnedSequence(nil), sequences...)
}

func NewContractChainSelector(critical []contracts.CriticalSeed, behaviors []contracts.LearnedBehavior, sequences []contracts.LearnedSequence, registry *ChainRegistry) *ContractChainSelector {
	return &ContractChainSelector{critical: append([]contracts.CriticalSeed(nil), critical...), behaviors: append([]contracts.LearnedBehavior(nil), behaviors...), sequences: append([]contracts.LearnedSequence(nil), sequences...), registry: registry, matcher: CriticalChainMatcher{}}
}

func (s *ContractChainSelector) SetCatalogProvider(provider CognitiveChainCatalogProvider) {
	if s != nil {
		s.catalog = provider
	}
}
func (s *ContractChainSelector) SetMatcher(matcher CognitiveChainMatcher) {
	if s != nil && matcher != nil {
		s.matcher = matcher
	}
}

func (s *ContractChainSelector) SelectDecisionChain(ctx context.Context, input CognitiveDecisionInput) (CognitiveChainCandidate, error) {
	if err := input.Validate(); err != nil {
		return CognitiveChainCandidate{}, err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return CognitiveChainCandidate{}, ctx.Err()
		default:
		}
	}
	if s == nil {
		return CognitiveChainCandidate{}, ErrNoDecisionChain
	}
	critical, behaviors, sequences := s.critical, s.behaviors, s.sequences
	if s.catalog != nil {
		snapshot, err := s.catalog.Snapshot(ctx)
		if err != nil {
			return CognitiveChainCandidate{}, err
		}
		critical, behaviors, sequences = snapshot.CriticalSeeds, snapshot.LearnedBehaviors, snapshot.LearnedSequences
	}
	// HistoricalChainID is intentionally not read here. The only acceptable
	// direct ID is the deprecated CGE alias, and autonomous runtime calls leave
	// both aliases empty so matching is always based on the situation history.
	if input.Situation.Validate() == nil && s.matcher != nil {
		matches, err := s.matcher.Match(ctx, input.Situation, critical)
		if err != nil {
			return CognitiveChainCandidate{}, err
		}
		for _, match := range matches {
			matchedInput := input
			matchedInput.EvidenceRefs = append([]string(nil), input.EvidenceRefs...)
			seenEvidence := make(map[string]struct{}, len(matchedInput.EvidenceRefs))
			for _, value := range matchedInput.EvidenceRefs {
				seenEvidence[value] = struct{}{}
			}
			for _, observationID := range match.MatchedObservationIDs {
				if len(matchedInput.EvidenceRefs) >= maxDecisionEvidenceRefs {
					break
				}
				if _, seen := seenEvidence[observationID]; seen {
					continue
				}
				seenEvidence[observationID] = struct{}{}
				matchedInput.EvidenceRefs = append(matchedInput.EvidenceRefs, observationID)
			}
			if s.registry != nil {
				if version, ok := s.registry.SelectVersion(match.ChainID); ok && version.Reference.Class == ChainClassLearned && version.Status == ChainStatusActive {
					if candidate, ok := learnedCandidateFrom(version, behaviors, sequences, matchedInput, match); ok {
						return candidate, nil
					}
				}
			}
			for _, seed := range critical {
				if seed.ID == match.ChainID && seed.Enabled && seed.DeletedAt == nil {
					return criticalCandidate(seed, matchedInput), nil
				}
			}
		}
		return CognitiveChainCandidate{}, ErrNoDecisionChain
	}
	// Compatibility for direct unit callers: ChainID means a CGE-owned chain
	// lookup, never a historical reference. This path does not inspect any
	// historical field and is not used by the runtime.
	chainID := input.CognitiveChainID
	if chainID == "" {
		chainID = input.ChainID
	}
	if chainID != "" {
		if s.registry != nil {
			if version, ok := s.registry.SelectVersion(chainID); ok && version.Reference.Class == ChainClassLearned && version.Status == ChainStatusActive {
				if candidate, ok := learnedCandidateFrom(version, behaviors, sequences, input, CognitiveChainMatch{ChainID: chainID, Scope: version.Scope}); ok {
					return candidate, nil
				}
			}
		}
		for _, seed := range critical {
			if seed.ID == chainID && seed.Enabled && seed.DeletedAt == nil {
				return criticalCandidate(seed, input), nil
			}
		}
	}
	return CognitiveChainCandidate{}, ErrNoDecisionChain
}

func learnedCandidateFrom(version ChainVersion, behaviors []contracts.LearnedBehavior, sequences []contracts.LearnedSequence, input CognitiveDecisionInput, match CognitiveChainMatch) (CognitiveChainCandidate, bool) {
	if version.Status != ChainStatusActive || version.Reference.Class != ChainClassLearned || version.Reference.Version == 0 || version.Evidence.InvariantViolations != 0 || (input.Target.Scope != "" && !scopeCompatible(version.Scope, input.Target.Scope)) {
		return CognitiveChainCandidate{}, false
	}
	if match.Scope != "" && !strings.HasPrefix(match.Scope, "critical/") && !scopeCompatible(version.Scope, match.Scope) {
		return CognitiveChainCandidate{}, false
	}
	for _, behavior := range behaviors {
		if behavior.ID != version.Reference.ChainID || !behavior.Enabled || behavior.Forgotten || behavior.Status != contracts.LearnedBehaviorApproved || !validCognitiveExpectedState(behavior.ExpectedState) {
			continue
		}
		actions, err := flattenActions(behavior.ProposedActions)
		if err != nil {
			return CognitiveChainCandidate{}, false
		}
		candidate := CognitiveChainCandidate{Reference: version.Reference, Source: ChainSourceLearnedBehavior, Status: ChainStatusActive, SituationID: input.SituationID, ExpectedState: behavior.ExpectedState, DangerScore: behavior.DangerScore, Confidence: behavior.Confidence, ProposedActions: actions, ForbiddenActions: append([]string(nil), behavior.ForbiddenActions...), EvidenceRefs: append([]string(nil), input.EvidenceRefs...), Scope: version.Scope}
		return candidate, candidate.Validate() == nil
	}
	for _, sequence := range sequences {
		if sequence.ID != version.Reference.ChainID || !validCognitiveExpectedState(sequence.ExpectedState) {
			continue
		}
		candidate := CognitiveChainCandidate{Reference: version.Reference, Source: ChainSourceLearnedSequence, Status: ChainStatusActive, SituationID: input.SituationID, ExpectedState: sequence.ExpectedState, DangerScore: sequence.DangerScore, Confidence: sequence.Confidence, EvidenceRefs: append([]string(nil), input.EvidenceRefs...), Scope: version.Scope}
		return candidate, candidate.Validate() == nil
	}
	return CognitiveChainCandidate{}, false
}

func revisionHash(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func criticalCandidate(seed contracts.CriticalSeed, input CognitiveDecisionInput) CognitiveChainCandidate {
	return CognitiveChainCandidate{Reference: ChainReference{ChainID: seed.ID, Version: uint64(maxInt(seed.Version, 1)), Class: ChainClassCritical, RevisionHash: revisionHash(seed)}, Source: ChainSourceCriticalSeed, Status: ChainStatusActive, SituationID: input.SituationID, ExpectedState: seed.ExpectedState, DangerScore: seed.DangerScore, Confidence: maxFloat(input.Confidence, 0.5), ProposedActions: append([]string(nil), seed.ProposedActions...), ForbiddenActions: append([]string(nil), seed.ForbiddenActions...), EvidenceRefs: append([]string(nil), input.EvidenceRefs...), Scope: "critical/" + seed.ID}
}

func (s *ContractChainSelector) learnedCandidate(version ChainVersion, input CognitiveDecisionInput) (CognitiveChainCandidate, bool) {
	ref := version.Reference
	if version.Status != ChainStatusActive || version.Evidence.InvariantViolations != 0 || !scopeCompatible(version.Scope, input.Target.Scope) {
		return CognitiveChainCandidate{}, false
	}
	for _, behavior := range s.behaviors {
		if behavior.ID == ref.ChainID && behavior.Status == contracts.LearnedBehaviorApproved && behavior.Enabled && !behavior.Forgotten && validCognitiveExpectedState(behavior.ExpectedState) {
			actions, err := flattenActions(behavior.ProposedActions)
			if err != nil {
				return CognitiveChainCandidate{}, false
			}
			candidate := CognitiveChainCandidate{Reference: ref, Source: ChainSourceLearnedBehavior, Status: ChainStatusActive, SituationID: input.SituationID, ExpectedState: behavior.ExpectedState, DangerScore: behavior.DangerScore, Confidence: behavior.Confidence, ProposedActions: actions, ForbiddenActions: append([]string(nil), behavior.ForbiddenActions...), EvidenceRefs: append([]string(nil), input.EvidenceRefs...), Scope: version.Scope}
			return candidate, candidate.Validate() == nil
		}
	}
	for _, sequence := range s.sequences {
		if sequence.ID == ref.ChainID && validCognitiveExpectedState(sequence.ExpectedState) {
			candidate := CognitiveChainCandidate{Reference: ref, Source: ChainSourceLearnedSequence, Status: ChainStatusActive, SituationID: input.SituationID, ExpectedState: sequence.ExpectedState, DangerScore: sequence.DangerScore, Confidence: sequence.Confidence, EvidenceRefs: append([]string(nil), input.EvidenceRefs...), Scope: version.Scope}
			return candidate, candidate.Validate() == nil
		}
	}
	return CognitiveChainCandidate{}, false
}

func scopeCompatible(chainScope, targetScope string) bool {
	chainScope, targetScope = strings.TrimSpace(chainScope), strings.TrimSpace(targetScope)
	if chainScope == "" || targetScope == "" {
		return true
	}
	return chainScope == targetScope || strings.HasPrefix(targetScope, chainScope+"/") || strings.HasPrefix(chainScope, targetScope+"/")
}

func flattenActions(values []map[string]any) ([]string, error) {
	var out []string
	for _, value := range values {
		action, ok := value["action"].(string)
		if !ok || strings.TrimSpace(action) == "" {
			return nil, ErrInvalidChainCandidate
		}
		out = append(out, action)
	}
	sort.Strings(out)
	if len(out) > 16 || validateStringSet(out, 128) != nil {
		return nil, ErrInvalidChainCandidate
	}
	return out, nil
}

func boundedUnitFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

type DecisionSynthesizer interface {
	SynthesizeDecision(context.Context, CognitiveDecisionInput, CognitiveChainCandidate) (DecisionEnvelope, error)
}

type DefaultDecisionSynthesizer struct{}

func (DefaultDecisionSynthesizer) SynthesizeDecision(ctx context.Context, input CognitiveDecisionInput, chain CognitiveChainCandidate) (DecisionEnvelope, error) {
	if err := input.Validate(); err != nil {
		return DecisionEnvelope{}, err
	}
	if err := chain.Validate(); err != nil {
		return DecisionEnvelope{}, err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return DecisionEnvelope{}, ctx.Err()
		default:
		}
	}
	decisionType, err := decisionTypeForState(chain.ExpectedState)
	if err != nil {
		return DecisionEnvelope{}, err
	}
	identityInput := input
	// HistoricalChainID is diagnostic comparison data. It must not affect the
	// cognitive decision identity or idempotency key.
	identityInput.HistoricalChainID = ""
	seed := struct {
		Input CognitiveDecisionInput
		Chain CognitiveChainCandidate
		Type  DecisionType
	}{identityInput, chain, decisionType}
	decisionID := "cge-decision-" + revisionHash(seed)
	constraints := DecisionConstraints{
		RequiresAuthorization: decisionType == DecisionTypeChangeMode,
		RequiresPhysicalLimit: decisionType == DecisionTypeChangeMode,
		RequiredStateRevision: input.TargetRevision,
		RequiredInvariantRefs: requiredDecisionInvariants(decisionType),
		ProposedActions:       append([]string(nil), chain.ProposedActions...),
		ForbiddenActions:      append([]string(nil), chain.ForbiddenActions...),
	}
	return DecisionEnvelope{SchemaVersion: DecisionEnvelopeSchemaVersion, DecisionID: decisionID, SituationID: input.SituationID, DecisionType: decisionType, DesiredState: chain.ExpectedState, Target: input.Target, Confidence: chain.Confidence, Priority: int(chain.DangerScore * 100), EvidenceRefs: append([]string(nil), chain.EvidenceRefs...), CriticalChainRef: criticalRef(chain), LearnedChainRef: learnedRef(chain), Constraints: constraints, CreatedAt: input.CreatedAt.UTC(), ValidUntil: input.ValidUntil.UTC(), IdempotencyKey: "cge-idempotency-" + revisionHash(seed)}, nil
}

// requiredDecisionInvariants is intentionally closed and independent of the
// selected business chain. The Safety Kernel owns these checks; a learned or
// critical chain can only add bounded intent and constraints.
func requiredDecisionInvariants(decisionType DecisionType) []string {
	values := []string{
		"safety.contract_valid",
		"safety.fresh_context",
		"safety.target_exists",
		"safety.idempotence",
		"safety.no_conflict",
		"safety.expiration",
		"safety.authority_mode",
	}
	if decisionType == DecisionTypeChangeMode {
		values = append(values, "safety.authorization", "safety.physical_limits")
	}
	return values
}

func decisionTypeForState(state string) (DecisionType, error) {
	switch state {
	case "idle", "activity":
		return DecisionTypeObserve, nil
	case "suspicious", "emergency":
		return DecisionTypeNotify, nil
	case "intrusion", "break_in":
		return DecisionTypeChangeMode, nil
	default:
		return "", ErrInvalidChainCandidate
	}
}

func criticalRef(candidate CognitiveChainCandidate) *ChainReference {
	if candidate.Source == ChainSourceCriticalSeed {
		ref := candidate.Reference
		return &ref
	}
	return nil
}
func learnedRef(candidate CognitiveChainCandidate) *ChainReference {
	if candidate.Source != ChainSourceCriticalSeed {
		ref := candidate.Reference
		return &ref
	}
	return nil
}

func maxFloat(value, fallback float64) float64 {
	if value < fallback {
		return fallback
	}
	return value
}
func maxInt(value, fallback int) int {
	if value < fallback {
		return fallback
	}
	return value
}
