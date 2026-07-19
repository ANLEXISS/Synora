package chains

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func testLifecyclePolicy() LifecyclePolicy {
	return LifecyclePolicy{
		CandidateTTL:                   time.Hour,
		ActiveDeclineAfter:             2 * time.Hour,
		ConfirmedDeclineAfter:          4 * time.Hour,
		DecliningDormantAfter:          8 * time.Hour,
		DormantArchiveAfter:            16 * time.Hour,
		MinConfidenceToRemainActive:    0.4,
		MinConfidenceToRemainConfirmed: 0.7,
		MaxCandidateContradictions:     2,
	}
}

func policySnapshot(status Status, lastSeen time.Time) Snapshot {
	id := ChainID("policy-chain")
	return Snapshot{
		ID:                    id,
		Status:                status,
		FirstSeenAt:           lastSeen,
		LastSeenAt:            lastSeen,
		CurrentConfidence:     0.8,
		HistoricalReliability: 0.9,
		Revision:              1,
		History: []RevisionRecord{{
			ChainID:          id,
			Operation:        OperationChainCreated,
			PreviousRevision: 0,
			NewRevision:      1,
			At:               lastSeen,
			Actor:            "test",
			Reason:           "create chain",
			NewStatus:        StatusCandidate,
		}},
	}
}

