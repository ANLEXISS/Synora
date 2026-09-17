package cognitive

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureDisabledDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dataset.jsonl")
	recorder, enabled := NewRecorderFromEnv(func(key string) string {
		if key == CapturePathEnv {
			return path
		}
		return ""
	})
	if enabled || recorder != nil {
		t.Fatal("capture must be disabled by default")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("capture wrote while disabled: %v", err)
	}
}

func TestCaptureEnabledWritesFrozenInputAndTeacher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dataset.jsonl")
	recorder, enabled := NewRecorderFromEnv(func(key string) string {
		switch key {
		case CaptureEnabledEnv:
			return "true"
		case CapturePathEnv:
			return path
		default:
			return ""
		}
	})
	if !enabled || recorder == nil {
		t.Fatal("capture must be enabled")
	}
	pending := recorder.Before(map[string]any{
		"event":         map[string]any{"type": "motion.detected"},
		"state_before":  map[string]any{"revision": 4},
		"recent_events": []any{"old"},
		"topology":      []any{"room"},
		"residents":     []any{"resident"},
	})
	recorder.After(pending, TeacherDecision{EventID: "event-1", EventType: "motion.detected", InferredState: "present"})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("expected one JSONL record")
	}
	var record DatasetRecord
	if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Teacher.EventID != "event-1" || record.Teacher.InferredState != "present" || record.Redaction.Applied {
		t.Fatalf("unexpected record: %+v", record)
	}
	var event map[string]any
	if err := json.Unmarshal(record.Input.Event, &event); err != nil || event["type"] != "motion.detected" {
		t.Fatalf("unexpected frozen event: %s", record.Input.Event)
	}
}
