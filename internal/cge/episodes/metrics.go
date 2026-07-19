package episodes

type Metrics struct {
	EpisodeCount       int
	OpenCount          int
	QuiescentCount     int
	ClosedCount        int
	ObservationCount   int
	AmbiguousPlanCount uint64
	DuplicateCount     uint64
	RejectedCount      uint64
}

func MetricsForSnapshot(snapshot Snapshot) Metrics {
	value := Metrics{EpisodeCount: len(snapshot.Episodes)}
	for _, episode := range snapshot.Episodes {
		value.ObservationCount += len(episode.Observations)
		switch episode.Status {
		case StatusOpen:
			value.OpenCount++
		case StatusQuiescent:
			value.QuiescentCount++
		case StatusClosed:
			value.ClosedCount++
		}
	}
	return value
}

func (r *Registry) Metrics() Metrics {
	if r == nil {
		return Metrics{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value := MetricsForSnapshot(Snapshot{Episodes: sortedEpisodes(mapEpisodes(r.episodes))})
	value.AmbiguousPlanCount, value.DuplicateCount, value.RejectedCount = r.metrics.AmbiguousPlanCount, r.metrics.DuplicateCount, r.metrics.RejectedCount
	return value
}

func mapEpisodes(values map[EpisodeID]Episode) []EpisodeSnapshot {
	out := make([]EpisodeSnapshot, 0, len(values))
	for _, value := range values {
		out = append(out, value.Clone())
	}
	return out
}
