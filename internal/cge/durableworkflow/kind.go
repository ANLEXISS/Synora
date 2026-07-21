package durableworkflow

type LayerKind string

const (
	LayerEpisode                LayerKind = "episode"
	LayerSituationFacts         LayerKind = "situation_facts"
	LayerSituationHypotheses    LayerKind = "situation_hypotheses"
	LayerEvidenceDiscrimination LayerKind = "evidence_discrimination"
	LayerAdvisoryRequests       LayerKind = "advisory_requests"
	LayerCapabilityMapping      LayerKind = "capability_mapping"
	LayerAuthorizationBoundary  LayerKind = "authorization_boundary"
)

type LayerFreshness string

const (
	FreshnessAbsent      LayerFreshness = "absent"
	FreshnessFresh       LayerFreshness = "fresh"
	FreshnessStale       LayerFreshness = "stale"
	FreshnessInvalidated LayerFreshness = "invalidated"
)

var layerOrder = []LayerKind{
	LayerEpisode,
	LayerSituationFacts,
	LayerSituationHypotheses,
	LayerEvidenceDiscrimination,
	LayerAdvisoryRequests,
	LayerCapabilityMapping,
	LayerAuthorizationBoundary,
}

func Layers() []LayerKind { return append([]LayerKind(nil), layerOrder...) }

func validLayer(value LayerKind) bool {
	for _, layer := range layerOrder {
		if layer == value {
			return true
		}
	}
	return false
}

func validFreshness(value LayerFreshness) bool {
	return value == FreshnessAbsent || value == FreshnessFresh || value == FreshnessStale || value == FreshnessInvalidated
}
