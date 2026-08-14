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
