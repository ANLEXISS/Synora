package situationfacts

import "synora/internal/cge/episodes"

func (b *extractionBuilder) extractContext() error {
	episode := b.input.Episode
	subject := FactSubject{Kind: "episode", ID: string(episode.ID), Role: "context"}
	prov := b.observationProvenance()
	houseModes, occupancies := []string{}, []string{}
	complete, partial, missing := 0, 0, 0
	for _, observation := range episode.Observations {
		if observation.HouseMode != "" {
			houseModes = append(houseModes, observation.HouseMode)
		}
		if observation.Occupancy != "" {
			occupancies = append(occupancies, observation.Occupancy)
		}
		switch observation.ContextQuality {
		case "complete":
			complete++
		case "partial":
			partial++
		default:
			missing++
		}
	}
	houseModes, occupancies = uniqueSorted(houseModes), uniqueSorted(occupancies)
	if err := b.add(CodeContextHouseModeSet, ScopeContext, subject, "", StringSetFactValue(houseModes), OriginObserved, StatusAsserted, episode.StartedAt, prov, partial > 0); err != nil {
		return err
	}
	if err := b.add(CodeContextOccupancySet, ScopeContext, subject, "", StringSetFactValue(occupancies), OriginObserved, StatusAsserted, episode.StartedAt, prov, partial > 0); err != nil {
		return err
	}
	if err := b.add(CodeContextHouseModeChanged, ScopeContext, subject, "", BoolFactValue(changedHouseMode(episode)), OriginDerived, StatusAsserted, episode.StartedAt, prov, partial > 0); err != nil {
		return err
	}
	if err := b.add(CodeContextOccupancyChanged, ScopeContext, subject, "", BoolFactValue(changedOccupancy(episode)), OriginDerived, StatusAsserted, episode.StartedAt, prov, partial > 0); err != nil {
		return err
	}
	houseConflict := sameInstantConflict(episode, true)
	occupancyConflict := sameInstantConflict(episode, false)
	if err := addInstantContextConflicts(b, episode, true, prov); err != nil {
		return err
	}
	if err := addInstantContextConflicts(b, episode, false, prov); err != nil {
		return err
	}
	if err := b.add(CodeContextHouseModeConflict, ScopeContext, subject, "", BoolFactValue(houseConflict), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeContextOccupancyConflict, ScopeContext, subject, "", BoolFactValue(occupancyConflict), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	for _, item := range []struct {
		code  FactCode
		value int
	}{{CodeContextCompleteCount, complete}, {CodeContextPartialCount, partial}, {CodeContextMissingCount, missing}} {
		if err := b.add(item.code, ScopeContext, subject, "", IntFactValue(int64(item.value)), OriginDerived, StatusAsserted, episode.StartedAt, prov, partial > 0); err != nil {
			return err
		}
	}
	if err := b.add(CodeContextPartialPresent, ScopeContext, subject, "", BoolFactValue(partial > 0), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	return b.add(CodeContextMissingPresent, ScopeContext, subject, "", BoolFactValue(missing > 0 || len(houseModes) == 0 && len(occupancies) == 0), OriginDerived, StatusAsserted, episode.StartedAt, prov, false)
}

func addInstantContextConflicts(b *extractionBuilder, episode episodes.EpisodeSnapshot, house bool, provenance []ProvenanceRef) error {
	for i, left := range episode.Observations {
		for _, right := range episode.Observations[i+1:] {
			if !left.ObservedAt.Equal(right.ObservedAt) || left.TrackID == "" || left.TrackID != right.TrackID {
				continue
			}
			leftValue, rightValue := left.HouseMode, right.HouseMode
			code := CodeContextHouseModeSet
			if !house {
				leftValue, rightValue, code = left.Occupancy, right.Occupancy, CodeContextOccupancySet
			}
			if leftValue == "" || rightValue == "" || leftValue == rightValue {
				continue
			}
			subject := FactSubject{Kind: "track", ID: left.TrackID, Role: "context"}
			if err := b.add(code, ScopeContext, subject, "instant", StringSetFactValue([]string{leftValue}), OriginObserved, StatusAsserted, left.ObservedAt, provenance, false); err != nil {
				return err
			}
			if err := b.add(code, ScopeContext, subject, "instant", StringSetFactValue([]string{rightValue}), OriginObserved, StatusAsserted, right.ObservedAt, provenance, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func changedHouseMode(episode episodes.EpisodeSnapshot) bool {
	last := ""
	changed := false
	for _, observation := range episode.Observations {
		if observation.HouseMode == "" {
			continue
		}
		if last != "" && last != observation.HouseMode {
			changed = true
		}
		last = observation.HouseMode
	}
	return changed
}
func changedOccupancy(episode episodes.EpisodeSnapshot) bool {
	last := ""
	changed := false
	for _, observation := range episode.Observations {
		if observation.Occupancy == "" {
			continue
		}
		if last != "" && last != observation.Occupancy {
			changed = true
		}
		last = observation.Occupancy
	}
	return changed
}
func sameInstantConflict(episode episodes.EpisodeSnapshot, house bool) bool {
	for i, left := range episode.Observations {
		for _, right := range episode.Observations[i+1:] {
			if !left.ObservedAt.Equal(right.ObservedAt) || left.TrackID == "" || left.TrackID != right.TrackID {
				continue
			}
			if house && left.HouseMode != "" && right.HouseMode != "" && left.HouseMode != right.HouseMode {
				return true
			}
			if !house && left.Occupancy != "" && right.Occupancy != "" && left.Occupancy != right.Occupancy {
				return true
			}
		}
	}
	return false
}
