package actions

import (
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestFileResultStoreSurvivesServiceRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actions", "results.json")
	first, err := OpenFileResultStore(path)
	if err != nil {
		t.Fatal(err)
	}
	result := testStoredResult("idem-1", 1)
	if err := first.Save("idem-1", result); err != nil {
		t.Fatal(err)
	}

	second, err := OpenFileResultStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := second.Lookup("idem-1")
	if err != nil || !ok {
		t.Fatalf("stored result missing: result=%#v ok=%t err=%v", got, ok, err)
	}
	if got.Status != contract.ActionStatusSuccess || got.CommandID != "cmd-idem-1" || got.IdempotencyKey != "idem-1" {
		t.Fatalf("unexpected stored result: %#v", got)
	}
}

func TestResultStoreRetainsOnlyLast200Results(t *testing.T) {
	store := NewMemoryResultStore()
	for i := 0; i < MaxStoredResults+1; i++ {
		key := "idem-" + string(rune('a'+i%26)) + "-" + time.Unix(int64(i), 0).UTC().Format("150405")
		if err := store.Save(key, testStoredResult(key, i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := store.Lookup("idem-a-000000"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("oldest result should have been evicted")
	}
	lastKey := "idem-" + string(rune('a'+MaxStoredResults%26)) + "-000320"
	if _, ok, err := store.Lookup(lastKey); err != nil || !ok {
		t.Fatalf("newest result missing: ok=%t err=%v", ok, err)
	}
}

func testStoredResult(key string, offset int) contract.ActionResult {
	at := time.Unix(int64(offset), 0).UTC()
	return contract.ActionResult{
		ID:             "result-" + key,
		RequestID:      "request-" + key,
		CommandID:      "cmd-" + key,
		IdempotencyKey: key,
		Status:         contract.ActionStatusSuccess,
		StartedAt:      at,
		FinishedAt:     at,
		Timestamp:      at,
	}
}
