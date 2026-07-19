package routines

import (
	"fmt"

	"synora/internal/cge/context"
)

type Kind string

const (
	KindPresence   Kind = "context_presence"
	KindTransition Kind = "context_transition"
)

type PresencePattern struct {
	ContextSchemaVersion context.SchemaVersion  `json:"context_schema_version"`
	NodeID               string                 `json:"node_id,omitempty"`
	ZoneID               string                 `json:"zone_id,omitempty"`
	NodeKind             context.NodeKind       `json:"node_kind"`
	EntryPoint           bool                   `json:"entry_point,omitempty"`
	Exterior             bool                   `json:"exterior,omitempty"`
	Occupancy            context.OccupancyState `json:"occupancy"`
	HouseMode            context.HouseMode      `json:"house_mode"`
}

type TransitionPattern struct {
	ContextSchemaVersion context.SchemaVersion  `json:"context_schema_version"`
	FromNodeID           string                 `json:"from_node_id,omitempty"`
	ToNodeID             string                 `json:"to_node_id,omitempty"`
	FromZoneID           string                 `json:"from_zone_id,omitempty"`
	ToZoneID             string                 `json:"to_zone_id,omitempty"`
	FromNodeKind         context.NodeKind       `json:"from_node_kind"`
	ToNodeKind           context.NodeKind       `json:"to_node_kind"`
	EntryTransition      bool                   `json:"entry_transition,omitempty"`
	ExitTransition       bool                   `json:"exit_transition,omitempty"`
	ExteriorTransition   bool                   `json:"exterior_transition,omitempty"`
	Adjacent             bool                   `json:"adjacent,omitempty"`
	GraphDistanceKnown   bool                   `json:"graph_distance_known,omitempty"`
	GraphDistance        int                    `json:"graph_distance,omitempty"`
	OccupancyBefore      context.OccupancyState `json:"occupancy_before"`
	OccupancyAfter       context.OccupancyState `json:"occupancy_after"`
	HouseModeBefore      context.HouseMode      `json:"house_mode_before"`
	HouseModeAfter       context.HouseMode      `json:"house_mode_after"`
}

type Pattern struct {
	Kind       Kind               `json:"kind"`
	Presence   *PresencePattern   `json:"presence,omitempty"`
	Transition *TransitionPattern `json:"transition,omitempty"`
}

func (p Pattern) Validate() error {
	switch p.Kind {
	case KindPresence:
		if p.Presence == nil || p.Transition != nil {
			return fmt.Errorf("%w: presence union", ErrInvalidPattern)
		}
		return validatePresencePattern(*p.Presence)
	case KindTransition:
		if p.Transition == nil || p.Presence != nil {
			return fmt.Errorf("%w: transition union", ErrInvalidPattern)
		}
		return validateTransitionPattern(*p.Transition)
	default:
		return fmt.Errorf("%w: kind %q", ErrInvalidPattern, p.Kind)
	}
}

func validatePresencePattern(p PresencePattern) error {
	if p.ContextSchemaVersion != context.SchemaVersionCurrent || !validNodeKind(p.NodeKind) || !validOccupancy(p.Occupancy) || !validHouseMode(p.HouseMode) || !validOptionalText(p.NodeID) || !validOptionalText(p.ZoneID) {
		return fmt.Errorf("%w: presence fields", ErrInvalidPattern)
	}
	return nil
}

func validateTransitionPattern(p TransitionPattern) error {
	if p.ContextSchemaVersion != context.SchemaVersionCurrent || !validNodeKind(p.FromNodeKind) || !validNodeKind(p.ToNodeKind) || !validOccupancy(p.OccupancyBefore) || !validOccupancy(p.OccupancyAfter) || !validHouseMode(p.HouseModeBefore) || !validHouseMode(p.HouseModeAfter) || !validOptionalText(p.FromNodeID) || !validOptionalText(p.ToNodeID) || !validOptionalText(p.FromZoneID) || !validOptionalText(p.ToZoneID) {
		return fmt.Errorf("%w: transition fields", ErrInvalidPattern)
	}
	if p.GraphDistanceKnown && p.GraphDistance < 0 {
		return fmt.Errorf("%w: negative graph distance", ErrInvalidPattern)
	}
	return nil
}

func patternKey(p Pattern) string { return canonicalJSON(p) }

func validNodeKind(v context.NodeKind) bool {
	return v == context.NodeUnknown || v == context.NodeRoom || v == context.NodeCorridor || v == context.NodeEntrance || v == context.NodeExit || v == context.NodeStairs || v == context.NodeGarage || v == context.NodeGarden || v == context.NodeExterior
}
func validOccupancy(v context.OccupancyState) bool {
	return v == context.OccupancyUnknown || v == context.OccupancyUnoccupied || v == context.OccupancyOccupied
}
func validHouseMode(v context.HouseMode) bool {
	return v == context.HouseModeUnknown || v == context.HouseModeHome || v == context.HouseModeAway || v == context.HouseModeNight || v == context.HouseModeSleep || v == context.HouseModeArmed
}
