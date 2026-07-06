package cognitive

import (
	"math"
	"strings"

	"synora/internal/engine/contracts"
)

type SimilarityResult struct {

	EventSimilarity float64

	SubjectSimilarity float64

	TargetSimilarity float64

	TopologySimilarity float64

	ContextSimilarity float64

	TimeSimilarity float64

	StatisticalSimilarity float64

	Similarity float64

	Divergence float64
}

func ComputeDivergence(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) SimilarityResult {

	eventSimilarity :=
		computeEventSimilarity(
			node,
			event,
		)

	subjectSimilarity :=
		computeSubjectSimilarity(
			node,
			event,
		)

	targetSimilarity :=
		computeTargetSimilarity(
			node,
			event,
		)

	topologySimilarity :=
		computeTopologySimilarity(
			node,
			event,
		)

	contextSimilarity :=
		computeContextSimilarity(
			node,
			event,
		)

	timeSimilarity :=
		computeTimeSimilarity(
			node,
			event,
		)

	statisticalSimilarity :=
		computeStatisticalSimilarity(
			node,
		)

	// -------------------------------------------------------------
	// GLOBAL SIMILARITY
	// -------------------------------------------------------------

	similarity :=
		eventSimilarity*0.25 +
			subjectSimilarity*0.20 +
			targetSimilarity*0.10 +
			topologySimilarity*0.15 +
			contextSimilarity*0.10 +
			timeSimilarity*0.10 +
			statisticalSimilarity*0.10

	similarity =
		clamp(
			similarity,
			0.0,
			1.0,
		)

	return SimilarityResult{
		EventSimilarity:
			eventSimilarity,

		SubjectSimilarity:
			subjectSimilarity,

		TargetSimilarity:
			targetSimilarity,

		TopologySimilarity:
			topologySimilarity,

		ContextSimilarity:
			contextSimilarity,

		TimeSimilarity:
			timeSimilarity,

		StatisticalSimilarity:
			statisticalSimilarity,

		Similarity:
			similarity,

		Divergence:
			1.0 - similarity,
	}
}

func computeEventSimilarity(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) float64 {

	if node.Event == event.Type {
		return 1.0
	}

	nodeParts :=
		strings.Split(
			node.Event,
			".",
		)

	eventParts :=
		strings.Split(
			event.Type,
			".",
		)

	maxLen := len(nodeParts)

	if len(eventParts) < maxLen {
		maxLen = len(eventParts)
	}

	if maxLen == 0 {
		return 0
	}

	matches := 0

	for i := 0; i < maxLen; i++ {

		if nodeParts[i] == eventParts[i] {
			matches++
		}
	}

	return float64(matches) /
		float64(maxLen)
}

func computeTopologySimilarity(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) float64 {

	if node.TopologyNode ==
		event.TopologyNode {

		return 1.0
	}

	return 0.0
}

func computeTimeSimilarity(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) float64 {

	if node.Context == nil ||
		event.Metadata == nil {

		return 0.5
	}

	score := 0.0
	total := 0.0

	// ---------------------------------------------------------
	// HOUR
	// ---------------------------------------------------------

	nodeHourRaw, nodeHourOk :=
		node.Context["hour"]

	eventHourRaw, eventHourOk :=
		event.Metadata["hour"]

	if nodeHourOk && eventHourOk {

		total += 0.6

		nodeHour :=
			toFloat64(nodeHourRaw)

		eventHour :=
			toFloat64(eventHourRaw)

		diff :=
			math.Abs(
				nodeHour - eventHour,
			)

		switch {

		case diff <= 1:
			score += 0.6

		case diff <= 2:
			score += 0.45

		case diff <= 4:
			score += 0.25

		default:
			score += 0.0
		}
	}

	// ---------------------------------------------------------
	// WEEKDAY
	// ---------------------------------------------------------

	nodeDayRaw, nodeDayOk :=
		node.Context["weekday"]

	eventDayRaw, eventDayOk :=
		event.Metadata["weekday"]

	if nodeDayOk && eventDayOk {

		total += 0.2

		nodeDay :=
			toFloat64(nodeDayRaw)

		eventDay :=
			toFloat64(eventDayRaw)

		if nodeDay == eventDay {

			score += 0.2
		}
	}

	// ---------------------------------------------------------
	// HOUSE STATE
	// ---------------------------------------------------------

	nodeStateRaw, nodeStateOk :=
		node.Context["house_state"]

	eventStateRaw, eventStateOk :=
		event.Metadata["house_state"]

	if nodeStateOk && eventStateOk {

		total += 0.2

		if nodeStateRaw ==
			eventStateRaw {

			score += 0.2
		}
	}

	if total == 0 {

		return 0.5
	}

	return score / total
}

func toFloat64(
	value any,
) float64 {

	switch v := value.(type) {

	case int:
		return float64(v)

	case int32:
		return float64(v)

	case int64:
		return float64(v)

	case float32:
		return float64(v)

	case float64:
		return v
	}

	return 0
}

func computeStatisticalSimilarity(
	node *contracts.BehaviorNode,
) float64 {

	countScore :=
		1.0 -
			math.Exp(
				-float64(node.Count)/20.0,
			)

	weightScore :=
		node.Weight

	return clamp(
		(countScore+weightScore)/2.0,
		0.0,
		1.0,
	)
}

func computeSubjectSimilarity(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) float64 {

	if node.SubjectType != event.SubjectType {
		return 0.0
	}

	if node.SubjectID == event.SubjectID {
		return 1.0
	}

	return 0.0
}

func computeTargetSimilarity(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) float64 {

	if node.TargetType != event.TargetType {
		return 0.0
	}

	if node.TargetID == event.TargetID {
		return 1.0
	}

	return 0.0
}

func computeContextSimilarity(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) float64 {

	if node.Context == nil ||
		event.Metadata == nil {

		return 0.5
	}

	score := 0.0
	total := 0.0

	type weightedKey struct {
		Key    string
		Weight float64
	}

	keys := []weightedKey{
		{"house_state", 0.50},
		{"hour", 0.30},
		{"weekday", 0.20},
	}

	for _, k := range keys {

		nodeValue, nodeOk :=
			node.Context[k.Key]

		eventValue, eventOk :=
			event.Metadata[k.Key]

		if !nodeOk ||
			!eventOk {

			continue
		}

		total += k.Weight

		if nodeValue == eventValue {

			score += k.Weight
		}
	}

	if total == 0 {
		return 0.5
	}

	return score / total
}

func ComputeExperienceWeight(
	outcome *contracts.Outcome,
) float64 {

	if outcome == nil {
		return 0
	}

	total :=
		float64(
			outcome.SuccessCount +
				outcome.FailureCount,
		)

	if total == 0 {
		return 0
	}

	successRate :=
		float64(outcome.SuccessCount) /
			total

	experience :=
		math.Log10(
			total+1,
		) / 3.0

	experience =
		clamp(
			experience,
			0,
			1,
		)

	return clamp(
		successRate*
			outcome.Confidence*
			experience,
		0,
		1,
	)
}



func clamp(
	value float64,
	min float64,
	max float64,
) float64 {

	if value < min {
		return min
	}

	if value > max {
		return max
	}

	return value
}

