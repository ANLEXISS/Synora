package episodes

import (
	"math"
	"sort"
)

type FactorAssessment struct {
	Code       string
	Available  bool
	ScoreDelta int
	HardReject bool
}

type CandidateAssessment struct {
	EpisodeID       EpisodeID
	Eligible        bool
	Score           int
	Reasons         []FactorAssessment
	EpisodeRevision uint64
}

func addFactor(reasons *[]FactorAssessment, code string, available bool, delta int, reject bool) {
	*reasons = append(*reasons, FactorAssessment{Code: code, Available: available, ScoreDelta: delta, HardReject: reject})
}

func hasSubjectInEpisode(episode EpisodeSnapshot, subject SubjectRef) bool {
	for _, value := range episode.Subjects {
		if value.Kind == subject.Kind && value.EntityID == subject.EntityID {
			return true
		}
	}
	return false
}

func candidateEntityOverlap(left, right SubjectRef) bool {
	if left.Kind == SubjectKnown && right.Kind == SubjectKnown {
		return left.EntityID == right.EntityID
	}
	if left.Kind == SubjectKnown && right.Kind == SubjectUncertain {
		return contains(right.CandidateEntityIDs, left.EntityID)
	}
	if right.Kind == SubjectKnown && left.Kind == SubjectUncertain {
		return contains(left.CandidateEntityIDs, right.EntityID)
	}
	for _, a := range left.CandidateEntityIDs {
		if contains(right.CandidateEntityIDs, a) {
			return true
		}
	}
	return false
}

func contains(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	return index < len(values) && values[index] == target
}

func temporalMatch(episode EpisodeSnapshot, observation ObservationRef, policy Policy) (track, activation, sequence bool, before, afterMax, outOfWindow bool) {
	for _, existing := range episode.Observations {
		gap := existing.ObservedAt.Sub(observation.ObservedAt)
		if gap < 0 {
			gap = -gap
		}
		if observation.TrackID != "" && existing.TrackID == observation.TrackID {
			if gap <= policy.SameTrackMaxGap {
				track = true
			} else {
				outOfWindow = true
			}
		}
		if observation.ActivationID != "" && existing.ActivationID == observation.ActivationID {
			if gap <= policy.SameActivationMaxGap {
				activation = true
			} else {
				outOfWindow = true
			}
		}
		if observation.SequenceKey != "" && existing.SequenceKey == observation.SequenceKey && gap <= policy.SameActivationMaxGap {
			sequence = true
		}
	}
	if observation.ObservedAt.Before(episode.StartedAt) {
		before = true
		if episode.StartedAt.Sub(observation.ObservedAt) > policy.LateObservationGrace {
			outOfWindow = true
		}
	}
	if observation.ObservedAt.Sub(episode.StartedAt) > policy.MaxEpisodeDuration || episode.LastObservedAt.Sub(observation.ObservedAt) > policy.MaxEpisodeDuration {
		afterMax = true
		outOfWindow = true
	}
	return
}

func subjectKindForEpisode(episode EpisodeSnapshot) SubjectKind {
	if len(episode.Subjects) == 0 {
		return SubjectNone
	}
	first := episode.Subjects[0].Kind
	for _, subject := range episode.Subjects[1:] {
		if subject.Kind != first {
			return SubjectUncertain
		}
	}
	return first
}

