package situationfacts

import "synora/internal/cge/episodes"

func (b *extractionBuilder) extractSpatial() error {
	episode := b.input.Episode
	subject := episodeSubject(episode.ID)
	prov := b.observationProvenance()
	nodes, zones := make([]string, 0, len(episode.Observations)), make([]string, 0, len(episode.Observations))
	for _, observation := range episode.Observations {
		if observation.NodeID != "" {
			nodes = append(nodes, observation.NodeID)
		}
		if observation.ZoneID != "" {
			zones = append(zones, observation.ZoneID)
		}
	}
	if len(nodes) > 0 {
		if err := b.add(CodeSpatialStartNode, ScopeTransition, subject, "", StringFactValue(nodes[0]), OriginObserved, StatusAsserted, episode.StartedAt, prov, false); err != nil {
			return err
		}
		if err := b.add(CodeSpatialEndNode, ScopeTransition, subject, "", StringFactValue(nodes[len(nodes)-1]), OriginObserved, StatusAsserted, episode.LastObservedAt, prov, false); err != nil {
			return err
		}
	}
	if len(nodes) == 0 && b.policy.IncludeUnknownFacts {
		if err := b.addUnknown(CodeSpatialStartNode, ScopeTransition, subject, "", episode.StartedAt, prov); err != nil {
			return err
		}
		if err := b.addUnknown(CodeSpatialEndNode, ScopeTransition, subject, "", episode.LastObservedAt, prov); err != nil {
			return err
		}
	}
	if err := b.add(CodeSpatialNodeSequence, ScopeTransition, subject, "", StringListFactValue(nodes), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeSpatialZoneSequence, ScopeTransition, subject, "", StringListFactValue(zones), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	transitionCount, reachable, unreachable, unknown := 0, 0, 0, 0
	for i := 1; i < len(episode.Observations); i++ {
		from, to := episode.Observations[i-1].NodeID, episode.Observations[i].NodeID
		if from == to && from != "" {
			continue
		}
		if from == "" || to == "" {
			unknown++
			continue
		}
		transitionCount++
		if b.input.Topology == nil {
			unknown++
			continue
		}
		switch b.input.Topology.Relationship(from, to) {
		case episodes.TopologySame, episodes.TopologyAdjacent, episodes.TopologyReachable:
			reachable++
		case episodes.TopologyUnreachable:
			unreachable++
		default:
			unknown++
		}
	}
	for _, pair := range []struct {
		code  FactCode
		value int
	}{{CodeSpatialTransitionCount, transitionCount}, {CodeSpatialReachableTransitionCount, reachable}, {CodeSpatialUnreachableTransitionCount, unreachable}, {CodeSpatialUnknownTransitionCount, unknown}} {
		if err := b.add(pair.code, ScopeTransition, subject, "", IntFactValue(int64(pair.value)), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
			return err
		}
	}
	return b.add(CodeSpatialTopologyAvailable, ScopeTransition, subject, "", BoolFactValue(b.input.Topology != nil), OriginDerived, StatusAsserted, episode.StartedAt, prov, false)
}
