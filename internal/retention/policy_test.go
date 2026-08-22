package retention

import (
	"testing"
	"time"
)

func TestDefaultPolicyIsCompleteAndSafe(t *testing.T) {
	policy := DefaultPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if policy.MinFreeBytes < 256<<20 || policy.Clips.MaxCount < 1 || policy.Outbox.MaxCount < 1 {
		t.Fatalf("unsafe policy: %#v", policy)
	}
}

func TestSelectExpiredIsDeterministicAndProtectsReferences(t *testing.T) {
	policy := DefaultPolicy()
	policy.Events = Limit{MaxAge: time.Hour, MaxCount: 2, MaxBytes: 10}
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{ID: "event-b", Category: CategoryEvents, CreatedAt: now.Add(-2 * time.Hour), SizeBytes: 5},
		{ID: "event-a", Category: CategoryEvents, CreatedAt: now.Add(-2 * time.Hour), SizeBytes: 5},
		{ID: "event-protected", Category: CategoryEvents, CreatedAt: now.Add(-3 * time.Hour), SizeBytes: 100, Protected: true},
		{ID: "event-new", Category: CategoryEvents, CreatedAt: now.Add(-time.Minute), SizeBytes: 5},
	}
	removed := policy.SelectExpired(entries, now)
	if len(removed) != 2 || removed[0].ID != "event-a" || removed[1].ID != "event-b" {
		t.Fatalf("unexpected deterministic selection: %#v", removed)
	}
	for _, entry := range removed {
		if entry.Protected {
			t.Fatal("protected entry selected")
		}
	}
}

func TestHasReserveAccountsForIncomingBytes(t *testing.T) {
	ok, err := HasReserve(t.TempDir(), 1, 1)
	if err != nil || !ok {
		t.Fatalf("reserve check failed: ok=%t err=%v", ok, err)
	}
	if _, err := HasReserve(t.TempDir(), -1, 1); err == nil {
		t.Fatal("negative incoming bytes accepted")
	}
}
