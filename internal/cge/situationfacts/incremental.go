package situationfacts

import (
	"reflect"
	"sort"
	"time"

	"synora/internal/cge/episodes"
)

type IncrementalMode string

const (
	IncrementalModeIncremental  IncrementalMode = "incremental"
	IncrementalModeFullFallback IncrementalMode = "full_fallback"
	IncrementalModeIdempotent   IncrementalMode = "idempotent"
)

type IncrementalExtractionInput struct {
	PreviousEpisode episodes.EpisodeSnapshot
	CurrentEpisode  episodes.EpisodeSnapshot

	PreviousFactSet FactSet

	Topology    TopologyView
	ExtractedAt time.Time
}

type IncrementalExtractionResult struct {
	FactSet FactSet
	Diff    FactSetDiff

	Mode           IncrementalMode
	FallbackReason string

	ReusedFactCount     int
	RecomputedFactCount int
}

type incrementalAggregate struct {
	knownIDs     map[string]struct{}
	candidateIDs map[string]struct{}
	kinds        map[episodes.SubjectKind]bool
	trackKnown   map[string]map[string]struct{}

	stateChanged     bool
	identityConflict bool

	nodes                  []string
	zones                  []string
	transitionCount        int
	reachableCount         int
	unreachableCount       int
	unknownTransitionCount int

	dayparts   map[string]struct{}
	weekdays   map[string]struct{}
	minimumGap int64
	maximumGap int64
	averageGap int64
	outOfOrder bool

	houseModes        map[string]struct{}
	occupancies       map[string]struct{}
	complete          int
	partial           int
	missing           int
	houseModeChanged  bool
	occupancyChanged  bool
	houseModeConflict bool
	occupancyConflict bool

	activations            map[string]struct{}
	tracks                 map[string]struct{}
	sequences              map[string]struct{}
	clips                  map[string]struct{}
	trackNodes             map[string]map[string]struct{}
	hasSharedActivation    bool
	hasSharedTrack         bool
	hasSharedSequence      bool
	multipleNodesSameTrack bool

	deviations          []*episodes.DeviationRef
	deviationEvaluated  bool
	deviationStatuses   []string
	deviationBands      []string
	maximumScore        int64
	maximumCoverage     int64
	structuralAvailable bool
	temporalAvailable   bool
	intervalAvailable   bool
}