func TestLifecyclePolicyValidation(t *testing.T) {
	if err := DefaultLifecyclePolicy().Validate(); err != nil {
		t.Fatalf("default policy rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*LifecyclePolicy)
	}{
		{"negative duration", func(p *LifecyclePolicy) { p.CandidateTTL = -time.Second }},
		{"zero duration", func(p *LifecyclePolicy) { p.ActiveDeclineAfter = 0 }},
		{"confidence above range", func(p *LifecyclePolicy) { p.MinConfidenceToRemainActive = 1.1 }},
		{"confirmed not after active", func(p *LifecyclePolicy) { p.ConfirmedDeclineAfter = 2 * time.Hour }},
		{"confirmed confidence not above active", func(p *LifecyclePolicy) { p.MinConfidenceToRemainConfirmed = 0.4 }},
		{"zero contradiction threshold", func(p *LifecyclePolicy) { p.MaxCandidateContradictions = 0 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			policy := testLifecyclePolicy()
			test.edit(&policy)
			if err := policy.Validate(); err == nil || !errors.Is(err, ErrInvalidLifecyclePolicy) {
				t.Fatalf("expected invalid policy error, got %v", err)
			}
		})
	}

	invalid := testLifecyclePolicy()
	invalid.MinConfidenceToRemainActive = -0.1
	if _, err := EvaluateLifecycle(policySnapshot(StatusActive, chainTestBase), chainTestBase, invalid); err == nil {
		t.Fatal("invalid policy must prevent evaluation")
	}
	independent := testLifecyclePolicy()
	independent.CandidateTTL = 12 * time.Hour
	independent.DecliningDormantAfter = time.Hour
	independent.DormantArchiveAfter = time.Minute
	if err := independent.Validate(); err != nil {
		t.Fatalf("independent temporal anchors should be valid: %v", err)
	}
}

func TestCandidateLifecycleEvaluationPrioritizesContradictions(t *testing.T) {
	policy := testLifecyclePolicy()
	fresh, err := EvaluateLifecycle(policySnapshot(StatusCandidate, chainTestBase), chainTestBase.Add(policy.CandidateTTL-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("fresh candidate evaluation: %v", err)
	}
	if fresh.Proposal != nil || fresh.Reason != LifecycleEvaluationNoTransition {
		t.Fatalf("candidate at exact TTL should remain unchanged: %#v", fresh)
	}

	expired, err := EvaluateLifecycle(policySnapshot(StatusCandidate, chainTestBase), chainTestBase.Add(policy.CandidateTTL), policy)
	if err != nil {
		t.Fatalf("expired candidate evaluation: %v", err)
	}
	assertProposal(t, expired, StatusCandidate, StatusInvalidated, ReasonCandidateTTLExpired)

	contradictory := policySnapshot(StatusCandidate, chainTestBase)
	contradictory.ContradictionCount = policy.MaxCandidateContradictions
	evaluation, err := EvaluateLifecycle(contradictory, chainTestBase.Add(policy.CandidateTTL+time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("contradictory candidate evaluation: %v", err)
	}
	assertProposal(t, evaluation, StatusCandidate, StatusInvalidated, ReasonCandidateTooManyContradictions)
	if evaluation.Proposal.SupportingFacts[0].Name != "contradiction_count" {
		t.Fatalf("contradiction priority facts = %#v", evaluation.Proposal.SupportingFacts)
	}
}

func TestCandidateObservationRefreshesObservationAnchor(t *testing.T) {
	policy := testLifecyclePolicy()
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("candidate-event", "vision.motion", chainTestBase.Add(2*time.Hour), "entry"), mutation(1, "add candidate evidence")); err != nil {
		t.Fatalf("add candidate observation: %v", err)
	}
	snapshot := chain.Snapshot()
	evaluation, err := EvaluateLifecycle(snapshot, chainTestBase.Add(2*time.Hour).Add(policy.CandidateTTL-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("evaluate refreshed candidate: %v", err)
	}
	if evaluation.Proposal != nil || evaluation.InactiveFor != policy.CandidateTTL-time.Nanosecond {
		t.Fatalf("new observation did not refresh candidate inactivity: %#v", evaluation)
	}
}

func TestActiveLifecycleEvaluationPrioritizesConfidence(t *testing.T) {
	policy := testLifecyclePolicy()
	fresh, err := EvaluateLifecycle(policySnapshot(StatusActive, chainTestBase), chainTestBase.Add(policy.ActiveDeclineAfter-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("fresh active evaluation: %v", err)
	}
	if fresh.Proposal != nil {
		t.Fatalf("active at exact inactivity threshold should remain unchanged: %#v", fresh.Proposal)
	}

	inactive, err := EvaluateLifecycle(policySnapshot(StatusActive, chainTestBase), chainTestBase.Add(policy.ActiveDeclineAfter), policy)
	if err != nil {
		t.Fatalf("inactive active evaluation: %v", err)
	}
	assertProposal(t, inactive, StatusActive, StatusDeclining, ReasonActiveInactive)

	lowConfidence := policySnapshot(StatusActive, chainTestBase)
	lowConfidence.CurrentConfidence = policy.MinConfidenceToRemainActive - 0.01
	evaluation, err := EvaluateLifecycle(lowConfidence, chainTestBase.Add(policy.ActiveDeclineAfter), policy)
	if err != nil {
		t.Fatalf("low-confidence active evaluation: %v", err)
	}
	assertProposal(t, evaluation, StatusActive, StatusDeclining, ReasonActiveConfidenceBelowThreshold)
}

func TestConfirmedLifecycleEvaluationUsesConfidenceAndInactivity(t *testing.T) {
	policy := testLifecyclePolicy()
	fresh, err := EvaluateLifecycle(policySnapshot(StatusConfirmed, chainTestBase), chainTestBase.Add(policy.ConfirmedDeclineAfter-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("fresh confirmed evaluation: %v", err)
	}
	if fresh.Proposal != nil {
		t.Fatalf("confirmed at exact inactivity threshold should remain unchanged: %#v", fresh.Proposal)
	}

	inactive, err := EvaluateLifecycle(policySnapshot(StatusConfirmed, chainTestBase), chainTestBase.Add(policy.ConfirmedDeclineAfter), policy)
	if err != nil {
		t.Fatalf("inactive confirmed evaluation: %v", err)
	}
	assertProposal(t, inactive, StatusConfirmed, StatusDeclining, ReasonConfirmedInactive)

	lowConfidence := policySnapshot(StatusConfirmed, chainTestBase)
	lowConfidence.CurrentConfidence = policy.MinConfidenceToRemainConfirmed - 0.01
	evaluation, err := EvaluateLifecycle(lowConfidence, chainTestBase, policy)
	if err != nil {
		t.Fatalf("low-confidence confirmed evaluation: %v", err)
	}
	assertProposal(t, evaluation, StatusConfirmed, StatusDeclining, ReasonConfirmedConfidenceBelowThreshold)
	if evaluation.Proposal.SupportingFacts[len(evaluation.Proposal.SupportingFacts)-1].Name != "historical_reliability" {
		t.Fatalf("historical reliability was not explained: %#v", evaluation.Proposal.SupportingFacts)
	}
}

func TestDecliningDormantAndDormantArchivedEvaluation(t *testing.T) {
	policy := testLifecyclePolicy()
	declining, err := EvaluateLifecycle(policySnapshot(StatusDeclining, chainTestBase), chainTestBase.Add(policy.DecliningDormantAfter-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("recent declining evaluation: %v", err)
	}
	if declining.Proposal != nil {
		t.Fatalf("declining at exact threshold should remain unchanged: %#v", declining.Proposal)
	}
	declining, err = EvaluateLifecycle(policySnapshot(StatusDeclining, chainTestBase), chainTestBase.Add(policy.DecliningDormantAfter), policy)
	if err != nil {
		t.Fatalf("dormant declining evaluation: %v", err)
	}
	assertProposal(t, declining, StatusDeclining, StatusDormant, ReasonDecliningInactive)

	dormant, err := EvaluateLifecycle(policySnapshot(StatusDormant, chainTestBase), chainTestBase.Add(policy.DormantArchiveAfter-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("retained dormant evaluation: %v", err)
	}
	if dormant.Proposal != nil {
		t.Fatalf("dormant at exact retention threshold should remain unchanged: %#v", dormant.Proposal)
	}
	dormant, err = EvaluateLifecycle(policySnapshot(StatusDormant, chainTestBase), chainTestBase.Add(policy.DormantArchiveAfter), policy)
	if err != nil {
		t.Fatalf("archive dormant evaluation: %v", err)
	}
	assertProposal(t, dormant, StatusDormant, StatusArchived, ReasonDormantRetentionExpired)
}

func TestLifecycleEvaluationExcludesTerminalAndArchivedStatuses(t *testing.T) {
	policy := testLifecyclePolicy()
	for _, status := range []Status{StatusArchived, StatusMerged, StatusSplit, StatusInvalidated} {
		evaluation, err := EvaluateLifecycle(policySnapshot(status, chainTestBase), chainTestBase.Add(365*24*time.Hour), policy)
		if err != nil {
			t.Fatalf("evaluate %s: %v", status, err)
		}
		if evaluation.Proposal != nil || evaluation.Reason != LifecycleEvaluationStatusExcluded {
			t.Fatalf("status %s unexpectedly proposed a transition: %#v", status, evaluation)
		}
	}
}

func TestReactivatedLifecycleEvaluationIsConservative(t *testing.T) {
	policy := testLifecyclePolicy()
	fresh, err := EvaluateLifecycle(policySnapshot(StatusReactivated, chainTestBase), chainTestBase.Add(365*24*time.Hour), policy)
	if err != nil {
		t.Fatalf("reactivated evaluation: %v", err)
	}
	if fresh.Proposal == nil || fresh.Proposal.To != StatusDeclining || fresh.Proposal.ReasonCode != ReasonReactivatedInactive {
		t.Fatalf("reactivated inactivity should only propose declining: %#v", fresh.Proposal)
	}

	current, err := EvaluateLifecycle(policySnapshot(StatusReactivated, chainTestBase), chainTestBase.Add(policy.ActiveDeclineAfter-time.Nanosecond), policy)
	if err != nil {
		t.Fatalf("fresh reactivated evaluation: %v", err)
	}
	if current.Proposal != nil {
		t.Fatalf("reactivated chain should not be promoted automatically: %#v", current.Proposal)
	}
}

func TestLifecycleEvaluationIsPureDeterministicAndConcurrent(t *testing.T) {
	policy := testLifecyclePolicy()
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("policy-event", "vision.motion", chainTestBase, "entry"), mutation(1, "add policy observation")); err != nil {
		t.Fatalf("add observation: %v", err)
	}
	if err := chain.SetStatus(StatusActive, mutation(2, "activate policy chain")); err != nil {
		t.Fatalf("activate policy chain: %v", err)
	}
	before := chain.Snapshot()
	evaluatedAt := chainTestBase.Add(policy.ActiveDeclineAfter + time.Second)
	want, err := EvaluateLifecycle(before, evaluatedAt, policy)
	if err != nil {
		t.Fatalf("first evaluation: %v", err)
	}
	got, err := EvaluateLifecycle(before, evaluatedAt, policy)
	if err != nil {
		t.Fatalf("second evaluation: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("evaluation is not deterministic: want=%#v got=%#v", want, got)
	}
	if want.Proposal == nil || !CanTransition(want.Proposal.From, want.Proposal.To) {
		t.Fatalf("proposal does not satisfy transition policy: %#v", want.Proposal)
	}
	want.Proposal.SupportingFacts[0].Value = "mutated-outside"
	third, err := EvaluateLifecycle(before, evaluatedAt, policy)
	if err != nil {
		t.Fatalf("third evaluation: %v", err)
	}
	if third.Proposal.SupportingFacts[0].Value == "mutated-outside" {
		t.Fatal("proposal facts were shared between evaluations")
	}
	if after := chain.Snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatalf("lifecycle evaluation mutated source chain: before=%#v after=%#v", before, after)
	}

	var wait sync.WaitGroup
	for i := 0; i < 16; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, evaluationErr := EvaluateLifecycle(before, evaluatedAt, policy)
			if evaluationErr != nil || !reflect.DeepEqual(result, third) {
				t.Errorf("concurrent evaluation differs: result=%#v err=%v", result, evaluationErr)
			}
		}()
	}
	wait.Wait()
}

func TestLifecycleEvaluationRejectsPastDateAndUsesCreationTimeForEmptyChain(t *testing.T) {
	policy := testLifecyclePolicy()
	lastSeen := chainTestBase.Add(time.Hour)
	if _, err := EvaluateLifecycle(policySnapshot(StatusActive, lastSeen), lastSeen.Add(-time.Nanosecond), policy); err == nil {
		t.Fatal("expected evaluation before last observation to be rejected")
	}

	empty := policySnapshot(StatusCandidate, time.Time{})
	empty.History = []RevisionRecord{{Operation: OperationChainCreated, At: chainTestBase}}
	evaluation, err := EvaluateLifecycle(empty, chainTestBase.Add(policy.CandidateTTL), policy)
	if err != nil {
		t.Fatalf("empty chain creation-time evaluation: %v", err)
	}
	assertProposal(t, evaluation, StatusCandidate, StatusInvalidated, ReasonCandidateTTLExpired)
}

func TestSnapshotTemporalAnchorsAreDerivedFromHistory(t *testing.T) {
	chain := mustNewChain(t)
	created := chain.Snapshot()
	if !created.CreatedAt().Equal(chainTestBase) || !created.StatusChangedAt().Equal(chainTestBase) || !created.StatusSince().Equal(chainTestBase) {
		t.Fatalf("initial anchors = created=%s status=%s since=%s", created.CreatedAt(), created.StatusChangedAt(), created.StatusSince())
	}
	if err := chain.AddObservation(observation("anchor-event", "vision.motion", chainTestBase.Add(time.Hour), "entry"), mutation(1, "add anchor observation")); err != nil {
		t.Fatalf("add anchor observation: %v", err)
	}
	withoutStatusChange := chain.Snapshot()
	if !withoutStatusChange.StatusChangedAt().Equal(chainTestBase) || !withoutStatusChange.LastSeenAt.Equal(chainTestBase.Add(time.Hour)) {
		t.Fatalf("observation changed status anchor: %#v", withoutStatusChange)
	}
	if err := chain.SetStatus(StatusActive, mutation(2, "activate anchor chain")); err != nil {
		t.Fatalf("activate anchor chain: %v", err)
	}
	active := chain.Snapshot()
	if !active.StatusChangedAt().Equal(chainTestBase.Add(2 * time.Second)) {
		t.Fatalf("active status anchor = %s", active.StatusChangedAt())
	}
	if err := chain.SetConfidence(0.5, mutation(3, "update non-status anchor")); err != nil {
		t.Fatalf("update confidence: %v", err)
	}
	withoutStatusChange = chain.Snapshot()
	if !withoutStatusChange.StatusChangedAt().Equal(chainTestBase.Add(2 * time.Second)) {
		t.Fatalf("non-status mutation changed status anchor: %s", withoutStatusChange.StatusChangedAt())
	}
	if err := chain.SetStatus(StatusDeclining, mutation(4, "decline anchor chain")); err != nil {
		t.Fatalf("decline anchor chain: %v", err)
	}
	if err := chain.SetStatus(StatusDormant, mutation(5, "make anchor dormant")); err != nil {
		t.Fatalf("make anchor dormant: %v", err)
	}
	if err := chain.SetStatus(StatusArchived, mutation(6, "archive anchor chain")); err != nil {
		t.Fatalf("archive anchor chain: %v", err)
	}
	archived := chain.Snapshot()
	if !archived.StatusChangedAt().Equal(chainTestBase.Add(6 * time.Second)) {
		t.Fatalf("archive status anchor = %s", archived.StatusChangedAt())
	}
	if err := chain.SetStatus(StatusReactivated, mutation(7, "reactivate anchor chain")); err != nil {
		t.Fatalf("reactivate anchor chain: %v", err)
	}
	reactivated := chain.Snapshot()
	if !reactivated.StatusChangedAt().Equal(chainTestBase.Add(7 * time.Second)) {
		t.Fatalf("reactivation status anchor = %s", reactivated.StatusChangedAt())
	}

	history := reactivated.History
	history[0].At = history[0].At.Add(24 * time.Hour)
	if !chain.Snapshot().CreatedAt().Equal(chainTestBase) {
		t.Fatal("history mutation changed the chain creation anchor")
	}
}

func TestLifecycleEvaluationExposesThreeDurations(t *testing.T) {
	policy := testLifecyclePolicy()
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("duration-event", "vision.motion", chainTestBase.Add(time.Hour), "entry"), mutation(1, "add duration observation")); err != nil {
		t.Fatalf("add duration observation: %v", err)
	}
	if err := chain.SetStatus(StatusActive, mutation(2, "activate duration chain")); err != nil {
		t.Fatalf("activate duration chain: %v", err)
	}
	evaluatedAt := chainTestBase.Add(10 * time.Hour)
	evaluation, err := EvaluateLifecycle(chain.Snapshot(), evaluatedAt, policy)
	if err != nil {
		t.Fatalf("evaluate durations: %v", err)
	}
	if evaluation.Age != 10*time.Hour || evaluation.InactiveFor != 9*time.Hour || evaluation.InCurrentStatusFor != 10*time.Hour-2*time.Second {
		t.Fatalf("unexpected durations: age=%s inactive=%s status=%s", evaluation.Age, evaluation.InactiveFor, evaluation.InCurrentStatusFor)
	}
	if evaluation.Proposal == nil || evaluation.Proposal.Age != evaluation.Age || evaluation.Proposal.InCurrentStatusFor != evaluation.InCurrentStatusFor || !evaluation.Proposal.StatusChangedAt.Equal(evaluation.StatusChangedAt) {
		t.Fatalf("proposal did not preserve temporal anchors: %#v", evaluation.Proposal)
	}
}

func TestLifecycleEvaluationRejectsDatesBeforeEachAnchor(t *testing.T) {
	policy := testLifecyclePolicy()
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("date-event", "vision.motion", chainTestBase.Add(time.Hour), "entry"), mutation(1, "add date observation")); err != nil {
		t.Fatalf("add date observation: %v", err)
	}
	if err := chain.SetStatus(StatusActive, mutation(2, "activate date chain")); err != nil {
		t.Fatalf("activate date chain: %v", err)
	}
	for _, evaluatedAt := range []time.Time{
		chainTestBase.Add(-time.Nanosecond),
		chainTestBase.Add(time.Hour).Add(-time.Nanosecond),
		chainTestBase.Add(2 * time.Second).Add(-time.Nanosecond),
	} {
		if _, err := EvaluateLifecycle(chain.Snapshot(), evaluatedAt, policy); err == nil {
			t.Fatalf("expected date %s to be rejected", evaluatedAt)
		}
	}
}

func TestLifecyclePolicyPreventsPrematureTransitionCascade(t *testing.T) {
	policy := testLifecyclePolicy()
	chain := mustNewChain(t)
	if err := chain.AddObservation(observation("cascade-event", "vision.motion", chainTestBase.Add(time.Second), "entry"), mutation(1, "add cascade observation")); err != nil {
		t.Fatalf("add cascade observation: %v", err)
	}
	if err := chain.SetStatus(StatusActive, mutation(2, "activate cascade chain")); err != nil {
		t.Fatalf("activate cascade chain: %v", err)
	}
	if err := chain.SetStatus(StatusConfirmed, mutation(3, "confirm cascade chain")); err != nil {
		t.Fatalf("confirm cascade chain: %v", err)
	}
	if err := chain.SetConfidence(0.8, mutation(4, "support cascade chain")); err != nil {
		t.Fatalf("support cascade chain: %v", err)
	}
	evaluatedAt := chainTestBase.Add(10 * time.Hour)
	evaluation, err := EvaluateLifecycle(chain.Snapshot(), evaluatedAt, policy)
	if err != nil {
		t.Fatalf("evaluate old confirmed chain: %v", err)
	}
	assertProposal(t, evaluation, StatusConfirmed, StatusDeclining, ReasonConfirmedInactive)
	context, err := evaluation.Proposal.MutationContext("test", "cascade-correlation")
	if err != nil {
		t.Fatalf("prepare decline context: %v", err)
	}
	if err := chain.SetStatus(evaluation.Proposal.To, context); err != nil {
		t.Fatalf("apply manual decline: %v", err)
	}
	current, err := EvaluateLifecycle(chain.Snapshot(), evaluatedAt, policy)
	if err != nil {
		t.Fatalf("reevaluate after manual decline: %v", err)
	}
	if current.Proposal != nil {
		t.Fatalf("same-timestamp evaluation cascaded into another transition: %#v", current.Proposal)
	}
	beforeDormant := evaluatedAt.Add(policy.DecliningDormantAfter - time.Nanosecond)
	current, err = EvaluateLifecycle(chain.Snapshot(), beforeDormant, policy)
	if err != nil {
		t.Fatalf("evaluate before declining retention: %v", err)
	}
	if current.Proposal != nil {
		t.Fatalf("declining transition proposed too early: %#v", current.Proposal)
	}
	atDormant := evaluatedAt.Add(policy.DecliningDormantAfter)
	current, err = EvaluateLifecycle(chain.Snapshot(), atDormant, policy)
	if err != nil {
		t.Fatalf("evaluate at declining retention: %v", err)
	}
	assertProposal(t, current, StatusDeclining, StatusDormant, ReasonDecliningInactive)
}

func TestTransitionProposalPreparesExplicitMutationContext(t *testing.T) {
	policy := testLifecyclePolicy()
	evaluation, err := EvaluateLifecycle(policySnapshot(StatusActive, chainTestBase), chainTestBase.Add(policy.ActiveDeclineAfter+time.Second), policy)
	if err != nil {
		t.Fatalf("evaluate proposal: %v", err)
	}
	context, err := evaluation.Proposal.MutationContext("cge.lifecycle", "correlation-1")
	if err != nil {
		t.Fatalf("prepare mutation context: %v", err)
	}
	if !context.At.Equal(evaluation.Proposal.EvaluatedAt) || context.Actor != "cge.lifecycle" || context.CorrelationID != "correlation-1" || context.Reason == "" {
		t.Fatalf("unexpected prepared mutation context: %#v", context)
	}
}

func assertProposal(t *testing.T, evaluation LifecycleEvaluation, from, to Status, code LifecycleReasonCode) {
	t.Helper()
	if evaluation.Proposal == nil {
		t.Fatalf("expected proposal, evaluation=%#v", evaluation)
	}
	proposal := evaluation.Proposal
	if proposal.ChainID != evaluation.ChainID || proposal.From != from || proposal.To != to || proposal.ReasonCode != code {
		t.Fatalf("unexpected proposal: %#v", proposal)
	}
	if proposal.InactiveFor < 0 || proposal.EvaluatedAt.IsZero() || len(proposal.SupportingFacts) == 0 {
		t.Fatalf("proposal lacks deterministic facts: %#v", proposal)
	}
	if err := ValidateTransition(proposal.From, proposal.To); err != nil {
		t.Fatalf("proposal transition rejected: %v", err)
	}
}
