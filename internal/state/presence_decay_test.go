package state

import (
	"math"
	"testing"
	"time"
)

func TestDecayedPresenceConfidenceUsesFifteenMinuteTau(t *testing.T) {
	if got := DecayedPresenceConfidence(.8, 15*time.Minute); math.Abs(got-.8/math.E) > 0.0001 {
		t.Fatalf("confidence at tau=%v, want %v", got, .8/math.E)
	}
	if got := DecayedPresenceConfidence(.8, -time.Second); got != .8 {
		t.Fatalf("backward clock changed confidence to %v", got)
	}
}

func TestCleanupKeepsTrackTTLSeparateFromPresenceDecay(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := NewStore()
	store.SetEntityTrack(&EntityTrack{ID: "entity", LastSeen: now, UpdatedAt: now, ExpiresAt: now.Add(DefaultExpirationConfig().Tracks)})
	store.SetPresence(&PresenceState{ID: "alexis", ResidentID: "alexis", State: "present", Confidence: .9, LastSeen: now, UpdatedAt: now, ExpiresAt: now.Add(DefaultPresenceTTL)})

	result := store.Cleanup(now.Add(DefaultExpirationConfig().Tracks+time.Second), DefaultExpirationConfig())
	if len(result.Deleted["entity_tracks"]) != 1 {
		t.Fatalf("track TTL was not applied: %#v", result.Deleted)
	}
	presence, ok := store.PresenceState("alexis")
	if !ok || presence == nil || presence.State != "present" {
		t.Fatalf("presence cleanup was coupled to track TTL: %#v", presence)
	}
}