func ExtractIncremental(input IncrementalExtractionInput, policy Policy) (IncrementalExtractionResult, error) {
	if err := policy.Validate(); err != nil {
		return IncrementalExtractionResult{}, err
	}
	if input.CurrentEpisode.ID == "" {
		return IncrementalExtractionResult{}, ErrMissingEpisodeID
	}
	if input.CurrentEpisode.Revision == 0 {
		return IncrementalExtractionResult{}, ErrMissingEpisodeRevision
	}
	if input.ExtractedAt.IsZero() {
		return IncrementalExtractionResult{}, ErrInvalidFactSet
	}
	if err := input.CurrentEpisode.Validate(); err != nil {
		return IncrementalExtractionResult{}, err
	}
	if validator, ok := input.Topology.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return IncrementalExtractionResult{}, err
		}
	}

	previousValid := input.PreviousEpisode.Validate() == nil && validPreviousFactSetFast(input.PreviousFactSet, input.PreviousEpisode, policy)
	if input.PreviousEpisode.ID != input.CurrentEpisode.ID {
		return incrementalFallback(input, policy, "episode_id_changed", false)
	}
	if !previousValid {
		return incrementalFallback(input, policy, "previous_fact_set_invalid", false)
	}
	if input.PreviousFactSet.EpisodeID != input.PreviousEpisode.ID || input.PreviousFactSet.EpisodeRevision != input.PreviousEpisode.Revision {
		return incrementalFallback(input, policy, "previous_revision_mismatch", true)
	}
	if input.PreviousFactSet.SchemaFingerprint != SchemaFingerprint() || input.PreviousFactSet.PolicyFingerprint != policy.Fingerprint() {
		return incrementalFallback(input, policy, "fingerprint_incompatible", true)
	}
	if input.CurrentEpisode.Revision < input.PreviousEpisode.Revision {
		return incrementalFallback(input, policy, "revision_decreased", true)
	}
	if input.CurrentEpisode.Revision == input.PreviousEpisode.Revision {
		if reflect.DeepEqual(input.PreviousEpisode, input.CurrentEpisode) && input.PreviousFactSet.ExtractedAt.Equal(input.ExtractedAt.UTC().Round(0)) {
			set := input.PreviousFactSet.Clone()
			return IncrementalExtractionResult{FactSet: set, Diff: emptyDiff(set), Mode: IncrementalModeIdempotent, ReusedFactCount: len(set.Facts)}, nil
		}
		return incrementalFallback(input, policy, "same_revision_content_changed", true)
	}
	if len(input.CurrentEpisode.Observations) < len(input.PreviousEpisode.Observations) {
		return incrementalFallback(input, policy, "observations_not_appended", true)
	}
	if len(input.CurrentEpisode.Observations) == len(input.PreviousEpisode.Observations) {
		return incrementalFallback(input, policy, "historical_observation_changed", true)
	}
	if !reflect.DeepEqual(input.CurrentEpisode.Observations[:len(input.PreviousEpisode.Observations)], input.PreviousEpisode.Observations) {
		return incrementalFallback(input, policy, "historical_observation_changed", true)
	}
	// A non-nil topology is deliberately not assumed immutable through an
	// interface. Rebuilding is safer than using an unverified relationship.
	if input.Topology != nil {
		return incrementalFallback(input, policy, "topology_identity_unproven", true)
	}

	set, reused, recomputed, err := extractIncrementalAppend(input, policy)
	if err != nil {
		return IncrementalExtractionResult{}, err
	}
	diff, err := diffTrusted(input.PreviousFactSet, set)
	if err != nil {
		return IncrementalExtractionResult{}, err
	}
	return IncrementalExtractionResult{FactSet: set, Diff: diff, Mode: IncrementalModeIncremental, ReusedFactCount: reused, RecomputedFactCount: recomputed}, nil
}

func validPreviousFactSetFast(set FactSet, episode episodes.EpisodeSnapshot, policy Policy) bool {
	if set.EpisodeID == "" || set.EpisodeID != episode.ID || set.EpisodeRevision == 0 || set.EpisodeRevision != episode.Revision || set.SchemaFingerprint != SchemaFingerprint() || set.PolicyFingerprint != policy.Fingerprint() || set.Fingerprint == "" {
		return false
	}
	return FactSetFingerprint(set) == set.Fingerprint
}

func incrementalFallback(input IncrementalExtractionInput, policy Policy, reason string, previousValid bool) (IncrementalExtractionResult, error) {
	set, err := Extract(ExtractionInput{Episode: input.CurrentEpisode, Topology: input.Topology, ExtractedAt: input.ExtractedAt}, policy)
	if err != nil {
		return IncrementalExtractionResult{}, err
	}
	result := IncrementalExtractionResult{FactSet: set, Mode: IncrementalModeFullFallback, FallbackReason: reason, RecomputedFactCount: len(set.Facts)}
	if previousValid {
		diff, diffErr := Diff(input.PreviousFactSet, set)
		if diffErr != nil {
			return IncrementalExtractionResult{}, diffErr
		}
		result.Diff = diff
	}
	return result, nil
}

