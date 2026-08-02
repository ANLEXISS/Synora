package situationfacts

import (
	"sort"

	"synora/internal/cge/episodes"
)

func (b *extractionBuilder) extractMemory() error {
	episode := b.input.Episode
	subject := FactSubject{Kind: "episode", ID: string(episode.ID), Role: "memory"}
	prov := b.observationProvenance()
	if err := b.add(CodeMemoryChainRefCount, ScopeMemory, subject, "", IntFactValue(int64(len(episode.ChainRefs))), OriginCarried, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeMemoryRoutineRefCount, ScopeMemory, subject, "", IntFactValue(int64(len(episode.RoutineRefs))), OriginCarried, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	deviations := []*episodes.DeviationRef{}
	for _, observation := range episode.Observations {
		if observation.Deviation != nil {
			deviations = append(deviations, observation.Deviation)
		}
	}
	memoryProvenance := deviationProvenance(episode)
	if len(memoryProvenance) == 0 {
		memoryProvenance = []ProvenanceRef{episodeProvenance(episode)}
	}
	present := len(deviations) > 0
	evaluated, structural, temporal, interval := false, false, false, false
	statuses, bands := []string{}, []string{}
	maxScore, maxCoverage := int64(0), int64(0)
	for _, deviation := range deviations {
		statuses = append(statuses, deviation.Status)
		bands = append(bands, deviation.Band)
		if deviation.Status != "" || deviation.AssessmentID != "" {
			evaluated = true
		}
		structural = structural || deviation.StructuralAvailable
		temporal = temporal || deviation.TemporalAvailable
		interval = interval || deviation.IntervalAvailable
		if int64(deviation.ScorePermille) > maxScore {
			maxScore = int64(deviation.ScorePermille)
		}
		if int64(deviation.CoveragePermille) > maxCoverage {
			maxCoverage = int64(deviation.CoveragePermille)
		}
	}
	statuses, bands = uniqueSorted(statuses), uniqueSorted(bands)
	items := []struct {
		code   FactCode
		value  FactValue
		origin FactOrigin
	}{{CodeMemoryDeviationPresent, BoolFactValue(present), OriginCarried}, {CodeMemoryDeviationEvaluated, BoolFactValue(evaluated), OriginCarried}, {CodeMemoryDeviationStatusSet, StringSetFactValue(statuses), OriginCarried}, {CodeMemoryDeviationBandSet, StringSetFactValue(bands), OriginCarried}, {CodeMemoryDeviationMaximumScore, PermilleFactValue(maxScore), OriginCarried}, {CodeMemoryDeviationMaximumCoverage, PermilleFactValue(maxCoverage), OriginCarried}, {CodeMemoryDeviationStructuralAvailable, BoolFactValue(structural), OriginCarried}, {CodeMemoryDeviationTemporalAvailable, BoolFactValue(temporal), OriginCarried}, {CodeMemoryDeviationIntervalAvailable, BoolFactValue(interval), OriginCarried}, {CodeMemoryDeviationStructuralPositive, BoolFactValue(structural && maxScore > 0), OriginCarried}, {CodeMemoryDeviationTemporalPositive, BoolFactValue(temporal && maxScore > 0), OriginCarried}, {CodeMemoryDeviationIntervalPositive, BoolFactValue(interval && maxScore > 0), OriginCarried}}
	for _, item := range items {
		if err := b.add(item.code, ScopeMemory, subject, "", item.value, item.origin, StatusAsserted, episode.StartedAt, memoryProvenance, false); err != nil {
			return err
		}
	}
	return nil
}

func deviationProvenance(episode episodes.EpisodeSnapshot) []ProvenanceRef {
	out := []ProvenanceRef{}
	for _, observation := range episode.Observations {
		if observation.Deviation == nil {
			continue
		}
		id := observation.Deviation.AssessmentID
		if id == "" {
			id = observation.EventID
		}
		out = append(out, ProvenanceRef{SourceKind: "deviation_assessment", SourceID: id, SourceRevision: 1, ObservedAt: observation.ObservedAt, AlgorithmID: "carried-deviation-reference", AlgorithmVersion: "v1"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	return canonicalProvenance(out)
}
