package deviation

import (
	"reflect"
	"sort"

	"synora/internal/cge/context"
	"synora/internal/cge/routines"
)

func ComparePresencePattern(observed, baseline routines.PresencePattern) Factor {
	fields := []struct {
		knownObserved bool
		knownBaseline bool
		equal         bool
		weight        Score
		matchCode     string
		mismatchCode  string
	}{
		{observed.NodeID != "", baseline.NodeID != "", observed.NodeID == baseline.NodeID, 350, "presence.node_match", "presence.node_mismatch"},
		{observed.ZoneID != "", baseline.ZoneID != "", observed.ZoneID == baseline.ZoneID, 200, "presence.zone_match", "presence.zone_mismatch"},
		{observed.NodeKind != context.NodeUnknown, baseline.NodeKind != context.NodeUnknown, observed.NodeKind == baseline.NodeKind, 100, "presence.node_kind_match", "presence.node_kind_mismatch"},
		{true, true, observed.EntryPoint == baseline.EntryPoint, 50, "presence.entry_match", "presence.entry_mismatch"},
		{true, true, observed.Exterior == baseline.Exterior, 50, "presence.exterior_match", "presence.exterior_mismatch"},
		{observed.Occupancy != context.OccupancyUnknown, baseline.Occupancy != context.OccupancyUnknown, observed.Occupancy == baseline.Occupancy, 100, "presence.occupancy_match", "presence.occupancy_mismatch"},
		{observed.HouseMode != context.HouseModeUnknown, baseline.HouseMode != context.HouseModeUnknown, observed.HouseMode == baseline.HouseMode, 150, "presence.house_mode_match", "presence.house_mode_mismatch"},
	}
	return factorFromFields(FactorStructural, fields)
}

func CompareTransitionPattern(observed, baseline routines.TransitionPattern) Factor {
	fields := []struct {
		knownObserved bool
		knownBaseline bool
		equal         bool
		weight        Score
		matchCode     string
		mismatchCode  string
	}{
		{observed.FromNodeID != "", baseline.FromNodeID != "", observed.FromNodeID == baseline.FromNodeID, 120, "transition.from_node_match", "transition.from_node_mismatch"},
		{observed.ToNodeID != "", baseline.ToNodeID != "", observed.ToNodeID == baseline.ToNodeID, 120, "transition.to_node_match", "transition.to_node_mismatch"},
		{observed.FromZoneID != "", baseline.FromZoneID != "", observed.FromZoneID == baseline.FromZoneID, 80, "transition.from_zone_match", "transition.from_zone_mismatch"},
		{observed.ToZoneID != "", baseline.ToZoneID != "", observed.ToZoneID == baseline.ToZoneID, 80, "transition.to_zone_match", "transition.to_zone_mismatch"},
		{observed.FromNodeKind != context.NodeUnknown, baseline.FromNodeKind != context.NodeUnknown, observed.FromNodeKind == baseline.FromNodeKind, 40, "transition.from_kind_match", "transition.from_kind_mismatch"},
		{observed.ToNodeKind != context.NodeUnknown, baseline.ToNodeKind != context.NodeUnknown, observed.ToNodeKind == baseline.ToNodeKind, 40, "transition.to_kind_match", "transition.to_kind_mismatch"},
		{true, true, observed.EntryTransition == baseline.EntryTransition, 50, "transition.entry_match", "transition.entry_mismatch"},
		{true, true, observed.ExitTransition == baseline.ExitTransition, 50, "transition.exit_match", "transition.exit_mismatch"},
		{true, true, observed.ExteriorTransition == baseline.ExteriorTransition, 50, "transition.exterior_match", "transition.exterior_mismatch"},
		{true, true, observed.Adjacent == baseline.Adjacent, 50, "transition.adjacent_match", "transition.adjacent_mismatch"},
		{observed.OccupancyBefore != context.OccupancyUnknown, baseline.OccupancyBefore != context.OccupancyUnknown, observed.OccupancyBefore == baseline.OccupancyBefore, 40, "transition.occupancy_before_match", "transition.occupancy_before_mismatch"},
		{observed.OccupancyAfter != context.OccupancyUnknown, baseline.OccupancyAfter != context.OccupancyUnknown, observed.OccupancyAfter == baseline.OccupancyAfter, 40, "transition.occupancy_after_match", "transition.occupancy_after_mismatch"},
		{observed.HouseModeBefore != context.HouseModeUnknown, baseline.HouseModeBefore != context.HouseModeUnknown, observed.HouseModeBefore == baseline.HouseModeBefore, 80, "transition.house_mode_before_match", "transition.house_mode_before_mismatch"},
		{observed.HouseModeAfter != context.HouseModeUnknown, baseline.HouseModeAfter != context.HouseModeUnknown, observed.HouseModeAfter == baseline.HouseModeAfter, 80, "transition.house_mode_after_match", "transition.house_mode_after_mismatch"},
	}
	var available, mismatch int64
	var codes []string
	for _, field := range fields {
		if !field.knownObserved || !field.knownBaseline {
			continue
		}
		available += int64(field.weight)
		if field.equal {
			codes = append(codes, field.matchCode)
		} else {
			mismatch += int64(field.weight)
			codes = append(codes, field.mismatchCode)
		}
	}
	if observed.GraphDistanceKnown && baseline.GraphDistanceKnown {
		available += 80
		distance := observed.GraphDistance - baseline.GraphDistance
		if distance < 0 {
			distance = -distance
		}
		if distance == 0 {
			codes = append(codes, "transition.distance_match")
		} else {
			if distance > 3 {
				distance = 3
			}
			mismatch += int64(80*distance) / 3
			codes = append(codes, "transition.distance_mismatch")
		}
	}
	if available == 0 {
		return Factor{Kind: FactorStructural, Available: false, ReasonCodes: []string{"structural.comparison_partial"}}
	}
	if mismatch == 0 {
		codes = append(codes, "structural.exact")
	}
	sort.Strings(codes)
	return Factor{Kind: FactorStructural, Available: true, Score: roundedRatio(mismatch, available), ReasonCodes: uniqueCodes(codes)}
}

