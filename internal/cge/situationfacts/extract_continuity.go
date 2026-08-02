package situationfacts

import "synora/internal/cge/episodes"

func (b *extractionBuilder) extractContinuity() error {
	episode := b.input.Episode
	subject := episodeSubject(episode.ID)
	prov := b.observationProvenance()
	activations, tracks, sequences, clips := []string{}, []string{}, []string{}, []string{}
	trackNodes := map[string]map[string]struct{}{}
	for _, observation := range episode.Observations {
		if observation.ActivationID != "" {
			activations = append(activations, observation.ActivationID)
		}
		if observation.TrackID != "" {
			tracks = append(tracks, observation.TrackID)
			if trackNodes[observation.TrackID] == nil {
				trackNodes[observation.TrackID] = map[string]struct{}{}
			}
			if observation.NodeID != "" {
				trackNodes[observation.TrackID][observation.NodeID] = struct{}{}
			}
		}
		if observation.SequenceKey != "" {
			sequences = append(sequences, observation.SequenceKey)
		}
		if observation.ClipID != "" {
			clips = append(clips, observation.ClipID)
		}
	}
	activations, tracks, sequences, clips = uniqueSorted(activations), uniqueSorted(tracks), uniqueSorted(sequences), uniqueSorted(clips)
	counts := []struct {
		code  FactCode
		value int
	}{{CodeContinuityActivationCount, len(activations)}, {CodeContinuityTrackCount, len(tracks)}, {CodeContinuitySequenceCount, len(sequences)}, {CodeContinuityMultipleClips, len(clips)}}
	for _, item := range counts {
		if err := b.add(item.code, ScopeObservation, subject, "", IntFactValue(int64(item.value)), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
			return err
		}
	}
	if err := b.add(CodeContinuitySharedActivation, ScopeObservation, subject, "", BoolFactValue(hasDuplicate(activations, episode.Observations, 0)), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeContinuitySharedTrack, ScopeObservation, subject, "", BoolFactValue(hasDuplicate(tracks, episode.Observations, 1)), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeContinuitySharedSequence, ScopeObservation, subject, "", BoolFactValue(hasDuplicate(sequences, episode.Observations, 2)), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	multipleNodes := false
	for _, nodes := range trackNodes {
		if len(nodes) > 1 {
			multipleNodes = true
			break
		}
	}
	return b.add(CodeContinuityMultipleNodesSameTrack, ScopeObservation, subject, "", BoolFactValue(multipleNodes), OriginDerived, StatusAsserted, episode.StartedAt, prov, false)
}

func hasDuplicate(values []string, observations []episodes.ObservationRef, kind int) bool {
	counts := map[string]int{}
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
	for _, value := range values {
		if counts[value] > 1 {
			return true
		}
	}
	return false
}
