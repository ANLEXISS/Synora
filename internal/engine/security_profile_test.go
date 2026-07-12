package engine

import (
	"testing"
	"time"

	"synora/internal/engine/danger"
	"synora/pkg/contract"
)

func TestSecurityProfileInfluencesDangerScore(t *testing.T) {
	event := &contract.Event{ID: "unknown-1", Type: contract.EventVisionUnknown, NodeID: "entry", Timestamp: time.Date(2026, 7, 12, 23, 0, 0, 0, time.UTC), Payload: map[string]any{}}
	base := danger.AssessEvent(event, danger.Context{NodeID: "entry", DeviceRole: "access_control", Now: event.Timestamp})
	profile := contract.DefaultCgeSecurityProfile()
	profile.GlobalSensitivity = 1
	profile.NightSensitivityMultiplier = 2
	profile.CriticalRooms = []string{"entry"}
	context := danger.Context{NodeID: "entry", DeviceRole: "access_control", TimeBucket: "night", Now: event.Timestamp, GlobalSensitivity: profile.GlobalSensitivity, NightMultiplier: profile.NightSensitivityMultiplier, CriticalRoom: true, ProfileEnabled: true}
	configured := danger.AssessEvent(event, context)
	if configured.Score <= base.Score || configured.Level < base.Level {
		t.Fatalf("profile did not increase danger: base=%#v configured=%#v", base, configured)
	}
}
