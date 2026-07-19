package chains

import (
	"errors"
	"strings"
	"testing"
)

func TestLifecycleCategoriesAndPolicy(t *testing.T) {
	if !StatusActive.IsActiveModelStatus() || !StatusConfirmed.IsActiveModelStatus() || !StatusDeclining.IsActiveModelStatus() || !StatusReactivated.IsActiveModelStatus() {
		t.Fatal("expected active-model statuses")
	}
	if StatusCandidate.IsActiveModelStatus() || !StatusCandidate.IsPreModelStatus() {
		t.Fatal("candidate must remain pre-model")
	}
	if !StatusDormant.IsHistoricalStatus() || !StatusArchived.IsHistoricalStatus() || StatusActive.IsHistoricalStatus() {
		t.Fatal("unexpected historical classification")
	}
	if !StatusMerged.IsReplacementStatus() || !StatusSplit.IsReplacementStatus() || StatusActive.IsReplacementStatus() {
		t.Fatal("unexpected replacement classification")
	}
	if !StatusInvalidated.IsTerminal() || !StatusMerged.IsTerminal() || !StatusSplit.IsTerminal() || StatusArchived.IsTerminal() {
		t.Fatal("unexpected terminal classification")
	}

	valid := [][2]Status{
		{StatusCandidate, StatusActive},
		{StatusActive, StatusConfirmed},
		{StatusConfirmed, StatusDeclining},
		{StatusDeclining, StatusDormant},
		{StatusDormant, StatusArchived},
		{StatusDormant, StatusReactivated},
		{StatusArchived, StatusReactivated},
		{StatusReactivated, StatusActive},
		{StatusCandidate, StatusInvalidated},
	}
	for _, transition := range valid {
		if !CanTransition(transition[0], transition[1]) {
			t.Errorf("expected transition %s -> %s to be allowed", transition[0], transition[1])
		}
		if err := ValidateTransition(transition[0], transition[1]); err != nil {
			t.Errorf("validate %s -> %s: %v", transition[0], transition[1], err)
		}
	}
}

func TestLifecycleTransitionsRecordExplicitHistory(t *testing.T) {
	chain := mustNewChain(t)
	transitions := []struct {
		to        Status
		operation RevisionOperation
		reason    string
	}{
		{StatusActive, OperationStatusChanged, "manual activation"},
		{StatusConfirmed, OperationStatusChanged, "manual confirmation"},
		{StatusDeclining, OperationStatusChanged, "evidence contradiction"},
		{StatusDormant, OperationStatusChanged, "manual dormancy"},
		{StatusArchived, OperationChainArchived, "manual archival"},
		{StatusReactivated, OperationChainReactivated, "manual reactivation"},
		{StatusActive, OperationStatusChanged, "resume active model"},
	}
	for i, transition := range transitions {
		context := mutation(i+1, transition.reason)
		context.CorrelationID = "correlation-lifecycle"
		if err := chain.SetStatus(transition.to, context); err != nil {
			t.Fatalf("transition to %s: %v", transition.to, err)
		}
		history := chain.History()
		record := history[len(history)-1]
		if record.Operation != transition.operation || record.PreviousStatus != history[len(history)-2].NewStatus || record.NewStatus != transition.to {
			t.Fatalf("unexpected transition record: %#v", record)
		}
		if record.Actor != "test" || record.Reason != transition.reason || record.CorrelationID != "correlation-lifecycle" {
			t.Fatalf("transition provenance was not retained: %#v", record)
		}
		if record.PreviousRevision != uint64(i+1) || record.NewRevision != uint64(i+2) {
			t.Fatalf("transition revision = %d -> %d, want %d -> %d", record.PreviousRevision, record.NewRevision, i+1, i+2)
		}
	}
	if err := chain.Validate(); err != nil {
		t.Fatalf("valid lifecycle chain rejected: %v", err)
	}
	history := chain.History()
	last := history[len(history)-1]
	if snapshot := chain.Snapshot(); snapshot.Status != last.NewStatus || snapshot.Revision != last.NewRevision {
		t.Fatalf("snapshot does not match last lifecycle revision: snapshot=%#v record=%#v", snapshot, last)
	}
}

