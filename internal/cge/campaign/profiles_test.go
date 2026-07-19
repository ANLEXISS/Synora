package campaign

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultProfilesValidate(t *testing.T) {
	profiles := DefaultProfiles()
	if len(profiles) != 8 {
		t.Fatalf("profiles = %d, want 8", len(profiles))
	}
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			t.Fatalf("%s: %v", profile.ID, err)
		}
		timeline, err := GenerateTimeline(profile)
		if err != nil {
			t.Fatalf("%s timeline: %v", profile.ID, err)
		}
		if len(timeline.Events) == 0 {
			t.Fatalf("%s generated no events", profile.ID)
		}
	}
}

func TestTimelineDeterministicAndLabelsInvisible(t *testing.T) {
	profile, ok := ProfileByID("stable_single_resident_30d")
	if !ok {
		t.Fatal("stable profile missing")
	}
	profile.DurationDays = 7
	first, err := GenerateTimeline(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateTimeline(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same profile and seed produced different timelines")
	}
	if len(first.Events) == 0 {
		t.Fatal("empty timeline")
	}
	changed := first.Events[0]
	changed.Label = LabelSyntheticIntrusion
	if changed.ID != first.Events[0].ID {
		t.Fatal("experimental label changed event identity")
	}
	if !reflect.DeepEqual(buildEvent(changed), buildEvent(first.Events[0])) {
		t.Fatal("experimental label changed CGE input")
	}
	profile.Seed++
	third, err := GenerateTimeline(profile)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, third) {
		t.Fatal("different seed produced identical timeline")
	}
}

func TestCheckpointBoundariesAreSingleEvents(t *testing.T) {
	profile, _ := ProfileByID("long_memory_90d")
	profile.DurationDays = 14
	timeline, err := GenerateTimeline(profile)
	if err != nil {
		t.Fatal(err)
	}
	perDay := map[string]int{}
	for _, event := range timeline.Events {
		if event.CheckpointAfter {
			key := event.OccurredAt.In(mustLocation(t, profile.Timezone)).Format("2006-01-02")
			perDay[key]++
		}
	}
	for day, count := range perDay {
		if count != 1 {
			t.Fatalf("day %s has %d checkpoint markers", day, count)
		}
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}
