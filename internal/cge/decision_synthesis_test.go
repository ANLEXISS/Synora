package cge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	contracts "synora/internal/engine/contracts"
	"synora/internal/engine/graph"
)

func synthesisInput(chainID, eventType string) CognitiveDecisionInput {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	return CognitiveDecisionInput{EventID: "event-protected", ObservedEventType: eventType, SituationID: "situation-protected", ChainID: chainID, CoreRevision: 7, Target: DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, DangerScore: .5, Confidence: .8, EvidenceRefs: []string{"event-protected"}, CreatedAt: now, ValidUntil: now.Add(time.Minute)}
}

func loadTestCriticalSeeds(t *testing.T) []contracts.CriticalSeed {
	_, file, _, _ := runtime.Caller(0)
	seeds, err := graph.LoadCriticalSeeds(filepath.Join(filepath.Dir(file), "../../configs/cge_critical_chains.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return seeds
}

func TestCriticalChainsFromYAMLAndDecisionSynthesis(t *testing.T) {
	selector := NewContractChainSelector(loadTestCriticalSeeds(t), nil, nil, nil)
	candidate, err := selector.SelectDecisionChain(context.Background(), synthesisInput("single_unknown", "vision.unknown"))
	if err != nil || candidate.Reference.ChainID != "single_unknown" || candidate.Source != ChainSourceCriticalSeed {
		t.Fatalf("candidate=%+v err=%v", candidate, err)
	}
	decision, err := (DefaultDecisionSynthesizer{}).SynthesizeDecision(context.Background(), synthesisInput("single_unknown", "vision.unknown"), candidate)
	if err != nil || decision.DecisionType != DecisionTypeNotify || decision.DesiredState != "suspicious" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	if len(decision.Constraints.ForbiddenActions) != 2 {
		t.Fatalf("forbidden constraints=%+v", decision.Constraints)
	}
	if len(decision.Constraints.RequiredInvariantRefs) != 7 {
		t.Fatalf("synthesized safety invariants=%v", decision.Constraints.RequiredInvariantRefs)
	}

	intrusionInput := synthesisInput("unknown_moves_inside", "vision.unknown")
	intrusion, err := selector.SelectDecisionChain(context.Background(), intrusionInput)
	if err != nil {
		t.Fatal(err)
	}
	if intrusion.ExpectedState != "intrusion" {
		t.Fatalf("intrusion candidate=%+v", intrusion)
	}
	intrusionDecision, err := (DefaultDecisionSynthesizer{}).SynthesizeDecision(context.Background(), intrusionInput, intrusion)
	if err != nil || intrusionDecision.DecisionType != DecisionTypeChangeMode || intrusionDecision.DesiredState != "intrusion" {
		t.Fatalf("intrusion decision=%+v err=%v", intrusionDecision, err)
	}
}

func TestInvariantIsNeverASelectedBusinessChain(t *testing.T) {
	input := synthesisInput("invariant", "vision.unknown")
	candidate := CognitiveChainCandidate{
		Reference:     ChainReference{ChainID: "invariant", Version: 1, Class: ChainClassInvariant, RevisionHash: "invariant-revision"},
		Source:        ChainSourceCriticalSeed,
		Status:        ChainStatusActive,
		SituationID:   input.SituationID,
		ExpectedState: "suspicious",
		DangerScore:   .5,
		Confidence:    .5,
		EvidenceRefs:  input.EvidenceRefs,
		Scope:         "critical/invariant",
	}
	if err := candidate.Validate(); !errors.Is(err, ErrInvalidChainCandidate) {
		t.Fatalf("invariant accepted as business candidate: %v", err)
	}
}

func TestLearnedChainMustBePersistedActiveBeforeSelection(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	registry := NewChainRegistry()
	critical := ChainReference{ChainID: "single_unknown", Version: 1, Class: ChainClassCritical, RevisionHash: "critical-revision"}
	learned := ChainReference{ChainID: "single_unknown", Version: 2, Class: ChainClassLearned, RevisionHash: "learned-revision"}
	if err := registry.Register(ChainVersion{Reference: critical, Status: ChainStatusActive, Scope: "home", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(ChainVersion{Reference: learned, Status: ChainStatusCandidate, Scope: "home", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	behavior := contracts.LearnedBehavior{ID: "single_unknown", ExpectedState: "suspicious", DangerScore: .9, Confidence: .95, Status: contracts.LearnedBehaviorApproved, Enabled: true, ProposedActions: []map[string]any{{"action": "notify_user_critical"}}}
	selector := NewContractChainSelector(loadTestCriticalSeeds(t), []contracts.LearnedBehavior{behavior}, nil, registry)
	input := synthesisInput("single_unknown", "vision.unknown")
	candidate, err := selector.SelectDecisionChain(context.Background(), input)
	if err != nil || candidate.Source != ChainSourceCriticalSeed {
		t.Fatalf("inactive learned candidate=%+v err=%v", candidate, err)
	}
	if _, err := registry.Promote("single_unknown", learned, PromotionEvidence{CandidateOccurrences: 10, ObservationWindow: time.Hour, CandidatePerformance: .95, ActivePerformance: .7, StableAfterRestart: true, RollbackAvailable: true, CandidateScope: "home", ActiveScope: "home"}, now.Add(time.Hour), ChainPromotionPolicy{MinimumOccurrences: 3, MinimumWindow: time.Hour, MinimumPerformanceGain: .1}); err != nil {
		t.Fatal(err)
	}
	candidate, err = selector.SelectDecisionChain(context.Background(), input)
	if err != nil || candidate.Source != ChainSourceLearnedBehavior || candidate.Status != ChainStatusActive {
		t.Fatalf("active learned candidate=%+v err=%v", candidate, err)
	}
}

func TestChainGovernanceStoreRecoversAndFailsClosedOnCorruption(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "chain-governance.ndjson")
	store, err := NewFileChainGovernanceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewChainRegistryWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	critical := ChainReference{ChainID: "chain", Version: 1, Class: ChainClassCritical, RevisionHash: "critical"}
	learned := ChainReference{ChainID: "chain", Version: 2, Class: ChainClassLearned, RevisionHash: "learned"}
	if err := registry.Register(ChainVersion{Reference: critical, Status: ChainStatusActive, Scope: "home", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(ChainVersion{Reference: learned, Status: ChainStatusCandidate, Scope: "home", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Promote("chain", learned, PromotionEvidence{CandidateOccurrences: 4, ObservationWindow: time.Hour, CandidatePerformance: .9, ActivePerformance: .7, StableAfterRestart: true, RollbackAvailable: true, CandidateScope: "home", ActiveScope: "home"}, now.Add(time.Hour), ChainPromotionPolicy{MinimumOccurrences: 3, MinimumWindow: time.Hour, MinimumPerformanceGain: .1}); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewChainRegistryWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if selected, ok := recovered.Select("chain"); !ok || selected.Class != ChainClassLearned {
		t.Fatalf("recovered selected=%+v ok=%v", selected, ok)
	}
	if err := store.Append(context.Background(), ChainGovernanceRecord{Operation: "bad", Chain: ChainVersion{Reference: learned, Status: ChainStatusActive, Scope: "home", CreatedAt: now}, RecordedAt: now}); !errors.Is(err, ErrInvalidPromotion) {
		t.Fatalf("invalid governance record accepted: %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := NewChainRegistryWithStore(store); !errors.Is(err, ErrInvalidPromotion) {
		t.Fatalf("corrupt governance store did not fail closed: %v", err)
	}
}

func TestChainGovernanceRecoveryUsesNewestActiveVersionForRollback(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "chain-governance.ndjson")
	store, err := NewFileChainGovernanceStore(path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewChainRegistryWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	first := ChainReference{ChainID: "chain", Version: 1, Class: ChainClassLearned, RevisionHash: "learned-1"}
	if err := registry.Register(ChainVersion{Reference: first, Status: ChainStatusCandidate, Scope: "home", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	active1, err := registry.Promote("chain", first, PromotionEvidence{CandidateOccurrences: 4, ObservationWindow: time.Hour, CandidatePerformance: .9, ActivePerformance: .7, StableAfterRestart: true, RollbackAvailable: true, CandidateScope: "home", ActiveScope: "home"}, now, ChainPromotionPolicy{MinimumOccurrences: 3, MinimumWindow: time.Hour, MinimumPerformanceGain: .1})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := NewChainRegistryWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	second := ChainReference{ChainID: "chain", Version: 4, Class: ChainClassLearned, RevisionHash: "learned-2"}
	if err := recovered.Register(ChainVersion{Reference: second, Status: ChainStatusCandidate, Scope: "home", CreatedAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	active2, err := recovered.Promote("chain", second, PromotionEvidence{CandidateOccurrences: 5, ObservationWindow: time.Hour, CandidatePerformance: .95, ActivePerformance: .7, StableAfterRestart: true, RollbackAvailable: true, CandidateScope: "home", ActiveScope: "home"}, now.Add(2*time.Hour), ChainPromotionPolicy{MinimumOccurrences: 3, MinimumWindow: time.Hour, MinimumPerformanceGain: .1})
	if err != nil {
		t.Fatal(err)
	}
	recoveredAgain, err := NewChainRegistryWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	if selected, ok := recoveredAgain.Select("chain"); !ok || selected != active2 {
		t.Fatalf("newest recovered active=%+v ok=%v expected=%+v", selected, ok, active2)
	}
	rolledBack, err := recoveredAgain.Rollback("chain", active1, now.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if selected, ok := recoveredAgain.Select("chain"); !ok || selected != rolledBack || selected.ChainID != active1.ChainID {
		t.Fatalf("rollback selected=%+v ok=%v rollback=%+v", selected, ok, rolledBack)
	}
}
