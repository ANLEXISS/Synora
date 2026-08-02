package situationfacts

import (
	"sort"
	"time"
)

func (b *extractionBuilder) extractTemporal() error {
	episode := b.input.Episode
	subject := episodeSubject(episode.ID)
	prov := b.observationProvenance()
	dayparts, weekdays := []string{}, []string{}
	gaps := []int64{}
	outOfOrder := false
	for i, observation := range episode.Observations {
		at := observation.ObservedAt.UTC()
		weekdays = append(weekdays, at.Weekday().String())
		dayparts = append(dayparts, daypart(at))
		if i > 0 {
			gap := at.Sub(episode.Observations[i-1].ObservedAt.UTC()).Milliseconds()
			if !episode.Observations[i-1].ReceivedAt.IsZero() && !observation.ReceivedAt.IsZero() && observation.ReceivedAt.Before(episode.Observations[i-1].ReceivedAt) {
				outOfOrder = true
			}
			if gap < 0 {
				outOfOrder = true
				gap = 0
			}
			gaps = append(gaps, gap)
		}
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	minimum, maximum, average := int64(0), int64(0), int64(0)
	if len(gaps) > 0 {
		minimum, maximum = gaps[0], gaps[len(gaps)-1]
		for _, gap := range gaps {
			average += gap
		}
		average /= int64(len(gaps))
	}
	values := []struct {
		code  FactCode
		value FactValue
	}{{CodeTemporalDurationMS, DurationMSFactValue(episode.DurationObserved.Milliseconds())}, {CodeTemporalOutOfOrderPresent, BoolFactValue(outOfOrder)}, {CodeTemporalDaypartSet, StringSetFactValue(dayparts)}, {CodeTemporalWeekdaySet, StringSetFactValue(weekdays)}, {CodeTemporalMinimumGapMS, DurationMSFactValue(minimum)}, {CodeTemporalMaximumGapMS, DurationMSFactValue(maximum)}, {CodeTemporalAverageGapMS, DurationMSFactValue(average)}}
	for _, item := range values {
		if err := b.add(item.code, ScopeEpisode, subject, "", item.value, OriginDerived, StatusAsserted, episode.StartedAt, prov, false); err != nil {
			return err
		}
	}
	return nil
}

func daypart(at time.Time) string {
	hour := at.Hour()
	switch {
	case hour < 6:
		return "night"
	case hour < 12:
		return "morning"
	case hour < 18:
		return "day"
	default:
		return "evening"
	}
}
