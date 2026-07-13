package automation

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"synora/pkg/contract"
)

func evaluateConditions(conds []Condition, logic string, event contract.Event, decision *contract.Decision, now time.Time) bool {
	matched, _ := evaluateConditionsDetailed(conds, logic, event, decision, now)
	return matched
}

type conditionEvaluation struct {
	condition Condition
	actual    any
	exists    bool
	result    bool
}

func evaluateConditionsDetailed(conds []Condition, logic string, event contract.Event, decision *contract.Decision, now time.Time) (bool, []conditionEvaluation) {
	evaluations := make([]conditionEvaluation, 0, len(conds))
	if strings.EqualFold(logic, "any") {
		for _, c := range conds {
			evaluation := evaluateConditionDetailed(c, event, decision, now)
			evaluations = append(evaluations, evaluation)
			if evaluation.result {
				return true, evaluations
			}
		}
		return len(conds) == 0, evaluations
	}
	for _, c := range conds {
		evaluation := evaluateConditionDetailed(c, event, decision, now)
		evaluations = append(evaluations, evaluation)
		if !evaluation.result {
			return false, evaluations
		}
	}

	return true, evaluations
}

func evaluateCondition(c Condition, event contract.Event, decision *contract.Decision, now time.Time) bool {
	return evaluateConditionDetailed(c, event, decision, now).result
}

func evaluateConditionDetailed(c Condition, event contract.Event, decision *contract.Decision, now time.Time) conditionEvaluation {
	evaluation := conditionEvaluation{condition: c}
	result := false
	field := strings.ToLower(strings.TrimSpace(c.Field))
	switch field {
	case "time_after":
		v, ok := c.Value.(string)
		result = ok && afterClock(now, v)
	case "time_before":
		v, ok := c.Value.(string)
		result = ok && beforeClock(now, v)
	default:
		actual, exists := conditionValue(field, event, decision)
		evaluation.actual = actual
		evaluation.exists = exists
		if isDangerLevelField(field) {
			result = compareDangerLevel(c.Op, actual, exists, c.Value)
		} else {
			result = compareCondition(c.Op, actual, exists, c.Value)
		}
	}
	if c.Negate {
		result = !result
	}
	evaluation.result = result
	return evaluation
}

func conditionValue(field string, event contract.Event, decision *contract.Decision) (any, bool) {
	field = strings.ToLower(strings.TrimSpace(field))
	switch field {
	case "device", "device.id", "device_id":
		return event.DeviceID, event.DeviceID != ""
	case "node", "node.id", "node_id":
		if event.NodeID != "" {
			return event.NodeID, true
		}
		if decision != nil && decision.NodeID != "" {
			return decision.NodeID, true
		}
	case "type", "event.type", "event_type":
		return event.Type, event.Type != ""
	case "state", "decision_state", "system.state", "system_state", "current_state":
		if decision != nil {
			return decision.State, decision.State != ""
		}
	case "score", "effective_score":
		if decision != nil {
			return decision.EffectiveScore, true
		}
	case "danger.level", "danger_level", "cge.danger_level":
		if decision != nil && decision.DangerLevel != "" {
			return decision.DangerLevel, true
		}
		return eventPayloadValue(event, "danger_level", "danger.level")
	case "danger.score", "danger_score":
		if decision != nil && (decision.DangerLevel != "" || decision.DangerScore != 0) {
			return decision.DangerScore, true
		}
		if decision != nil {
			return decision.EffectiveScore, true
		}
	case "danger.source", "danger_source":
		if decision != nil && decision.DangerSource != "" {
			return decision.DangerSource, true
		}
		return eventPayloadValue(event, "danger_source", "danger.source")
	case "test", "simulated", "dry_run":
		return eventMetadataValue(event, field)
	case "clip_id":
		if decision != nil && decision.ClipID != "" {
			return decision.ClipID, true
		}
		return event.ClipID, event.ClipID != ""
	case "decision_type", "situation_type":
		if decision != nil {
			return decision.Type, decision.Type != ""
		}
	default:
		if strings.HasPrefix(field, "payload.") {
			return nestedValue(event.Payload, strings.TrimPrefix(field, "payload."))
		}
		if value, ok := nestedValue(event.Payload, field); ok {
			return value, true
		}
		if event.Payload != nil {
			if value, ok := event.Payload[field]; ok {
				return value, true
			}
		}
	}
	return nil, false
}

func isDangerLevelField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "danger.level", "danger_level", "cge.danger_level":
		return true
	default:
		return false
	}
}

func compareDangerLevel(op string, actual any, exists bool, expected any) bool {
	if op == "" {
		op = "=="
	}
	if op == "exists" {
		return exists
	}
	if op == "not_exists" {
		return !exists
	}
	left, lok := dangerLevelRank(actual)
	right, rok := dangerLevelRank(expected)
	if !exists || !lok || !rok {
		return false
	}
	switch op {
	case "==":
		return left == right
	case "!=":
		return left != right
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<":
		return left < right
	case "<=":
		return left <= right
	default:
		return false
	}
}

func dangerLevelRank(value any) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case string(contract.DangerNone):
		return 0, true
	case string(contract.DangerLow):
		return 1, true
	case string(contract.DangerMedium):
		return 2, true
	case string(contract.DangerHigh):
		return 3, true
	case string(contract.DangerCritical):
		return 4, true
	default:
		return 0, false
	}
}

func eventPayloadValue(event contract.Event, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := event.Payload[key]; ok {
			return value, true
		}
		if value, ok := nestedValue(event.Payload, key); ok {
			return value, true
		}
	}
	return nil, false
}

func eventMetadataValue(event contract.Event, key string) (any, bool) {
	if value, ok := eventPayloadValue(event, key); ok {
		return value, true
	}
	metadata, ok := event.Payload["metadata"].(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := metadata[key]
	return value, ok
}

func nestedValue(root map[string]any, path string) (any, bool) {
	var current any = root
	for _, part := range strings.Split(path, ".") {
		mapped, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapped[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func compareCondition(op string, actual any, exists bool, expected any) bool {
	if op == "" {
		op = "=="
	}
	switch op {
	case "exists":
		return exists
	case "not_exists":
		return !exists
	case "==":
		return exists && valuesEqual(actual, expected)
	case "!=":
		return !exists || !valuesEqual(actual, expected)
	case ">", ">=", "<", "<=":
		left, lok := asFloat(actual)
		right, rok := asFloat(expected)
		if !exists || !lok || !rok {
			return false
		}
		switch op {
		case ">":
			return left > right
		case ">=":
			return left >= right
		case "<":
			return left < right
		case "<=":
			return left <= right
		}
	case "contains":
		return exists && containsValue(actual, expected)
	}
	return false
}

func valuesEqual(actual any, expected any) bool {
	if left, ok := asFloat(actual); ok {
		if right, ok := asFloat(expected); ok {
			return left == right
		}
	}
	return fmt.Sprint(actual) == fmt.Sprint(expected) || reflect.DeepEqual(actual, expected)
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func containsValue(actual any, expected any) bool {
	switch v := actual.(type) {
	case string:
		return strings.Contains(v, fmt.Sprint(expected))
	case []string:
		want := fmt.Sprint(expected)
		for _, item := range v {
			if item == want {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if valuesEqual(item, expected) {
				return true
			}
		}
	}
	return false
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

func isWithinSchedule(schedule *Schedule, now time.Time) bool {

	if schedule == nil {
		return true
	}

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
