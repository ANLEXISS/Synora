package vision

import (
	"testing"
	"time"
)

func TestClipManagerAssignsFixedWindowAndEpisodeContinuity(t *testing.T) {
	base := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	m, err := NewClipManager(10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Open("clip-1", "cam-1", "front", "exterior", "motion.sensor.front", "track-1", base)
	if err != nil {
		t.Fatal(err)
	}
	if !first.EndsAt.Equal(base.Add(10 * time.Second)) {
		t.Fatalf("ends_at=%s", first.EndsAt)
	}
	second, err := m.Open("clip-2", "cam-1", "front", "exterior", "motion.sensor.front", "", base.Add(12*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.EpisodeID != first.EpisodeID {
		t.Fatalf("episode continuity lost: %s != %s", second.EpisodeID, first.EpisodeID)
	}
	third, err := m.Open("clip-3", "cam-1", "front", "exterior", "motion.sensor.front", "track-1", base.Add(40*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if third.EpisodeID == second.EpisodeID {
		t.Fatal("distant clip reused episode")
	}
}