func scoreCandidate(episode EpisodeSnapshot, observation ObservationRef, topology TopologyView, policy Policy) CandidateAssessment {
	assessment := CandidateAssessment{EpisodeID: episode.ID, Eligible: true, EpisodeRevision: episode.Revision}
	score := 300
	hardReject := false
	reject := func(code string) { addFactor(&assessment.Reasons, code, true, 0, true); hardReject = true }
	if episode.Status == StatusClosed || episode.Status == StatusInvalidated {
		reject("status.not_modifiable")
	}
	if observation.Subject.Kind == SubjectKnown {
		for _, subject := range episode.Subjects {
			if subject.Kind == SubjectKnown && subject.EntityID != observation.Subject.EntityID {
				reject("subject.different_known")
				break
			}
		}
	}
	track, activation, sequence, before, afterMax, outOfWindow := temporalMatch(episode, observation, policy)
	if track {
		addFactor(&assessment.Reasons, "track.same", true, 400, false)
	} else if observation.TrackID != "" {
		addFactor(&assessment.Reasons, "track.missing_or_different", false, 0, false)
	}
	if activation {
		addFactor(&assessment.Reasons, "activation.same", true, 300, false)
	} else if observation.ActivationID != "" {
		addFactor(&assessment.Reasons, "activation.missing_or_different", false, 0, false)
	}
	if sequence {
		addFactor(&assessment.Reasons, "sequence.same", true, 120, false)
	} else if observation.SequenceKey != "" {
		addFactor(&assessment.Reasons, "sequence.missing_or_different", false, 0, false)
	}
	if observation.ClipID != "" {
		related := false
		for _, existing := range episode.Observations {
			if existing.ClipID == observation.ClipID {
				related = true
				break
			}
		}
		if related {
			addFactor(&assessment.Reasons, "clip.related", true, 50, false)
		} else {
			addFactor(&assessment.Reasons, "clip.different", true, -20, false)
		}
	}
	if before {
		addFactor(&assessment.Reasons, "time.before_episode", true, -20, false)
	}
	if afterMax {
		addFactor(&assessment.Reasons, "time.after_max_duration", true, 0, true)
		hardReject = true
	}
	if outOfWindow {
		addFactor(&assessment.Reasons, "time.out_of_window", true, 0, true)
		hardReject = true
	} else if track {
		addFactor(&assessment.Reasons, "time.same_track_window", true, 100, false)
	} else if activation {
		addFactor(&assessment.Reasons, "time.same_activation_window", true, 70, false)
	} else {
		gap := episode.LastObservedAt.Sub(observation.ObservedAt)
		if gap < 0 {
			gap = -gap
		}
		maxGap := policy.SameSubjectMaxGap
		if observation.Subject.Kind == SubjectUnknown || observation.Subject.Kind == SubjectNone {
			maxGap = policy.UnknownSubjectMaxGap
		}
		if gap <= maxGap {
			addFactor(&assessment.Reasons, "time.same_subject_window", true, 40, false)
		} else {
			addFactor(&assessment.Reasons, "time.out_of_window", true, 0, true)
			hardReject = true
		}
	}

	knownOverlap := false
	for _, subject := range episode.Subjects {
		if candidateEntityOverlap(subject, observation.Subject) {
			knownOverlap = true
			break
		}
	}
	switch {
	case observation.Subject.Kind == SubjectKnown && knownOverlap:
		addFactor(&assessment.Reasons, "subject.same_known", true, 250, false)
	case observation.Subject.Kind == SubjectKnown:
		// Different known identities were rejected above; an episode without an
		// identity can still be joined when technical continuity is strong.
		if track || activation {
			addFactor(&assessment.Reasons, "subject.missing", false, 0, false)
		} else {
			addFactor(&assessment.Reasons, "subject.unknown_compatible", true, 20, false)
		}
	case observation.Subject.Kind == SubjectUncertain && knownOverlap:
		addFactor(&assessment.Reasons, "subject.uncertain_overlap", true, 180, false)
	case observation.Subject.Kind == SubjectUnknown || observation.Subject.Kind == SubjectNone:
		if subjectKindForEpisode(episode) == SubjectUnknown || subjectKindForEpisode(episode) == SubjectNone {
			addFactor(&assessment.Reasons, "subject.unknown_compatible", true, 80, false)
		} else {
			addFactor(&assessment.Reasons, "subject.missing", false, 0, false)
		}
	default:
		addFactor(&assessment.Reasons, "subject.missing", false, 0, false)
	}
	if observation.Subject.Kind == SubjectUncertain && len(observation.Subject.CandidateEntityIDs) > 0 && !knownOverlap {
		reject("subject.uncertain_no_overlap")
	}

	spaceSame, spaceZone, spaceReachable, spaceUnreachable := false, false, false, false
	for _, node := range episode.Nodes {
		if observation.NodeID != "" && node.ID == observation.NodeID {
			spaceSame = true
		}
		if observation.ZoneID != "" && node.ZoneID == observation.ZoneID {
			spaceZone = true
		}
		if topology != nil && node.ID != "" && observation.NodeID != "" {
			switch topology.Relationship(node.ID, observation.NodeID) {
			case TopologyReachable, TopologyAdjacent, TopologySame:
				spaceReachable = true
			case TopologyUnreachable:
				spaceUnreachable = true
			}
		}
	}
	if spaceSame {
		addFactor(&assessment.Reasons, "space.same_node", true, 90, false)
	}
	if spaceZone {
		addFactor(&assessment.Reasons, "space.same_zone", true, 50, false)
	}
	if topology == nil || observation.NodeID == "" || len(episode.Nodes) == 0 {
		addFactor(&assessment.Reasons, "space.unknown", false, 0, false)
	} else if spaceUnreachable && !spaceReachable {
		addFactor(&assessment.Reasons, "space.unreachable", true, 0, true)
		hardReject = true
	} else if spaceReachable {
		addFactor(&assessment.Reasons, "space.reachable", true, 60, false)
	}

	chainSame, chainDifferent := false, false
	for _, existing := range episode.ChainRefs {
		if existing.ID != "" && existing.ID == observation.ChainID {
			chainSame = true
		}
		if existing.ID != "" && observation.ChainID != "" && existing.ID != observation.ChainID {
			chainDifferent = true
		}
	}
	if chainSame {
		addFactor(&assessment.Reasons, "chain.same", true, 40, false)
	} else if chainDifferent {
		addFactor(&assessment.Reasons, "chain.different", true, -40, false)
	} else {
		addFactor(&assessment.Reasons, "chain.related", false, 0, false)
	}
	sharedRoutine := false
	for _, routine := range episode.RoutineRefs {
		if contains(observation.RoutineIDs, routine.ID) {
			sharedRoutine = true
			break
		}
	}
	if sharedRoutine {
		addFactor(&assessment.Reasons, "routine.shared", true, 20, false)
	}

	if observation.HouseMode != "" || observation.Occupancy != "" {
		for _, existing := range episode.Observations {
			if observation.HouseMode != "" && existing.HouseMode != "" {
				if observation.HouseMode == existing.HouseMode {
					addFactor(&assessment.Reasons, "context.house_mode_same", true, 10, false)
				} else {
					addFactor(&assessment.Reasons, "context.house_mode_different", true, -10, false)
				}
				break
			}
		}
		for _, existing := range episode.Observations {
			if observation.Occupancy != "" && existing.Occupancy != "" {
				if observation.Occupancy == existing.Occupancy {
					addFactor(&assessment.Reasons, "context.occupancy_same", true, 10, false)
				} else {
					addFactor(&assessment.Reasons, "context.occupancy_different", true, -10, false)
				}
				break
			}
		}
	} else {
		addFactor(&assessment.Reasons, "context.missing", false, 0, false)
	}

	if hardReject {
		assessment.Eligible = false
		assessment.Score = 0
		return assessment
	}
	assessment.Score = int(math.Max(0, math.Min(1000, float64(score))))
	for _, reason := range assessment.Reasons {
		assessment.Score += reason.ScoreDelta
	}
	if assessment.Score < 0 {
		assessment.Score = 0
	}
	if assessment.Score > 1000 {
		assessment.Score = 1000
	}
	return assessment
}
