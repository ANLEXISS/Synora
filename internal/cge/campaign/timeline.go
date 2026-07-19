package campaign

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	cgecontext "synora/internal/cge/context"
)

func GenerateTimeline(profile Profile) (Timeline, error) {
	if err := profile.Validate(); err != nil {
		return Timeline{}, err
	}
	end := profile.StartAt.AddDate(0, 0, profile.DurationDays)
	result := Timeline{ProfileID: profile.ID, Seed: profile.Seed, StartAt: profile.StartAt, EndAt: end}
	sequence := 0
	for day := 0; day < profile.DurationDays; day++ {
		date := profile.StartAt.AddDate(0, 0, day)
		weekday := date.Weekday()
		for templateIndex, template := range profile.RoutineTemplates {
			if !containsWeekday(template.DaysOfWeek, weekday) || !selected(profile.Seed, uint64(day), uint64(templateIndex), template.ProbabilityPermille) {
				continue
			}
			jitter := signedVariation(profile.Seed, uint64(day), uint64(templateIndex), template.VariationMinutes)
			start := date.Add(time.Duration(template.StartMinuteOfDay+jitter) * time.Minute)
			for pathIndex, nodeID := range template.Path {
				at := start.Add(time.Duration(pathIndex) * 3 * time.Minute)
				if at.Before(profile.StartAt) || !at.Before(end) {
					continue
				}
				result.Events = append(result.Events, makeEvent(profile, at, template.ResidentID, nodeID, LabelOrdinary, template.ID, sequence, true, true))
				sequence++
			}
		}
	}
	for episodeIndex, episode := range profile.Episodes {
		count := episode.RepeatCount
		if count <= 0 {
			count = 1
		}
		for repeat := 0; repeat < count; repeat++ {
			day := episode.StartDay + repeat*episode.RepeatEveryDays
			if day < 0 || day >= profile.DurationDays {
				continue
			}
			start := profile.StartAt.AddDate(0, 0, day).Add(time.Duration(episode.StartMinuteOfDay) * time.Minute).Add(time.Duration(signedVariation(profile.Seed, uint64(episodeIndex), uint64(repeat), 3)) * time.Minute)
			for pathIndex, nodeID := range episode.Path {
				at := start.Add(time.Duration(pathIndex) * 3 * time.Minute)
				if at.Before(profile.StartAt) || !at.Before(end) {
					continue
				}
				available, topology := true, true
				quality := "complete"
				switch episode.Label {
				case LabelSensorDropout:
					available, quality = false, "unknown"
				case LabelTopologyUnavailable:
					topology, quality = false, "partial"
				case LabelIdentityUncertain:
					quality = "partial"
				}
				result.Events = append(result.Events, makeEventWithAvailability(profile, at, episode.ResidentID, nodeID, episode.Label, episode.ID, sequence, available, topology, quality))
				sequence++
			}
		}
	}
	sort.SliceStable(result.Events, func(i, j int) bool {
		if !result.Events[i].OccurredAt.Equal(result.Events[j].OccurredAt) {
			return result.Events[i].OccurredAt.Before(result.Events[j].OccurredAt)
		}
		if priority(result.Events[i]) != priority(result.Events[j]) {
			return priority(result.Events[i]) < priority(result.Events[j])
		}
		return result.Events[i].ID < result.Events[j].ID
	})
	markBoundaries(&result, profile)
	return result, nil
}

func containsWeekday(days []time.Weekday, value time.Weekday) bool {
	for _, day := range days {
		if day == value {
			return true
		}
	}
	return false
}

func mix(seed, a, b uint64) uint64 {
	x := seed ^ (a + 0x9e3779b97f4a7c15) ^ (b * 0xbf58476d1ce4e5b9)
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func selected(seed, a, b uint64, probability int) bool {
	return probability >= 1000 || int(mix(seed, a, b)%1000) < probability
}

func signedVariation(seed, a, b uint64, magnitude int) int {
	if magnitude <= 0 {
		return 0
	}
	return int(mix(seed, a, b)%uint64(2*magnitude+1)) - magnitude
}

func makeEvent(profile Profile, at time.Time, resident, node string, label EpisodeLabel, episode string, sequence int, available, topology bool) TimelineEvent {
	return makeEventWithAvailability(profile, at, resident, node, label, episode, sequence, available, topology, "complete")
}

func makeEventWithAvailability(profile Profile, at time.Time, resident, node string, label EpisodeLabel, episode string, sequence int, available, topology bool, quality string) TimelineEvent {
	// Labels are experimental truth metadata. They must not influence the
	// observable event identity or any downstream CGE result.
	material := fmt.Sprintf("%s|%d|%d|%s|%s|%s", profile.ID, profile.Seed, sequence, at.UTC().Format(time.RFC3339Nano), resident, node)
	digest := sha256.Sum256([]byte(material))
	return TimelineEvent{ID: "campaign-event-" + hex.EncodeToString(digest[:]), OccurredAt: at.UTC(), ResidentID: resident, NodeID: node, Label: label, EpisodeID: episode, ContextQuality: contextQuality(quality), ContextAvailable: available, TopologyAvailable: topology}
}

func contextQuality(value string) (quality cgecontext.ContextQuality) {
	switch value {
	case "complete":
		return cgecontext.QualityComplete
	case "partial":
		return cgecontext.QualityPartial
	default:
		return cgecontext.QualityUnknown
	}
}

func priority(event TimelineEvent) int {
	if event.Label == LabelSystemRestart {
		return 0
	}
	return 1
}

func markBoundaries(timeline *Timeline, profile Profile) {
	location, err := time.LoadLocation(profile.Timezone)
	if err != nil {
		location = time.UTC
	}
	seenDays := map[string]bool{}
	for i := range timeline.Events {
		dayKey := timeline.Events[i].OccurredAt.In(location).Format("2006-01-02")
		if profile.RestartPolicy.EveryDays > 0 {
			day := int(timeline.Events[i].OccurredAt.Sub(profile.StartAt).Hours() / 24)
			if day%profile.RestartPolicy.EveryDays == 0 && !seenDays[dayKey] {
				timeline.Events[i].RestartBefore = true
				seenDays[dayKey] = true
			}
		}
		if profile.CheckpointPolicy.EveryDays > 0 {
			day := int(timeline.Events[i].OccurredAt.Sub(profile.StartAt).Hours() / 24)
			lastEventOfDay := i == len(timeline.Events)-1 || timeline.Events[i+1].OccurredAt.In(location).Format("2006-01-02") != dayKey
			if lastEventOfDay && (day+1)%profile.CheckpointPolicy.EveryDays == 0 {
				timeline.Events[i].CheckpointAfter = true
			}
		}
	}
}
