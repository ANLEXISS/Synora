package cognitive

import (
	"time"

	"synora/internal/engine/contracts"
	"synora/internal/engine/graph"
)

const (
	MinSimilarity = 0.80
)

type Engine struct {
	graphMemory *graph.GraphMemory

	sequences *SequenceManager
}

func NewEngine(
	graphMemory *graph.GraphMemory,
) *Engine {

	return &Engine{
		graphMemory: graphMemory,

		sequences: NewSequenceManager(
			2 * time.Minute,
		),
	}
}

// -----------------------------------------------------------------------------
// PUBLIC ENTRYPOINT
// -----------------------------------------------------------------------------

func (e *Engine) ProcessEvent(
	event *contracts.Event,
) contracts.DecisionResult {

	return e.AnalyzeEvent(
		event,
	)
}

func (e *Engine) Sequence(
	sequenceKey string,
) (*contracts.ActiveSequence, bool) {
	if e == nil || e.sequences == nil {
		return nil, false
	}
	return e.sequences.Get(sequenceKey)
}

// -----------------------------------------------------------------------------
// ANALYSIS
// -----------------------------------------------------------------------------

func (e *Engine) AnalyzeEvent(
	event *contracts.Event,
) contracts.DecisionResult {
	sequenceKey :=
		graph.SequenceKey(
			event,
		)

	seq :=
		e.sequences.AddEvent(
			event,
		)

	var nodes []*contracts.BehaviorNode

	if seq.CurrentNode != nil {

		nodes =
			seq.CurrentNode.Children

	} else {

		nodes =
			e.graphMemory.GetNextProbableNodes(
				graph.SequenceKey(event),
			)
	}

	// -------------------------------------------------------------------------
	// ROOT FALLBACK
	// -------------------------------------------------------------------------

	if len(nodes) == 0 {

		graphData :=
			e.graphMemory.GetGraph()

		if graphData != nil {

			for _, root := range graphData.Roots {

				if root.Event == event.Type &&
					root.TopologyNode == event.TopologyNode {

					divergence :=
						ComputeDivergence(
							root,
							event,
						)

					seq.CurrentNode =
						root

					seq.Predictions =
						root.Children

					return annotateDecision(
						ComputeDecision(
							divergence,

							root.Outcome,

							event,
						),
						event,
						sequenceKey,
						root,
						"root_fallback",
					)
				}
			}
		}

		return annotateDecision(
			ComputeDecision(
				SimilarityResult{
					EventSimilarity:       0,
					TopologySimilarity:    0,
					TimeSimilarity:        0,
					StatisticalSimilarity: 0,

					Similarity: 0,

					Divergence: 1,
				},

				nil,

				event,
			),
			event,
			sequenceKey,
			nil,
			"no_graph_match",
		)
	}

	// -------------------------------------------------------------------------
	// FIND BEST LOCAL MATCH
	// -------------------------------------------------------------------------

	var matchedNode *contracts.BehaviorNode

	bestSimilarity := -1.0

	var bestResult SimilarityResult

	for _, node := range nodes {

		result :=
			ComputeDivergence(
				node,
				event,
			)

		if result.Similarity >
			bestSimilarity {

			bestSimilarity =
				result.Similarity

			bestResult =
				result

			matchedNode =
				node
		}
	}

	// -------------------------------------------------------------------------
	// GLOBAL SEARCH FALLBACK
	// -------------------------------------------------------------------------

	if bestSimilarity < MinSimilarity {

		globalNode,
			globalResult :=
			e.FindBestMatchingNode(
				event,
			)

		if globalNode != nil &&
			globalResult.Similarity > bestSimilarity {

			matchedNode =
				globalNode

			bestResult =
				globalResult

			bestSimilarity =
				globalResult.Similarity
		}
	}

	// -------------------------------------------------------------------------
	// NO MATCH FOUND
	// -------------------------------------------------------------------------

	if matchedNode == nil {

		return annotateDecision(
			ComputeDecision(
				SimilarityResult{
					Divergence: 1,
				},

				nil,

				event,
			),
			event,
			sequenceKey,
			nil,
			"no_match",
		)
	}

	// -------------------------------------------------------------------------
	// SIMILARITY TOO LOW
	// -------------------------------------------------------------------------

	if bestSimilarity < MinSimilarity {

		return annotateDecision(
			ComputeDecision(
				SimilarityResult{
					EventSimilarity:       bestResult.EventSimilarity,
					TopologySimilarity:    bestResult.TopologySimilarity,
					TimeSimilarity:        bestResult.TimeSimilarity,
					StatisticalSimilarity: bestResult.StatisticalSimilarity,

					Similarity: bestSimilarity,

					Divergence: 1,
				},

				nil,

				event,
			),
			event,
			sequenceKey,
			matchedNode,
			"low_similarity",
		)
	}

	// -------------------------------------------------------------------------
	// UPDATE SEQUENCE POSITION
	// -------------------------------------------------------------------------

	seq.CurrentNode =
		matchedNode

	seq.Predictions =
		matchedNode.Children

	// -------------------------------------------------------------------------
	// DECISION
	// -------------------------------------------------------------------------

	return annotateDecision(
		ComputeDecision(
			bestResult,

			matchedNode.Outcome,

			event,
		),
		event,
		sequenceKey,
		matchedNode,
		"graph_match",
	)
}

// -----------------------------------------------------------------------------
// PERIODIC TASK
// -----------------------------------------------------------------------------

func (e *Engine) Tick() {

	expired :=
		e.sequences.ExpireSequences(
			time.Now(),
		)

	for _, seq := range expired {

		outcome :=
			InferOutcome(
				seq,
			)

		seq.Outcome =
			outcome

		// TODO
		// e.LearnSequence(seq)
	}
}

// -----------------------------------------------------------------------------
// HELPERS
// -----------------------------------------------------------------------------

func (e *Engine) PredictNext(
	subjectID string,
) []*contracts.BehaviorNode {

	return e.graphMemory.GetNextProbableNodes(
		string(contracts.SubjectResident) + ":" + subjectID,
	)
}

func (e *Engine) FindBestMatchingNode(
	event *contracts.Event,
) (*contracts.BehaviorNode, SimilarityResult) {

	graphData :=
		e.graphMemory.GetGraph()

	if graphData == nil {

		return nil,
			SimilarityResult{
				Divergence: 1.0,
			}
	}

	var bestNode *contracts.BehaviorNode

	bestResult :=
		SimilarityResult{
			Divergence: 1.0,
		}

	bestSimilarity := -1.0

	var walk func(
		nodes []*contracts.BehaviorNode,
	)

	walk = func(
		nodes []*contracts.BehaviorNode,
	) {

		for _, node := range nodes {

			result :=
				ComputeDivergence(
					node,
					event,
				)

			// ---------------------------------------------------------
			// HARD FILTERS
			// ---------------------------------------------------------

			if result.EventSimilarity < 0.80 {
				continue
			}

			if result.SubjectSimilarity < 0.20 {
				continue
			}

			if result.TopologySimilarity < 0.20 {
				continue
			}

			// ---------------------------------------------------------
			// BEST MATCH
			// ---------------------------------------------------------

			if result.Similarity >
				bestSimilarity {

				bestSimilarity =
					result.Similarity

				bestResult =
					result

				bestNode =
					node
			}

			// ---------------------------------------------------------
			// RECURSIVE WALK
			// ---------------------------------------------------------

			if len(node.Children) > 0 {

				walk(
					node.Children,
				)
			}
		}
	}

	walk(
		graphData.Roots,
	)

	return bestNode,
		bestResult
}
