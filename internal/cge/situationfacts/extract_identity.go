package situationfacts

import "synora/internal/cge/episodes"

func (b *extractionBuilder) extractIdentity() error {
	episode := b.input.Episode
	knownIDs, candidateIDs := []string{}, []string{}
	kinds := map[episodes.SubjectKind]bool{}
	trackKnown := map[string]map[string]struct{}{}
	for _, observation := range episode.Observations {
		kind := observation.Subject.Kind
		kinds[kind] = true
		if kind == episodes.SubjectKnown {
			knownIDs = append(knownIDs, observation.Subject.EntityID)
		}
		candidateIDs = append(candidateIDs, observation.Subject.CandidateEntityIDs...)
		if observation.TrackID != "" && kind == episodes.SubjectKnown {
			if trackKnown[observation.TrackID] == nil {
				trackKnown[observation.TrackID] = map[string]struct{}{}
			}
			trackKnown[observation.TrackID][observation.Subject.EntityID] = struct{}{}
		}
	}
	subject := episodeSubject(episode.ID)
	prov := b.observationProvenance()
	for kind, code := range map[episodes.SubjectKind]FactCode{episodes.SubjectKnown: CodeIdentityKnownPresent, episodes.SubjectUnknown: CodeIdentityUnknownPresent, episodes.SubjectUncertain: CodeIdentityUncertainPresent, episodes.SubjectNone: CodeIdentityNonePresent} {
		if err := b.add(code, ScopeEntity, subject, string(kind), BoolFactValue(kinds[kind]), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
			return err
		}
	}
	if err := b.add(CodeIdentityKnownEntitySet, ScopeEntity, subject, "", StringSetFactValue(knownIDs), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeIdentityCandidateEntitySet, ScopeEntity, subject, "", StringSetFactValue(candidateIDs), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeIdentityMultipleKnownEntities, ScopeEntity, subject, "", BoolFactValue(len(uniqueSorted(knownIDs)) > 1), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	stateChanged := false
	conflict := false
	for track, ids := range trackKnown {
		if len(ids) > 1 {
			conflict = true
		}
		_ = track
	}
	for i := 1; i < len(episode.Observations); i++ {
		if episode.Observations[i].Subject.Kind != episode.Observations[i-1].Subject.Kind || episode.Observations[i].Subject.EntityID != episode.Observations[i-1].Subject.EntityID {
			stateChanged = true
			break
		}
	}
	if err := b.add(CodeIdentityStateChanged, ScopeEntity, subject, "", BoolFactValue(stateChanged), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	if err := b.add(CodeIdentityConflict, ScopeEntity, subject, "", BoolFactValue(conflict), OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
		return err
	}
	// Preserve same-track identity contradictions as a conflict set without
	// choosing one of the identities.
	for track, ids := range trackKnown {
		if len(ids) < 2 {
			continue
		}
		for id := range ids {
			if err := b.add(CodeIdentityKnownEntitySet, ScopeEntity, FactSubject{Kind: "track", ID: track, Role: "identity"}, "track", StringSetFactValue([]string{id}), OriginObserved, StatusAsserted, episode.StartedAt, prov, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func observationProvenance(values []episodes.ObservationRef) []ProvenanceRef {
	out := make([]ProvenanceRef, 0, len(values))
	for _, value := range values {
		out = append(out, obsProvenance(value))
	}
	return canonicalProvenance(out)
}