func extractIncrementalAppend(input IncrementalExtractionInput, policy Policy) (FactSet, int, int, error) {
	current := input.CurrentEpisode
	builder := &extractionBuilder{input: ExtractionInput{Episode: current, ExtractedAt: input.ExtractedAt}, policy: policy, schema: compiledSchema(), drafts: make(map[string][]factDraft, estimateFactCapacity(current, policy))}
	aggregate := collectIncrementalAggregate(current)
	prov := builder.observationProvenance()
	episodeSubject := episodeSubject(current.ID)
	addEpisode := func(code FactCode, value FactValue, validFrom time.Time) error {
		return builder.add(code, ScopeEpisode, episodeSubject, "", value, OriginDerived, StatusAsserted, validFrom, []ProvenanceRef{episodeProvenance(current)}, false)
	}
	for _, item := range []struct {
		code  FactCode
		value FactValue
	}{
		{CodeEpisodeStatus, StringFactValue(string(current.Status))},
		{CodeEpisodeObservationCount, IntFactValue(int64(len(current.Observations)))},
		{CodeEpisodeDurationMS, DurationMSFactValue(current.DurationObserved.Milliseconds())},
		{CodeEpisodeEntityCount, IntFactValue(int64(len(current.Subjects)))},
		{CodeEpisodeNodeCount, IntFactValue(int64(len(current.Nodes)))},
		{CodeEpisodeZoneCount, IntFactValue(int64(uniqueZoneCount(current.Nodes)))},
		{CodeEpisodeChainCount, IntFactValue(int64(len(current.ChainRefs)))},
		{CodeEpisodeRoutineCount, IntFactValue(int64(len(current.RoutineRefs)))},
		{CodeEpisodeEventTypeSet, StringSetFactValue(current.EventTypes)},
		{CodeEpisodeContextQualitySet, StringSetFactValue(current.ContextQualities)},
		{CodeEpisodeMultipleObservations, BoolFactValue(len(current.Observations) > 1)},
		{CodeEpisodeMultipleEntities, BoolFactValue(len(current.Subjects) > 1)},
		{CodeEpisodeStartedAt, TimestampFactValue(current.StartedAt)},
		{CodeEpisodeLastObservedAt, TimestampFactValue(current.LastObservedAt)},
	} {
		if err := addEpisode(item.code, item.value, current.StartedAt); err != nil {
			return FactSet{}, 0, 0, err
		}
	}

	entitySubject := episodeSubject
	for kind, code := range map[episodes.SubjectKind]FactCode{episodes.SubjectKnown: CodeIdentityKnownPresent, episodes.SubjectUnknown: CodeIdentityUnknownPresent, episodes.SubjectUncertain: CodeIdentityUncertainPresent, episodes.SubjectNone: CodeIdentityNonePresent} {
		if err := builder.add(code, ScopeEntity, entitySubject, string(kind), BoolFactValue(aggregate.kinds[kind]), OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	}
	if err := builder.add(CodeIdentityKnownEntitySet, ScopeEntity, entitySubject, "", StringSetFactValue(mapKeys(aggregate.knownIDs)), OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
		return FactSet{}, 0, 0, err
	}
	if err := builder.add(CodeIdentityCandidateEntitySet, ScopeEntity, entitySubject, "", StringSetFactValue(mapKeys(aggregate.candidateIDs)), OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
		return FactSet{}, 0, 0, err
	}
	if err := builder.add(CodeIdentityMultipleKnownEntities, ScopeEntity, entitySubject, "", BoolFactValue(len(aggregate.knownIDs) > 1), OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
		return FactSet{}, 0, 0, err
	}
	if err := builder.add(CodeIdentityStateChanged, ScopeEntity, entitySubject, "", BoolFactValue(aggregate.stateChanged), OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
		return FactSet{}, 0, 0, err
	}
	if err := builder.add(CodeIdentityConflict, ScopeEntity, entitySubject, "", BoolFactValue(aggregate.identityConflict), OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
		return FactSet{}, 0, 0, err
	}
	for track, ids := range aggregate.trackKnown {
		if len(ids) < 2 {
			continue
		}
		for id := range ids {
			if err := builder.add(CodeIdentityKnownEntitySet, ScopeEntity, FactSubject{Kind: "track", ID: track, Role: "identity"}, "track", StringSetFactValue([]string{id}), OriginObserved, StatusAsserted, current.StartedAt, prov, false); err != nil {
				return FactSet{}, 0, 0, err
			}
		}
	}

	spatialSubject := episodeSubject
	if len(aggregate.nodes) > 0 {
		if err := builder.add(CodeSpatialStartNode, ScopeTransition, spatialSubject, "", StringFactValue(aggregate.nodes[0]), OriginObserved, StatusAsserted, current.StartedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
		if err := builder.add(CodeSpatialEndNode, ScopeTransition, spatialSubject, "", StringFactValue(aggregate.nodes[len(aggregate.nodes)-1]), OriginObserved, StatusAsserted, current.LastObservedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	} else if policy.IncludeUnknownFacts {
		if err := builder.addUnknown(CodeSpatialStartNode, ScopeTransition, spatialSubject, "", current.StartedAt, prov); err != nil {
			return FactSet{}, 0, 0, err
		}
		if err := builder.addUnknown(CodeSpatialEndNode, ScopeTransition, spatialSubject, "", current.LastObservedAt, prov); err != nil {
			return FactSet{}, 0, 0, err
		}
	}
	for _, item := range []struct {
		code  FactCode
		value FactValue
	}{
		{CodeSpatialNodeSequence, StringListFactValue(aggregate.nodes)},
		{CodeSpatialZoneSequence, StringListFactValue(aggregate.zones)},
		{CodeSpatialTransitionCount, IntFactValue(int64(aggregate.transitionCount))},
		{CodeSpatialReachableTransitionCount, IntFactValue(int64(aggregate.reachableCount))},
		{CodeSpatialUnreachableTransitionCount, IntFactValue(int64(aggregate.unreachableCount))},
		{CodeSpatialUnknownTransitionCount, IntFactValue(int64(aggregate.unknownTransitionCount))},
		{CodeSpatialTopologyAvailable, BoolFactValue(false)},
	} {
		if err := builder.add(item.code, ScopeTransition, spatialSubject, "", item.value, OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	}

	for _, item := range []struct {
		code  FactCode
		value FactValue
	}{
		{CodeTemporalDurationMS, DurationMSFactValue(current.DurationObserved.Milliseconds())},
		{CodeTemporalOutOfOrderPresent, BoolFactValue(aggregate.outOfOrder)},
		{CodeTemporalDaypartSet, StringSetFactValue(mapKeys(aggregate.dayparts))},
		{CodeTemporalWeekdaySet, StringSetFactValue(mapKeys(aggregate.weekdays))},
		{CodeTemporalMinimumGapMS, DurationMSFactValue(aggregate.minimumGap)},
		{CodeTemporalMaximumGapMS, DurationMSFactValue(aggregate.maximumGap)},
		{CodeTemporalAverageGapMS, DurationMSFactValue(aggregate.averageGap)},
	} {
		if err := builder.add(item.code, ScopeEpisode, episodeSubject, "", item.value, OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	}

	contextSubject := FactSubject{Kind: "episode", ID: string(current.ID), Role: "context"}
	partial := aggregate.partial > 0
	for _, item := range []struct {
		code    FactCode
		value   FactValue
		origin  FactOrigin
		partial bool
	}{
		{CodeContextHouseModeSet, StringSetFactValue(mapKeys(aggregate.houseModes)), OriginObserved, partial},
		{CodeContextOccupancySet, StringSetFactValue(mapKeys(aggregate.occupancies)), OriginObserved, partial},
		{CodeContextHouseModeChanged, BoolFactValue(aggregate.houseModeChanged), OriginDerived, partial},
		{CodeContextOccupancyChanged, BoolFactValue(aggregate.occupancyChanged), OriginDerived, partial},
		{CodeContextHouseModeConflict, BoolFactValue(aggregate.houseModeConflict), OriginDerived, false},
		{CodeContextOccupancyConflict, BoolFactValue(aggregate.occupancyConflict), OriginDerived, false},
		{CodeContextCompleteCount, IntFactValue(int64(aggregate.complete)), OriginDerived, partial},
		{CodeContextPartialCount, IntFactValue(int64(aggregate.partial)), OriginDerived, partial},
		{CodeContextMissingCount, IntFactValue(int64(aggregate.missing)), OriginDerived, partial},
		{CodeContextPartialPresent, BoolFactValue(partial), OriginDerived, false},
		{CodeContextMissingPresent, BoolFactValue(aggregate.missing > 0 || len(aggregate.houseModes) == 0 && len(aggregate.occupancies) == 0), OriginDerived, false},
	} {
		if err := builder.add(item.code, ScopeContext, contextSubject, "", item.value, item.origin, StatusAsserted, current.StartedAt, prov, item.partial); err != nil {
			return FactSet{}, 0, 0, err
		}
	}
	if err := addIncrementalContextConflicts(builder, current, aggregate, prov); err != nil {
		return FactSet{}, 0, 0, err
	}

	continuitySubject := episodeSubject
	for _, item := range []struct {
		code  FactCode
		value FactValue
	}{
		{CodeContinuityActivationCount, IntFactValue(int64(len(aggregate.activations)))},
		{CodeContinuityTrackCount, IntFactValue(int64(len(aggregate.tracks)))},
		{CodeContinuitySequenceCount, IntFactValue(int64(len(aggregate.sequences)))},
		{CodeContinuityMultipleClips, IntFactValue(int64(len(aggregate.clips)))},
		{CodeContinuitySharedActivation, BoolFactValue(aggregate.hasSharedActivation)},
		{CodeContinuitySharedTrack, BoolFactValue(aggregate.hasSharedTrack)},
		{CodeContinuitySharedSequence, BoolFactValue(aggregate.hasSharedSequence)},
		{CodeContinuityMultipleNodesSameTrack, BoolFactValue(aggregate.multipleNodesSameTrack)},
	} {
		if err := builder.add(item.code, ScopeObservation, continuitySubject, "", item.value, OriginDerived, StatusAsserted, current.StartedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	}

	memorySubject := FactSubject{Kind: "episode", ID: string(current.ID), Role: "memory"}
	memoryProv := deviationProvenance(current)
	if len(memoryProv) == 0 {
		memoryProv = []ProvenanceRef{episodeProvenance(current)}
	}
	for _, item := range []struct {
		code  FactCode
		value FactValue
	}{
		{CodeMemoryChainRefCount, IntFactValue(int64(len(current.ChainRefs)))},
		{CodeMemoryRoutineRefCount, IntFactValue(int64(len(current.RoutineRefs)))},
	} {
		if err := builder.add(item.code, ScopeMemory, memorySubject, "", item.value, OriginCarried, StatusAsserted, current.StartedAt, prov, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	}
	for _, item := range []struct {
		code  FactCode
		value FactValue
	}{
		{CodeMemoryDeviationPresent, BoolFactValue(len(aggregate.deviations) > 0)},
		{CodeMemoryDeviationEvaluated, BoolFactValue(aggregate.deviationEvaluated)},
		{CodeMemoryDeviationStatusSet, StringSetFactValue(aggregate.deviationStatuses)},
		{CodeMemoryDeviationBandSet, StringSetFactValue(aggregate.deviationBands)},
		{CodeMemoryDeviationMaximumScore, PermilleFactValue(aggregate.maximumScore)},
		{CodeMemoryDeviationMaximumCoverage, PermilleFactValue(aggregate.maximumCoverage)},
		{CodeMemoryDeviationStructuralAvailable, BoolFactValue(aggregate.structuralAvailable)},
		{CodeMemoryDeviationTemporalAvailable, BoolFactValue(aggregate.temporalAvailable)},
		{CodeMemoryDeviationIntervalAvailable, BoolFactValue(aggregate.intervalAvailable)},
		{CodeMemoryDeviationStructuralPositive, BoolFactValue(aggregate.structuralAvailable && aggregate.maximumScore > 0)},
		{CodeMemoryDeviationTemporalPositive, BoolFactValue(aggregate.temporalAvailable && aggregate.maximumScore > 0)},
		{CodeMemoryDeviationIntervalPositive, BoolFactValue(aggregate.intervalAvailable && aggregate.maximumScore > 0)},
	} {
		if err := builder.add(item.code, ScopeMemory, memorySubject, "", item.value, OriginCarried, StatusAsserted, current.StartedAt, memoryProv, false); err != nil {
			return FactSet{}, 0, 0, err
		}
	}

	set, err := builder.finish()
	if err != nil {
		return FactSet{}, 0, 0, err
	}
	return set, 0, len(set.Facts), nil
}

func collectIncrementalAggregate(episode episodes.EpisodeSnapshot) incrementalAggregate {
	aggregate := incrementalAggregate{knownIDs: map[string]struct{}{}, candidateIDs: map[string]struct{}{}, kinds: map[episodes.SubjectKind]bool{}, trackKnown: map[string]map[string]struct{}{}, dayparts: map[string]struct{}{}, weekdays: map[string]struct{}{}, houseModes: map[string]struct{}{}, occupancies: map[string]struct{}{}, activations: map[string]struct{}{}, tracks: map[string]struct{}{}, sequences: map[string]struct{}{}, clips: map[string]struct{}{}, trackNodes: map[string]map[string]struct{}{}}
	var gapSum int64
	for i, observation := range episode.Observations {
		kind := observation.Subject.Kind
		aggregate.kinds[kind] = true
		if kind == episodes.SubjectKnown {
			aggregate.knownIDs[observation.Subject.EntityID] = struct{}{}
		}
		for _, id := range observation.Subject.CandidateEntityIDs {
			aggregate.candidateIDs[id] = struct{}{}
		}
		if observation.TrackID != "" && kind == episodes.SubjectKnown {
			if aggregate.trackKnown[observation.TrackID] == nil {
				aggregate.trackKnown[observation.TrackID] = map[string]struct{}{}
			}
			aggregate.trackKnown[observation.TrackID][observation.Subject.EntityID] = struct{}{}
		}
		if i > 0 && (observation.Subject.Kind != episode.Observations[i-1].Subject.Kind || observation.Subject.EntityID != episode.Observations[i-1].Subject.EntityID) {
			aggregate.stateChanged = true
		}
		if observation.NodeID != "" {
			aggregate.nodes = append(aggregate.nodes, observation.NodeID)
		}
		if observation.ZoneID != "" {
			aggregate.zones = append(aggregate.zones, observation.ZoneID)
		}
		if observation.ActivationID != "" {
			aggregate.activations[observation.ActivationID] = struct{}{}
		}
		if observation.TrackID != "" {
			aggregate.tracks[observation.TrackID] = struct{}{}
			if aggregate.trackNodes[observation.TrackID] == nil {
				aggregate.trackNodes[observation.TrackID] = map[string]struct{}{}
			}
			if observation.NodeID != "" {
				aggregate.trackNodes[observation.TrackID][observation.NodeID] = struct{}{}
			}
		}
		if observation.SequenceKey != "" {
			aggregate.sequences[observation.SequenceKey] = struct{}{}
		}
		if observation.ClipID != "" {
			aggregate.clips[observation.ClipID] = struct{}{}
		}
		at := observation.ObservedAt.UTC()
		aggregate.dayparts[daypart(at)] = struct{}{}
		aggregate.weekdays[at.Weekday().String()] = struct{}{}
		if observation.HouseMode != "" {
			aggregate.houseModes[observation.HouseMode] = struct{}{}
		}
		if observation.Occupancy != "" {
			aggregate.occupancies[observation.Occupancy] = struct{}{}
		}
		switch observation.ContextQuality {
		case "complete":
			aggregate.complete++
		case "partial":
			aggregate.partial++
		default:
			aggregate.missing++
		}
		if observation.Deviation != nil {
			aggregate.deviations = append(aggregate.deviations, observation.Deviation)
			if observation.Deviation.Status != "" || observation.Deviation.AssessmentID != "" {
				aggregate.deviationEvaluated = true
			}
			aggregate.structuralAvailable = aggregate.structuralAvailable || observation.Deviation.StructuralAvailable
			aggregate.temporalAvailable = aggregate.temporalAvailable || observation.Deviation.TemporalAvailable
			aggregate.intervalAvailable = aggregate.intervalAvailable || observation.Deviation.IntervalAvailable
			if int64(observation.Deviation.ScorePermille) > aggregate.maximumScore {
				aggregate.maximumScore = int64(observation.Deviation.ScorePermille)
			}
			if int64(observation.Deviation.CoveragePermille) > aggregate.maximumCoverage {
				aggregate.maximumCoverage = int64(observation.Deviation.CoveragePermille)
			}
			if observation.Deviation.Status != "" {
				aggregate.deviationStatuses = append(aggregate.deviationStatuses, observation.Deviation.Status)
			}
			if observation.Deviation.Band != "" {
				aggregate.deviationBands = append(aggregate.deviationBands, observation.Deviation.Band)
			}
		}
		if i > 0 {
			previous := episode.Observations[i-1]
			gap := observation.ObservedAt.UTC().Sub(previous.ObservedAt.UTC()).Milliseconds()
			if previous.ReceivedAt.IsZero() == false && observation.ReceivedAt.IsZero() == false && observation.ReceivedAt.Before(previous.ReceivedAt) {
				aggregate.outOfOrder = true
			}
			if gap < 0 {
				aggregate.outOfOrder = true
				gap = 0
			}
			if i == 1 || gap < aggregate.minimumGap {
				aggregate.minimumGap = gap
			}
			if gap > aggregate.maximumGap {
				aggregate.maximumGap = gap
			}
			gapSum += gap
			if previous.NodeID != observation.NodeID || previous.NodeID == "" {
				if previous.NodeID == "" || observation.NodeID == "" {
					aggregate.unknownTransitionCount++
				} else {
					aggregate.transitionCount++
					aggregate.unknownTransitionCount++
				}
			}
		}
	}
	if len(episode.Observations) > 1 {
		aggregate.averageGap = gapSum / int64(len(episode.Observations)-1)
	}
	for track, ids := range aggregate.trackKnown {
		if len(ids) > 1 {
			aggregate.identityConflict = true
		}
		_ = track
	}
	for track, nodes := range aggregate.trackNodes {
		if len(nodes) > 1 {
			aggregate.multipleNodesSameTrack = true
		}
		_ = track
	}
	aggregate.hasSharedActivation = hasMapDuplicate(aggregate.activations, episode.Observations, 0)
	aggregate.hasSharedTrack = hasMapDuplicate(aggregate.tracks, episode.Observations, 1)
	aggregate.hasSharedSequence = hasMapDuplicate(aggregate.sequences, episode.Observations, 2)
	aggregate.houseModeChanged = changedHouseMode(episode)
	aggregate.occupancyChanged = changedOccupancy(episode)
	for i, left := range episode.Observations {
		for _, right := range episode.Observations[i+1:] {
			if !left.ObservedAt.Equal(right.ObservedAt) || left.TrackID == "" || left.TrackID != right.TrackID {
				continue
			}
			if left.HouseMode != "" && right.HouseMode != "" && left.HouseMode != right.HouseMode {
				aggregate.houseModeConflict = true
			}
			if left.Occupancy != "" && right.Occupancy != "" && left.Occupancy != right.Occupancy {
				aggregate.occupancyConflict = true
			}
		}
	}
	aggregate.deviationStatuses = uniqueSorted(aggregate.deviationStatuses)
	aggregate.deviationBands = uniqueSorted(aggregate.deviationBands)
	return aggregate
}

func addIncrementalContextConflicts(builder *extractionBuilder, episode episodes.EpisodeSnapshot, aggregate incrementalAggregate, provenance []ProvenanceRef) error {
	for i, left := range episode.Observations {
		for _, right := range episode.Observations[i+1:] {
			if !left.ObservedAt.Equal(right.ObservedAt) || left.TrackID == "" || left.TrackID != right.TrackID {
				continue
			}
			for _, item := range []struct {
				code        FactCode
				left, right string
			}{{CodeContextHouseModeSet, left.HouseMode, right.HouseMode}, {CodeContextOccupancySet, left.Occupancy, right.Occupancy}} {
				if item.left == "" || item.right == "" || item.left == item.right {
					continue
				}
				subject := FactSubject{Kind: "track", ID: left.TrackID, Role: "context"}
				if err := builder.add(item.code, ScopeContext, subject, "instant", StringSetFactValue([]string{item.left}), OriginObserved, StatusAsserted, left.ObservedAt, provenance, false); err != nil {
					return err
				}
				if err := builder.add(item.code, ScopeContext, subject, "instant", StringSetFactValue([]string{item.right}), OriginObserved, StatusAsserted, right.ObservedAt, provenance, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func uniqueZoneCount(nodes []episodes.NodeRef) int {
	values := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.ZoneID != "" {
			values[node.ZoneID] = struct{}{}
		}
	}
	return len(values)
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasMapDuplicate(values map[string]struct{}, observations []episodes.ObservationRef, kind int) bool {
	counts := make(map[string]int, len(values))
	for _, observation := range observations {
		var value string
		switch kind {
		case 0:
			value = observation.ActivationID
		case 1:
			value = observation.TrackID
		case 2:
			value = observation.SequenceKey
		}
		if value != "" {
			counts[value]++
		}
	}
	for value := range values {
		if counts[value] > 1 {
			return true
		}
	}
	return false
}
