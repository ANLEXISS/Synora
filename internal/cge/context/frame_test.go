package context

import (
	stdcontext "context"
	"errors"
	"testing"
	"time"
)

func TestResolveFrameIsDeterministicAndVersioned(t *testing.T) {
	at := time.Date(2026, 7, 18, 21, 17, 0, 0, time.UTC)
	input := ResolveInput{ObservationID: "obs-1", ObservedAt: at, NodeID: "room", Timezone: "Europe/Paris", Occupancy: OccupancyOccupied, HouseMode: HouseModeHome, Topology: testTopology()}
	first, err := ResolveFrame(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveFrame(input)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Fingerprint == "" {
		t.Fatalf("resolution is not deterministic: %#v %#v", first, second)
	}
	if first.Quality != QualityComplete || first.Time.Weekday != time.Saturday || !first.Time.Weekend || first.Time.DayPart != DayPartEvening {
		t.Fatalf("unexpected frame temporal/context values: %#v", first)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	if first.Time.UTCOffsetMinutes != 120 {
		t.Fatalf("Paris summer offset = %d", first.Time.UTCOffsetMinutes)
	}
	legacy := first
	legacy.Fingerprint = "bad"
	if err := legacy.Validate(); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("bad fingerprint error = %v", err)
	}
}

func TestResolveFramePartialAndUnknownNode(t *testing.T) {
	at := time.Date(2026, 7, 18, 1, 2, 0, 0, time.UTC)
	partial, err := ResolveFrame(ResolveInput{ObservationID: "obs-partial", ObservedAt: at, NodeID: "unknown", Timezone: "UTC", Occupancy: OccupancyUnknown, HouseMode: HouseModeUnknown, Topology: testTopology(), AllowPartial: true})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Quality != QualityPartial || partial.NodeKind != NodeUnknown {
		t.Fatalf("unexpected partial frame: %#v", partial)
	}
	if _, err := ResolveFrame(ResolveInput{ObservationID: "obs-unknown", ObservedAt: at, NodeID: "unknown", Timezone: "UTC", Occupancy: OccupancyUnknown, HouseMode: HouseModeUnknown, Topology: testTopology()}); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("unknown node error = %v", err)
	}
	if _, err := ResolveFrame(ResolveInput{ObservationID: "obs-invalid", ObservedAt: at, NodeID: "room", Timezone: "Mars/Phobos", Topology: testTopology()}); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("timezone error = %v", err)
	}
}

func TestTemporalBoundariesAndSignature(t *testing.T) {
	for _, tc := range []struct {
		at      time.Time
		part    DayPart
		weekend bool
	}{
		{time.Date(2026, 7, 20, 5, 59, 0, 0, time.UTC), DayPartNight, false},
		{time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC), DayPartMorning, false},
		{time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC), DayPartDay, false},
		{time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC), DayPartEvening, false},
		{time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC), DayPartNight, true},
	} {
		frame, err := ResolveFrame(ResolveInput{ObservationID: tc.at.Format("150405"), ObservedAt: tc.at, NodeID: "room", Timezone: "UTC", Topology: testTopology(), AllowPartial: true})
		if err != nil {
			t.Fatal(err)
		}
		if frame.Time.DayPart != tc.part || frame.Time.Weekend != tc.weekend {
			t.Fatalf("at %v got %s/%v", tc.at, frame.Time.DayPart, frame.Time.Weekend)
		}
		signature, err := FrameSignature(frame)
		if err != nil {
			t.Fatal(err)
		}
		if signature.Fingerprint == "" || signature.TimeBucket != frame.Time.MinuteOfDay/15 {
			t.Fatalf("bad signature: %#v", signature)
		}
	}
}

func TestTemporalContextPreservesDSTOffsetAndLocalWeek(t *testing.T) {
	before, err := ResolveFrame(ResolveInput{ObservationID: "dst-before", ObservedAt: time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), NodeID: "room", Timezone: "Europe/Paris", Topology: testTopology()})
	if err != nil {
		t.Fatal(err)
	}
	after, err := ResolveFrame(ResolveInput{ObservationID: "dst-after", ObservedAt: time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), NodeID: "room", Timezone: "Europe/Paris", Topology: testTopology()})
	if err != nil {
		t.Fatal(err)
	}
	if before.Time.UTCOffsetMinutes != 120 || after.Time.UTCOffsetMinutes != 60 || before.Time.MinuteOfDay != after.Time.MinuteOfDay {
		t.Fatalf("DST local context mismatch: before=%#v after=%#v", before.Time, after.Time)
	}
}

func TestStaticProviderHonorsCancellation(t *testing.T) {
	provider := StaticProvider{Timezone: "UTC", AllowPartial: true}
	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	cancel()
	if _, err := provider.Resolve(ctx, "obs", time.Now().UTC(), "room"); err == nil {
		t.Fatal("expected canceled provider")
	}
}