func factorFromFields(kind FactorKind, fields []struct {
	knownObserved bool
	knownBaseline bool
	equal         bool
	weight        Score
	matchCode     string
	mismatchCode  string
}) Factor {
	var available, mismatch int64
	var codes []string
	for _, field := range fields {
		if !field.knownObserved || !field.knownBaseline {
			continue
		}
		available += int64(field.weight)
		if field.equal {
			codes = append(codes, field.matchCode)
		} else {
			mismatch += int64(field.weight)
			codes = append(codes, field.mismatchCode)
		}
	}
	if available == 0 {
		return Factor{Kind: kind, Available: false, ReasonCodes: []string{string(kind) + ".comparison_partial"}}
	}
	if mismatch == 0 {
		codes = append(codes, string(kind)+".exact")
	}
	sort.Strings(codes)
	return Factor{Kind: kind, Available: true, Score: roundedRatio(mismatch, available), ReasonCodes: uniqueCodes(codes)}
}

func exactPattern(a, b routines.Pattern) bool { return reflect.DeepEqual(a, b) }

func patternFactor(observed, baseline routines.Pattern) (Factor, bool, error) {
	if observed.Kind != baseline.Kind {
		return Factor{}, false, ErrDeviationKindMismatch
	}
	switch observed.Kind {
	case routines.KindPresence:
		if observed.Presence == nil || baseline.Presence == nil {
			return Factor{}, false, ErrInvalidDeviationOccurrence
		}
		return ComparePresencePattern(*observed.Presence, *baseline.Presence), exactPattern(observed, baseline), nil
	case routines.KindTransition:
		if observed.Transition == nil || baseline.Transition == nil {
			return Factor{}, false, ErrInvalidDeviationOccurrence
		}
		return CompareTransitionPattern(*observed.Transition, *baseline.Transition), exactPattern(observed, baseline), nil
	default:
		return Factor{}, false, ErrInvalidDeviationOccurrence
	}
}

func uniqueCodes(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
