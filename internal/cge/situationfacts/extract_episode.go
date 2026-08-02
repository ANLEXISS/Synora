package situationfacts

func (b *extractionBuilder) extractEpisode() error {
	episode := b.input.Episode
	subject := episodeSubject(episode.ID)
	prov := []ProvenanceRef{episodeProvenance(episode)}
	start := episode.StartedAt
	add := func(code FactCode, value FactValue) error {
		return b.add(code, ScopeEpisode, subject, "", value, OriginDerived, StatusAsserted, start, prov, false)
	}
	if err := add(CodeEpisodeStatus, StringFactValue(string(episode.Status))); err != nil {
		return err
	}
	if err := add(CodeEpisodeObservationCount, IntFactValue(int64(len(episode.Observations)))); err != nil {
		return err
	}
	if err := add(CodeEpisodeDurationMS, DurationMSFactValue(episode.DurationObserved.Milliseconds())); err != nil {
		return err
	}
	if err := add(CodeEpisodeEntityCount, IntFactValue(int64(len(episode.Subjects)))); err != nil {
		return err
	}
	if err := add(CodeEpisodeNodeCount, IntFactValue(int64(len(episode.Nodes)))); err != nil {
		return err
	}
	zoneSet := make([]string, 0)
	for _, node := range episode.Nodes {
		if node.ZoneID != "" {
			zoneSet = append(zoneSet, node.ZoneID)
		}
	}
	if err := add(CodeEpisodeZoneCount, IntFactValue(int64(len(uniqueSorted(zoneSet))))); err != nil {
		return err
	}
	if err := add(CodeEpisodeChainCount, IntFactValue(int64(len(episode.ChainRefs)))); err != nil {
		return err
	}
	if err := add(CodeEpisodeRoutineCount, IntFactValue(int64(len(episode.RoutineRefs)))); err != nil {
		return err
	}
	eventTypes := append([]string(nil), episode.EventTypes...)
	if err := add(CodeEpisodeEventTypeSet, StringSetFactValue(eventTypes)); err != nil {
		return err
	}
	qualities := append([]string(nil), episode.ContextQualities...)
	if err := add(CodeEpisodeContextQualitySet, StringSetFactValue(qualities)); err != nil {
		return err
	}
	if err := add(CodeEpisodeMultipleObservations, BoolFactValue(len(episode.Observations) > 1)); err != nil {
		return err
	}
	if err := add(CodeEpisodeMultipleEntities, BoolFactValue(len(episode.Subjects) > 1)); err != nil {
		return err
	}
	if err := add(CodeEpisodeStartedAt, TimestampFactValue(episode.StartedAt)); err != nil {
		return err
	}
	return add(CodeEpisodeLastObservedAt, TimestampFactValue(episode.LastObservedAt))
}
