package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadReplayExpectationValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "expect.json")
	if err := os.WriteFile(path, []byte(`{"minimum_human_detections":2,"minimum_tracks":1,"backend_status":"ok","physical_action_executed":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readReplayExpectation(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.MinimumHumanDetections != 2 || got.MinimumTracks != 1 || got.BackendStatus != "ok" || got.PhysicalActionExecuted {
		t.Fatalf("unexpected expectation: %#v", got)
	}
}

func TestReadReplayExpectationRejectsInvalid(t *testing.T) {
	for name, contents := range map[string]string{
		"negative": `{"minimum_tracks":-1}`,
		"status":   `{"backend_status":"other"}`,
		"unknown":  `{"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "expect.json")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readReplayExpectation(path); err == nil {
				t.Fatal("expected invalid expectation to fail")
			}
		})
	}
}
