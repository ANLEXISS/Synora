package fieldtrial

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig(root string) Config {
	c := DefaultConfig()
	c.Enabled = true
	c.RootDir = root
	c.SessionID = "cge-trial-test"
	c.SegmentMaxBytes = 300
	c.MaximumTotalBytes = 1 << 20
	return c
}

func testInput(at time.Time, id string) EventInput {
	return EventInput{ObservedAt: at, RecordedAt: at.Add(time.Second), EventID: id, SubjectID: "resident-secret", ChainID: "chain-secret", NodeID: "kitchen-secret", ZoneID: "zone-secret", ContextQuality: "complete", NodeKind: "room", DeviationAttempted: true, DeviationStatus: "evaluated", DeviationBand: "aligned", DeviationFingerprint: "sha256:assessment", CognitiveWALSequence: 12, CognitiveWALHash: "sha256:wal-secret"}
}

func TestRecorderRotationPrivacyAndRecovery(t *testing.T) {
	root := t.TempDir()
	config := testConfig(root)
	key := []byte("test-key-for-field-trial-privacy")
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	recorder, err := OpenWithKey(context.Background(), config, OpenMetadata{}, base, key)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := recorder.Record(context.Background(), testInput(base.Add(time.Duration(i)*time.Minute), "event-secret-"+string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	first := recorder.Manifest()
	if first.EventCount != 6 || first.SegmentCount < 2 {
		t.Fatalf("manifest=%+v", first)
	}
	if err := recorder.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, _, _, err := ReadEvents(context.Background(), filepath.Join(root, config.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 6 || events[0].EventRef == "" {
		t.Fatalf("events=%d", len(events))
	}
	if strings.Contains(string(mustReadAll(t, filepath.Join(root, config.SessionID, "events-000001.ndjson"))), "resident-secret") || strings.Contains(string(mustReadAll(t, filepath.Join(root, config.SessionID, "events-000001.ndjson"))), "chain-secret") {
		t.Fatal("raw identifier leaked")
	}
	recovered, err := OpenWithKey(context.Background(), config, OpenMetadata{}, base, key)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Manifest().Status != SessionRecovered || recovered.Manifest().EventCount != 6 {
		t.Fatalf("recovered=%+v", recovered.Manifest())
	}
	if err := recovered.AddAnnotation(context.Background(), AnnotationInput{EventRef: events[0].EventRef, Label: AnnotationOrdinary, AnnotatedAt: base.Add(time.Hour), Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.Record(context.Background(), testInput(base.Add(10*time.Minute), "event-secret-next")); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Close(context.Background(), base.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if recovered.Manifest().Status != SessionClosed {
		t.Fatal(recovered.Manifest().Status)
	}
}

func TestRecorderTerminalPartialRecoveryIsExplicit(t *testing.T) {
	root := t.TempDir()
	config := testConfig(root)
	config.SegmentMaxBytes = 1 << 20
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	recorder, err := OpenWithKey(context.Background(), config, OpenMetadata{}, base, []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.Record(context.Background(), testInput(base, "event-a")); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, config.SessionID, "events-000001.ndjson")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("partial"); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := VerifySession(context.Background(), filepath.Dir(path), false); err == nil {
		t.Fatal("expected partial record error")
	}
	config.RepairTerminalPartial = true
	recovered, err := OpenWithKey(context.Background(), config, OpenMetadata{}, base, []byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.Record(context.Background(), testInput(base.Add(time.Minute), "event-b")); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Close(context.Background(), base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestDisabledRecorderDoesNotCreateFiles(t *testing.T) {
	config := DefaultConfig()
	config.RootDir = t.TempDir()
	recorder, err := Open(context.Background(), config, OpenMetadata{})
	if err != nil || recorder != nil {
		t.Fatalf("recorder=%v err=%v", recorder, err)
	}
	entries, err := os.ReadDir(config.RootDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected files: %d", len(entries))
	}
}

func TestQuotaFailureMarksTelemetryOnly(t *testing.T) {
	config := testConfig(t.TempDir())
	config.SegmentMaxBytes = 256
	config.MaximumTotalBytes = 256
	r, err := OpenWithKey(context.Background(), config, OpenMetadata{}, time.Now().UTC(), []byte("quota-test-key-with-length"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Record(context.Background(), testInput(time.Now().UTC(), "quota-event")); err == nil {
		t.Fatal("expected quota error")
	}
	if r.Status() != SessionDegraded {
		t.Fatal("quota did not degrade telemetry")
	}
}

func mustReadAll(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
