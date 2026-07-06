package automation

import (
	"strings"
	"time"

	"synora/pkg/contract"
)

func evaluateConditions(conds []Condition, event contract.Event, decision *contract.Decision) bool {
	now := time.Now()

	for _, c := range conds {
		switch c.Field {
		case "device":
			v, ok := c.Value.(string)
			if !ok {
				return false
			}
			if c.Op == "==" && event.DeviceID != v {
				return false
			}
			if c.Op == "!=" && event.DeviceID == v {
				return false
			}
		case "node":
			v, ok := c.Value.(string)
			if !ok {
				return false
			}
			if c.Op == "==" && event.NodeID != v {
				return false
			}
			if c.Op == "!=" && event.NodeID == v {
				return false
			}
		case "type":
			v, ok := c.Value.(string)
			if !ok {
				return false
			}
			if c.Op == "==" && event.Type != v {
				return false
			}
			if c.Op == "!=" && event.Type == v {
				return false
			}
		case "score":
			v, ok := c.Value.(float64)
			if !ok || decision == nil {
				return false
			}
			if c.Op == ">" && decision.EffectiveScore <= v {
				return false
			}
			if c.Op == "<" && decision.EffectiveScore >= v {
				return false
			}
		case "time_after":
			v, ok := c.Value.(string)
			if !ok || !afterClock(now, v) {
				return false
			}
		case "time_before":
			v, ok := c.Value.(string)
			if !ok || !beforeClock(now, v) {
				return false
			}
		}
	}

	return true
}

func afterClock(now time.Time, value string) bool {
	hour, minute, ok := parseClock(value)
	if !ok {
		return false
	}
	current := now.Hour()*60 + now.Minute()
	return current >= hour*60+minute
}

func beforeClock(now time.Time, value string) bool {
	hour, minute, ok := parseClock(value)
	if !ok {
		return false
	}
	current := now.Hour()*60 + now.Minute()
	return current <= hour*60+minute
}

func parseClock(value string) (int, int, bool) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, false
	}
	return hour.Hour(), hour.Minute(), true
}

func isWithinSchedule(schedule *Schedule) bool {

	if schedule == nil {
		return true
	}

	now := time.Now()
	current := now.Hour()*60 + now.Minute()

	startMinutes := -1
	endMinutes := -1

	if schedule.Start != "" {
		hour, minute, ok := parseClock(schedule.Start)
		if ok {
			startMinutes = hour*60 + minute
		}
	}

	if schedule.End != "" {
		hour, minute, ok := parseClock(schedule.End)
		if ok {
			endMinutes = hour*60 + minute
		}
	}

	if startMinutes >= 0 && endMinutes >= 0 {
		if startMinutes <= endMinutes {
			return current >= startMinutes && current <= endMinutes
		}
		return current >= startMinutes || current <= endMinutes
	}
	if startMinutes >= 0 {
		return current >= startMinutes
	}
	if endMinutes >= 0 {
		return current <= endMinutes
	}
	return true
}
