package cognitivecore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestUniversalStoreCompactionPreservesHistoryAndReplay(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	core := durableTestCore(store)
	for index := 0; index < 7; index++ {
		if _, err := core.Process(context.Background(), contract.Event{ID: "compact-" + string(rune('a'+index)), Type: "sensor.motion", Source: "discovery", Timestamp: time.Unix(int64(index+1), 0).UTC(), Payload: map[string]any{"movement": true}}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.History()
	if err != nil {
		t.Fatal(err)
	}
	snapshotBefore, err := store.SnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Compact(CompactionOptions{}); err != nil {
		t.Fatal(err)
	}
	report, err := store.DiskReport()
	if err != nil || report.SegmentCount != 1 || report.ArchivedBytes == 0 {
		t.Fatalf("unexpected compaction report=%#v err=%v", report, err)
	}
	after, err := store.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("history changed after compaction before=%d after=%d", len(before), len(after))
	}
	snapshotAfter, err := store.SnapshotJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(snapshotBefore) != string(snapshotAfter) {
		t.Fatal("compaction changed materialized snapshot")
	}
	restarted, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restarted.History()
	if err != nil || len(replayed) != len(before) || restarted.Revision() != store.Revision() {
		t.Fatalf("restart/replay mismatch revision=%d/%d history=%d/%d err=%v", restarted.Revision(), store.Revision(), len(replayed), len(before), err)
	}
}

func TestUniversalStoreCompactionDetectsCorruptArchive(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := durableTestCore(store).Process(context.Background(), contract.Event{ID: "archive-corrupt", Type: "sensor.motion", Source: "discovery", Timestamp: time.Unix(1, 0).UTC(), Payload: map[string]any{"movement": true}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Compact(CompactionOptions{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "segments"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive missing entries=%v err=%v", entries, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "segments", entries[0].Name()), []byte("corrupt\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUniversalStore(dir); err == nil {
		t.Fatal("corrupt audit archive opened successfully")
	}
}
