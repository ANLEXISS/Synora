package context

import (
	stdcontext "context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type OccupancyState string

const (
	OccupancyUnknown    OccupancyState = "unknown"
	OccupancyUnoccupied OccupancyState = "unoccupied"
	OccupancyOccupied   OccupancyState = "occupied"
)

type HouseMode string

const (
	HouseModeUnknown HouseMode = "unknown"
	HouseModeHome    HouseMode = "home"
	HouseModeAway    HouseMode = "away"
	HouseModeNight   HouseMode = "night"
	HouseModeSleep   HouseMode = "sleep"
	HouseModeArmed   HouseMode = "armed"
)

type ContextQuality string

const (
	QualityUnknown  ContextQuality = "unknown"
	QualityPartial  ContextQuality = "partial"
	QualityComplete ContextQuality = "complete"
)

type DayPart string

const (
	DayPartNight   DayPart = "night"
	DayPartMorning DayPart = "morning"
	DayPartDay     DayPart = "day"
	DayPartEvening DayPart = "evening"
)

type TemporalContext struct {
	Timezone         string       `json:"timezone"`
	UTCOffsetMinutes int          `json:"utc_offset_minutes"`
	Weekday          time.Weekday `json:"weekday"`
	MinuteOfDay      int          `json:"minute_of_day"`
	MinuteOfWeek     int          `json:"minute_of_week"`
	DayPart          DayPart      `json:"day_part"`
	Weekend          bool         `json:"weekend"`
}

type Frame struct {
	SchemaVersion       SchemaVersion   `json:"schema_version"`
	ObservationID       string          `json:"observation_id"`
	ObservedAt          time.Time       `json:"observed_at"`
	TopologyRevision    string          `json:"topology_revision,omitempty"`
	NodeID              string          `json:"node_id,omitempty"`
	ParentID            string          `json:"parent_id,omitempty"`
	ZoneID              string          `json:"zone_id,omitempty"`
	NodeKind            NodeKind        `json:"node_kind"`
	EntryPoint          bool            `json:"entry_point,omitempty"`
	Exterior            bool            `json:"exterior,omitempty"`
	Occupancy           OccupancyState  `json:"occupancy"`
	HouseMode           HouseMode       `json:"house_mode"`
	Time                TemporalContext `json:"time"`
	Quality             ContextQuality  `json:"quality"`
	SnapshotFingerprint string          `json:"snapshot_fingerprint,omitempty"`
	FreshnessCode       string          `json:"freshness_code,omitempty"`
	Fingerprint         string          `json:"fingerprint"`
}

type ResolveInput struct {
	ObservationID string
	ObservedAt    time.Time
	NodeID        string
	Timezone      string
	Occupancy     OccupancyState
	HouseMode     HouseMode
	Topology      TopologySnapshot
	AllowPartial  bool
}

func validOccupancy(value OccupancyState) bool {
	return value == OccupancyUnknown || value == OccupancyUnoccupied || value == OccupancyOccupied
}
func validHouseMode(value HouseMode) bool {
	return value == HouseModeUnknown || value == HouseModeHome || value == HouseModeAway || value == HouseModeNight || value == HouseModeSleep || value == HouseModeArmed
}
func validQuality(value ContextQuality) bool {
	return value == QualityUnknown || value == QualityPartial || value == QualityComplete
}
func validDayPart(value DayPart) bool {
	return value == DayPartNight || value == DayPartMorning || value == DayPartDay || value == DayPartEvening
}

func temporalAt(at time.Time, timezone string) (TemporalContext, error) {
	if strings.TrimSpace(timezone) == "" {
		return TemporalContext{}, fmt.Errorf("%w: timezone is empty", ErrInvalidTimezone)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return TemporalContext{}, fmt.Errorf("%w: %s", ErrInvalidTimezone, err)
	}
	local := at.In(loc)
	minute := local.Hour()*60 + local.Minute()
	weekday := local.Weekday()
	mondayIndex := (int(weekday) + 6) % 7
	part := DayPartNight
	switch {
	case minute >= 6*60 && minute < 12*60:
		part = DayPartMorning
	case minute >= 12*60 && minute < 18*60:
		part = DayPartDay
	case minute >= 18*60:
		part = DayPartEvening
	}
	_, offset := local.Zone()
	return TemporalContext{Timezone: timezone, UTCOffsetMinutes: offset / 60, Weekday: weekday, MinuteOfDay: minute, MinuteOfWeek: mondayIndex*1440 + minute, DayPart: part, Weekend: weekday == time.Saturday || weekday == time.Sunday}, nil
}

func ResolveFrame(input ResolveInput) (Frame, error) {
	if !validIdentifier(strings.TrimSpace(input.ObservationID)) {
		return Frame{}, fmt.Errorf("%w: observation id", ErrInvalidFrame)
	}
	if input.ObservedAt.IsZero() {
		return Frame{}, fmt.Errorf("%w: observed_at", ErrInvalidFrame)
	}
	if input.NodeID != "" && !validIdentifier(input.NodeID) {
		return Frame{}, fmt.Errorf("%w: node id", ErrInvalidFrame)
	}
	if input.Occupancy == "" {
		input.Occupancy = OccupancyUnknown
	}
	if input.HouseMode == "" {
		input.HouseMode = HouseModeUnknown
	}
	if !validOccupancy(input.Occupancy) || !validHouseMode(input.HouseMode) {
		return Frame{}, fmt.Errorf("%w: domestic state", ErrInvalidFrame)
	}
	timeValue, err := temporalAt(input.ObservedAt, input.Timezone)
	if err != nil {
		return Frame{}, err
	}
	quality := QualityComplete
	var node Node
	if err := input.Topology.Validate(); err != nil {
		if !input.AllowPartial {
			return Frame{}, err
		}
		quality = QualityPartial
	} else {
		found := false
		for _, candidate := range input.Topology.Nodes {
			if candidate.ID == input.NodeID {
				node = candidate
				found = true
				break
			}
		}
		if !found {
			if !input.AllowPartial {
				return Frame{}, fmt.Errorf("%w: %q", ErrUnknownNode, input.NodeID)
			}
			quality = QualityPartial
		}
	}
	frame := Frame{SchemaVersion: SchemaVersionCurrent, ObservationID: input.ObservationID, ObservedAt: input.ObservedAt, TopologyRevision: input.Topology.Revision, NodeID: input.NodeID, ParentID: node.ParentID, ZoneID: node.ZoneID, NodeKind: node.Kind, EntryPoint: node.EntryPoint, Exterior: node.Exterior, Occupancy: input.Occupancy, HouseMode: input.HouseMode, Time: timeValue, Quality: quality}
	if frame.NodeKind == "" {
		frame.NodeKind = NodeUnknown
	}
	frame.Fingerprint = frameFingerprint(frame)
	return frame, nil
}

func (f Frame) Validate() error {
	if f.SchemaVersion != SchemaVersionV1 || !validIdentifier(strings.TrimSpace(f.ObservationID)) || f.ObservedAt.IsZero() || !validNodeKind(f.NodeKind) || !validOccupancy(f.Occupancy) || !validHouseMode(f.HouseMode) || !validQuality(f.Quality) || !validDayPart(f.Time.DayPart) {
		return fmt.Errorf("%w: fields", ErrInvalidFrame)
	}
	if f.NodeID != "" && !validIdentifier(f.NodeID) {
		return fmt.Errorf("%w: node id", ErrInvalidFrame)
	}
	if f.FreshnessCode != "" && !FreshnessCode(f.FreshnessCode).valid() {
		return fmt.Errorf("%w: freshness", ErrInvalidFrame)
	}
	if f.TimezoneInvalid() {
		return fmt.Errorf("%w: timezone", ErrInvalidFrame)
	}
	expectedTime, err := temporalAt(f.ObservedAt, f.Time.Timezone)
	if err != nil || expectedTime != f.Time {
		return fmt.Errorf("%w: temporal context does not match observed timestamp", ErrInvalidFrame)
	}
	if f.Time.MinuteOfDay < 0 || f.Time.MinuteOfDay >= 1440 || f.Time.MinuteOfWeek < 0 || f.Time.MinuteOfWeek >= 10080 {
		return fmt.Errorf("%w: temporal bounds", ErrInvalidFrame)
	}
	if f.Fingerprint == "" || frameFingerprint(f) != f.Fingerprint {
		return fmt.Errorf("%w: fingerprint", ErrInvalidFrame)
	}
	return nil
}
func (f Frame) TimezoneInvalid() bool {
	_, err := time.LoadLocation(f.Time.Timezone)
	return strings.TrimSpace(f.Time.Timezone) == "" || err != nil
}
func (f Frame) Clone() Frame { return f }

func frameFingerprint(f Frame) string {
	value := struct {
		SchemaVersion            SchemaVersion `json:"schema_version"`
		ObservationID            string        `json:"observation_id"`
		ObservedAt               time.Time     `json:"observed_at"`
		TopologyRevision         string        `json:"topology_revision"`
		NodeID, ParentID, ZoneID string
		NodeKind                 NodeKind
		EntryPoint, Exterior     bool
		Occupancy                OccupancyState
		HouseMode                HouseMode
		Time                     TemporalContext
		Quality                  ContextQuality
		SnapshotFingerprint      string
		FreshnessCode            string
	}{f.SchemaVersion, f.ObservationID, f.ObservedAt, f.TopologyRevision, f.NodeID, f.ParentID, f.ZoneID, f.NodeKind, f.EntryPoint, f.Exterior, f.Occupancy, f.HouseMode, f.Time, f.Quality, f.SnapshotFingerprint, f.FreshnessCode}
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type Provider interface {
	Resolve(stdcontext.Context, string, time.Time, string) (Frame, error)
}

type StaticProvider struct {
	Topology     TopologySnapshot
	Timezone     string
	Occupancy    OccupancyState
	HouseMode    HouseMode
	AllowPartial bool
}

func (p StaticProvider) Resolve(ctx stdcontext.Context, id string, at time.Time, nodeID string) (Frame, error) {
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	return ResolveFrame(ResolveInput{ObservationID: id, ObservedAt: at, NodeID: nodeID, Timezone: p.Timezone, Occupancy: p.Occupancy, HouseMode: p.HouseMode, Topology: p.Topology, AllowPartial: p.AllowPartial})
}

func FrameSignature(f Frame) (Signature, error) {
	if err := f.Validate(); err != nil {
		return Signature{}, err
	}
	s := Signature{SchemaVersion: f.SchemaVersion, NodeID: f.NodeID, ZoneID: f.ZoneID, NodeKind: f.NodeKind, EntryPoint: f.EntryPoint, Exterior: f.Exterior, Occupancy: f.Occupancy, HouseMode: f.HouseMode, Weekday: f.Time.Weekday, DayPart: f.Time.DayPart, TimeBucket: f.Time.MinuteOfDay / 15}
	payload, _ := json.Marshal(s)
	digest := sha256.Sum256(payload)
	s.Fingerprint = "sha256:" + hex.EncodeToString(digest[:])
	return s, nil
}

type Signature struct {
	SchemaVersion SchemaVersion  `json:"schema_version"`
	NodeID        string         `json:"node_id,omitempty"`
	ZoneID        string         `json:"zone_id,omitempty"`
	NodeKind      NodeKind       `json:"node_kind"`
	EntryPoint    bool           `json:"entry_point,omitempty"`
	Exterior      bool           `json:"exterior,omitempty"`
	Occupancy     OccupancyState `json:"occupancy"`
	HouseMode     HouseMode      `json:"house_mode"`
	Weekday       time.Weekday   `json:"weekday"`
	DayPart       DayPart        `json:"day_part"`
	TimeBucket    int            `json:"time_bucket"`
	Fingerprint   string         `json:"fingerprint"`
}
