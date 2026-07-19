package deviation

import (
	"testing"

	"synora/internal/cge/context"
	"synora/internal/cge/routines"
)

func TestPresenceStructuralWeightsAndUnknowns(t *testing.T) {
	base := routines.PresencePattern{ContextSchemaVersion: context.SchemaVersionV1, NodeID: "room", ZoneID: "ground", NodeKind: context.NodeRoom, Occupancy: context.OccupancyOccupied, HouseMode: context.HouseModeHome}
	if factor := ComparePresencePattern(base, base); !factor.Available || factor.Score != 0 {
		t.Fatalf("expected exact presence factor: %+v", factor)
	}
	other := base
	other.ZoneID = "upper"
	factor := ComparePresencePattern(base, other)
	if factor.Score == 0 || factor.Score > MaxScore || len(factor.ReasonCodes) == 0 {
		t.Fatalf("unexpected mismatch factor: %+v", factor)
	}
	unknown := routines.PresencePattern{ContextSchemaVersion: context.SchemaVersionV1, NodeKind: context.NodeUnknown, Occupancy: context.OccupancyUnknown, HouseMode: context.HouseModeUnknown}
	if factor := ComparePresencePattern(unknown, unknown); !factor.Available || factor.Score != 0 {
		t.Fatalf("expected boolean dimensions to remain comparable: %+v", factor)
	}
}

func TestTransitionDistanceInterpolation(t *testing.T) {
	base := routines.TransitionPattern{ContextSchemaVersion: context.SchemaVersionV1, FromNodeID: "a", ToNodeID: "b", FromZoneID: "z", ToZoneID: "z", FromNodeKind: context.NodeRoom, ToNodeKind: context.NodeCorridor, Adjacent: true, GraphDistanceKnown: true, GraphDistance: 1, OccupancyBefore: context.OccupancyOccupied, OccupancyAfter: context.OccupancyOccupied, HouseModeBefore: context.HouseModeHome, HouseModeAfter: context.HouseModeHome}
	near := base
	near.GraphDistance = 2
	far := base
	far.GraphDistance = 4
	nearFactor := CompareTransitionPattern(base, near)
	farFactor := CompareTransitionPattern(base, far)
	if nearFactor.Score <= 0 || nearFactor.Score >= farFactor.Score || farFactor.Score == 0 {
		t.Fatalf("expected deterministic distance interpolation: near=%+v far=%+v", nearFactor, farFactor)
	}
}

func TestTransitionInverseIsDifferent(t *testing.T) {
	forward := routines.TransitionPattern{ContextSchemaVersion: context.SchemaVersionV1, FromNodeID: "entry", ToNodeID: "corridor", FromZoneID: "ground", ToZoneID: "ground", FromNodeKind: context.NodeEntrance, ToNodeKind: context.NodeCorridor, EntryTransition: true, Adjacent: true, GraphDistanceKnown: true, GraphDistance: 1, OccupancyBefore: context.OccupancyOccupied, OccupancyAfter: context.OccupancyOccupied, HouseModeBefore: context.HouseModeHome, HouseModeAfter: context.HouseModeHome}
	inverse := forward
	inverse.FromNodeID, inverse.ToNodeID = inverse.ToNodeID, inverse.FromNodeID
	inverse.FromNodeKind, inverse.ToNodeKind = inverse.ToNodeKind, inverse.FromNodeKind
	inverse.EntryTransition = false
	inverse.ExitTransition = true
	factor := CompareTransitionPattern(forward, inverse)
	if !factor.Available || factor.Score == 0 {
		t.Fatalf("expected inverse transition mismatch: %+v", factor)
	}
}