func TestLifecycleSupportsDormantReactivationAndInvalidation(t *testing.T) {
	t.Run("dormant reactivation", func(t *testing.T) {
		chain := mustNewChain(t)
		for offset, status := range []Status{StatusActive, StatusDeclining, StatusDormant, StatusReactivated} {
			if err := chain.SetStatus(status, mutation(offset+1, "explicit lifecycle transition")); err != nil {
				t.Fatalf("set %s: %v", status, err)
			}
		}
		history := chain.History()
		if history[len(history)-1].Operation != OperationChainReactivated {
			t.Fatalf("dormant reactivation operation = %q", history[len(history)-1].Operation)
		}
	})

	t.Run("candidate invalidation", func(t *testing.T) {
		chain := mustNewChain(t)
		if err := chain.SetStatus(StatusInvalidated, mutation(1, "operator correction")); err != nil {
			t.Fatalf("invalidate candidate: %v", err)
		}
		history := chain.History()
		if history[len(history)-1].Operation != OperationStatusChanged || chain.Snapshot().Status != StatusInvalidated {
			t.Fatalf("unexpected invalidation result: %#v", history[len(history)-1])
		}
	})
}

func TestLifecycleTransitionRejectionIsNonMutating(t *testing.T) {
	chain := mustNewChain(t)
	if err := chain.SetStatus(StatusActive, mutation(1, "activate")); err != nil {
		t.Fatalf("activate: %v", err)
	}
	before := chain.Snapshot()
	invalid := [][2]Status{
		{StatusActive, StatusActive},
		{StatusCandidate, StatusConfirmed},
		{StatusCandidate, StatusDormant},
		{StatusArchived, StatusActive},
		{StatusDormant, StatusConfirmed},
		{StatusInvalidated, StatusCandidate},
		{StatusMerged, StatusActive},
		{StatusSplit, StatusActive},
		{StatusActive, StatusMerged},
		{StatusActive, StatusSplit},
		{Status("unknown"), StatusActive},
		{StatusActive, Status("unknown")},
	}
	for _, transition := range invalid {
		err := ValidateTransition(transition[0], transition[1])
		if err == nil {
			t.Errorf("expected transition %s -> %s to be rejected", transition[0], transition[1])
		}
	}
	if err := chain.SetStatus(StatusConfirmed, mutation(0, "out of order")); err == nil {
		t.Fatal("expected out-of-order transition context to be rejected")
	}
	if err := chain.SetStatus(StatusConfirmed, mutation(2, "confirm")); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	before = chain.Snapshot()
	if err := chain.SetStatus(StatusDormant, mutation(1, "reject old transition")); err == nil {
		t.Fatal("expected old timestamp to be rejected")
	}
	after := chain.Snapshot()
	if after.Revision != before.Revision || after.Status != before.Status || len(after.History) != len(before.History) {
		t.Fatalf("rejected transition changed chain: before=%#v after=%#v", before, after)
	}
}

func TestLifecycleValidateDetectsIncoherentHistory(t *testing.T) {
	t.Run("impossible transition", func(t *testing.T) {
		chain := mustNewChain(t)
		if err := chain.SetStatus(StatusActive, mutation(1, "activate")); err != nil {
			t.Fatalf("activate: %v", err)
		}
		chain.history[1].NewStatus = StatusConfirmed
		chain.status = StatusConfirmed
		if err := chain.Validate(); err == nil || !strings.Contains(err.Error(), "invalid chain status transition") {
			t.Fatalf("expected impossible transition error, got %v", err)
		}
	})

	t.Run("terminal exit", func(t *testing.T) {
		chain := mustNewChain(t)
		if err := chain.SetStatus(StatusInvalidated, mutation(1, "invalidate")); err != nil {
			t.Fatalf("invalidate: %v", err)
		}
		record := newRevisionRecord(chain.id, OperationStatusChanged, mutation(2, "corrupt terminal exit"))
		record.PreviousRevision = 2
		record.NewRevision = 3
		record.PreviousStatus = StatusInvalidated
		record.NewStatus = StatusActive
		chain.history = append(chain.history, record)
		chain.revision = 3
		chain.status = StatusActive
		if err := chain.Validate(); err == nil || !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("expected terminal-exit error, got %v", err)
		}
	})

	t.Run("specialized operation mismatch", func(t *testing.T) {
		chain := mustNewChain(t)
		if err := chain.SetStatus(StatusActive, mutation(1, "activate")); err != nil {
			t.Fatalf("activate: %v", err)
		}
		chain.history[1].Operation = OperationChainArchived
		if err := chain.Validate(); err == nil || !strings.Contains(err.Error(), "uses operation") {
			t.Fatalf("expected operation mismatch error, got %v", err)
		}
	})
}
