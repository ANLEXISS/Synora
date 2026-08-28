package contract

import (
	"math"
	"testing"
)

func TestClipContractValidatesAndTransitions(t *testing.T) {
	clip := Clip{ID: "clip-1", CameraID: "cam-1", Status: ClipStatusReceiving, SizeBytes: 10}
	if err := clip.Validate(); err != nil {
		t.Fatal(err)
	}
	allowed := [][2]ClipStatus{{ClipStatusReceiving, ClipStatusReady}, {ClipStatusReady, ClipStatusProcessing}, {ClipStatusProcessing, ClipStatusProcessed}, {ClipStatusProcessed, ClipStatusExpired}}
	for _, transition := range allowed {
		if !ValidClipTransition(transition[0], transition[1]) {
			t.Fatalf("transition should be allowed: %s -> %s", transition[0], transition[1])
		}
	}
	if ValidClipTransition(ClipStatusExpired, ClipStatusReady) || ValidClipTransition(ClipStatusProcessed, ClipStatusReady) {
		t.Fatal("invalid clip transition accepted")
	}
}

func TestClipContractTransitionMatrix(t *testing.T) {
	statuses := []ClipStatus{
		ClipStatusReceiving, ClipStatusReady, ClipStatusProcessing,
		ClipStatusProcessed, ClipStatusFailed, ClipStatusMissing, ClipStatusExpired,
	}
	allowed := map[ClipStatus]map[ClipStatus]bool{
		ClipStatusReceiving:  {ClipStatusReady: true, ClipStatusFailed: true},
		ClipStatusReady:      {ClipStatusProcessing: true, ClipStatusProcessed: true, ClipStatusMissing: true, ClipStatusExpired: true, ClipStatusFailed: true},
		ClipStatusProcessing: {ClipStatusProcessed: true, ClipStatusReady: true, ClipStatusFailed: true, ClipStatusMissing: true, ClipStatusExpired: true},
		ClipStatusProcessed:  {ClipStatusMissing: true, ClipStatusExpired: true},
		ClipStatusFailed:     {ClipStatusReady: true, ClipStatusExpired: true},
		ClipStatusMissing:    {ClipStatusReady: true, ClipStatusExpired: true},
		ClipStatusExpired:    {},
	}
	for _, from := range statuses {
		for _, to := range statuses {
			want := from == to || allowed[from][to]
			if got := ValidClipTransition(from, to); got != want {
				t.Fatalf("transition %s -> %s = %t, want %t", from, to, got, want)
			}
		}
	}
}

func TestClipContractRejectsInvalidMetadata(t *testing.T) {
	base := Clip{ID: "clip-1", CameraID: "cam-1", Status: ClipStatusReady}
	for name, value := range map[string]Clip{
		"negative index":    func() Clip { v := base; v.ClipIndex = -1; return v }(),
		"bad checksum":      func() Clip { v := base; v.Checksum = "not-a-digest"; return v }(),
		"infinite duration": func() Clip { v := base; v.Duration = math.Inf(1); return v }(),
	} {
		if err := value.Validate(); err == nil {
			t.Fatalf("%s should be rejected: %#v", name, value)
		}
	}
}
