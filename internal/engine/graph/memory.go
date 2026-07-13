package graph

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/internal/engine/contracts"
)

const (
	SequenceBreakThreshold = 2 * time.Minute
)

type GraphMemory struct {
	graph *contracts.BehaviorGraph

	lastNodes map[string]*contracts.BehaviorNode

	learnedSequences   map[string]*contracts.LearnedSequence
	learnedTransitions map[string]*contracts.LearnedTransition
	learnedBehaviors   map[string]*contracts.LearnedBehavior
	behaviorOverrides  map[string]contracts.LearnedBehaviorOverride
	runEvents          map[string][]learnedEvent
	countedRunKeys     map[string]map[string]bool
	recentEvents       map[string][]learnedEvent
	seedRecentEvents   map[string][]learnedEvent
	lastSequenceByRun  map[string]string
	criticalSeeds      map[string]contracts.CriticalSeed

	mu sync.RWMutex
}

type learnedEvent struct {
	EventType       string
	SourceType      string
	DeviceID        string
	NodeID          string
	Identity        string
	Signature       string
	Timestamp       time.Time
	Simulated       bool
	TestRunID       string
	ScenarioID      string
	ScenarioStepID  string
	EventInstanceID string
	ZoneRole        string
	ZoneScope       string
	HouseState      string
	TimeWindow      string
}

func NewGraphMemory() *GraphMemory {

	return &GraphMemory{
		graph: &contracts.BehaviorGraph{
			GraphID: "house_main",

			Roots: make(
				[]*contracts.BehaviorNode,
				0,
			),

			Version: 1,

			LastUpdate: time.Now(),
		},

		lastNodes:          make(map[string]*contracts.BehaviorNode),
		learnedSequences:   make(map[string]*contracts.LearnedSequence),
		learnedTransitions: make(map[string]*contracts.LearnedTransition),
		learnedBehaviors:   make(map[string]*contracts.LearnedBehavior),
		behaviorOverrides:  make(map[string]contracts.LearnedBehaviorOverride),
		runEvents:          make(map[string][]learnedEvent),
		countedRunKeys:     make(map[string]map[string]bool),
		recentEvents:       make(map[string][]learnedEvent),
		seedRecentEvents:   make(map[string][]learnedEvent),
		lastSequenceByRun:  make(map[string]string),
		criticalSeeds:      make(map[string]contracts.CriticalSeed),
	}
}

func SequenceKey(
	event *contracts.Event,
) string {

	if event == nil {
		return ":"
	}

	return string(
		event.SubjectType,
	) + ":" +
		event.SubjectID
}

func createNodeFromEvent(
	event *contracts.Event,
) *contracts.BehaviorNode {
	context := make(map[string]any)
	for key, value := range event.Metadata {
		context[key] = value
	}

	return &contracts.BehaviorNode{
		Event: event.Type,

		SubjectType: event.SubjectType,

		SubjectID: event.SubjectID,

		TargetType: event.TargetType,

		TargetID: event.TargetID,

		TopologyNode: event.TopologyNode,

		Weight: calculateWeight(1),

		Count: 1,

		LastSeen: event.Timestamp,

		Context: context,

		Children: make(
			[]*contracts.BehaviorNode,
			0,
		),
	}
}

func isSameNode(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) bool {

	if node.Event != event.Type {
		return false
	}

	if node.SubjectType != event.SubjectType {
		return false
	}

	if node.TargetType != event.TargetType {
		return false
	}

	if node.TopologyNode != event.TopologyNode {
		return false
	}

	// identité connue
	if node.SubjectID != "" &&
		event.SubjectID != "" &&
		node.SubjectID != event.SubjectID {

		return false
	}

	if node.TargetID != "" &&
		event.TargetID != "" &&
		node.TargetID != event.TargetID {

		return false
	}

	return true
}

func updateAverageDelta(
	node *contracts.BehaviorNode,
	deltaMs int64,
) {

	if node.AvgDeltaMs == 0 {

		node.AvgDeltaMs =
			deltaMs

		return
	}

	node.AvgDeltaMs =
		(node.AvgDeltaMs + deltaMs) / 2
}

func (g *GraphMemory) LearnEvent(
	event *contracts.Event,
) *contracts.CriticalSeedMatch {
	if event == nil {
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	graph := g.graph

	if graph == nil {

		graph = &contracts.BehaviorGraph{
			GraphID: "house_main",

			Roots: make(
				[]*contracts.BehaviorNode,
				0,
			),

			Version: 1,

			LastUpdate: time.Now(),
		}

		g.graph = graph
	}

	if learningMode(event) == "disabled" {
		return nil
	}
	if !validationLearningEnabled(event) {
		return nil
	}

	current := learnedEventFromCGE(event)
	var seedMatch *contracts.CriticalSeedMatch
	if current.EventType != "" {
		seedMatch = g.observeCriticalSeedLocked(SequenceKey(event), current)
	}

	g.learnInspectionLocked(event)

	if isSimulated(event) {
		return seedMatch
	}

	sequenceKey :=
		SequenceKey(
			event,
		)

	previousNode, hasPrevious :=
		g.lastNodes[sequenceKey]

	// ---------------------------------------------------------------------
	// BREAK DETECTION
	// ---------------------------------------------------------------------

	if hasPrevious {

		delta :=
			event.Timestamp.Sub(
				previousNode.LastSeen,
			)

		if delta >
			SequenceBreakThreshold {

			delete(
				g.lastNodes,
				sequenceKey,
			)

			hasPrevious = false
			previousNode = nil
		}
	}

	// ---------------------------------------------------------------------
	// ROOT NODE
	// ---------------------------------------------------------------------

	if !hasPrevious {

		for _, root := range graph.Roots {

			if isSameNode(
				root,
				event,
			) {

				root.Count++

				root.LastSeen =
					event.Timestamp

				root.Weight =
					calculateWeight(
						int(root.Count),
					)

				g.lastNodes[sequenceKey] =
					root

				graph.Version++
				graph.LastUpdate =
					time.Now()

				return seedMatch
			}
		}

		root :=
			createNodeFromEvent(
				event,
			)

		graph.Roots =
			append(
				graph.Roots,
				root,
			)

		g.lastNodes[sequenceKey] =
			root

		graph.Version++
		graph.LastUpdate =
			time.Now()

		return seedMatch
	}

	// ---------------------------------------------------------------------
	// CHILD SEARCH
	// ---------------------------------------------------------------------

	for _, child := range previousNode.Children {

		if isSameNode(
			child,
			event,
		) {

			child.Count++

			delta :=
				event.Timestamp.Sub(
					previousNode.LastSeen,
				)

			updateAverageDelta(
				child,
				delta.Milliseconds(),
			)

			child.LastSeen =
				event.Timestamp

			child.Weight =
				calculateWeight(
					int(child.Count),
				)
			if child.Context == nil {
				child.Context = make(map[string]any)
			}
			child.Context["novel_transition"] = false

			g.lastNodes[sequenceKey] =
				child

			graph.Version++
			graph.LastUpdate =
				time.Now()

			return seedMatch
		}
	}

	// ---------------------------------------------------------------------
	// CREATE NEW BRANCH
	// ---------------------------------------------------------------------

	delta :=
		event.Timestamp.Sub(
			previousNode.LastSeen,
		)

	node :=
		createNodeFromEvent(
			event,
		)

	node.AvgDeltaMs =
		delta.Milliseconds()
	if node.Context == nil {
		node.Context = make(map[string]any)
	}
	node.Context["novel_transition"] = true
	node.Context["transition_ms"] = delta.Milliseconds()
	node.Context["previous_topology_node"] = previousNode.TopologyNode

	previousNode.Children =
		append(
			previousNode.Children,
			node,
		)

	g.lastNodes[sequenceKey] =
		node

	graph.Version++
	graph.LastUpdate =
		time.Now()
	return seedMatch
}

func findMatchingRoot(
	graph *contracts.BehaviorGraph,
	event *contracts.Event,
) *contracts.BehaviorNode {

	for _, root := range graph.Roots {

		if root.Event == event.Type &&
			root.TopologyNode == event.TopologyNode {

			return root
		}
	}

	return nil
}

func (g *GraphMemory) GetGraph() *contracts.BehaviorGraph {

	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.graph
}

func (g *GraphMemory) Clear() {

	g.mu.Lock()
	defer g.mu.Unlock()

	g.graph = &contracts.BehaviorGraph{
		GraphID: "house_main",

		Roots: make(
			[]*contracts.BehaviorNode,
			0,
		),

		Version: 1,

		LastUpdate: time.Now(),
	}

	g.lastNodes =
		make(map[string]*contracts.BehaviorNode)
	g.learnedSequences = make(map[string]*contracts.LearnedSequence)
	g.learnedTransitions = make(map[string]*contracts.LearnedTransition)
	g.learnedBehaviors = make(map[string]*contracts.LearnedBehavior)
	g.behaviorOverrides = make(map[string]contracts.LearnedBehaviorOverride)
	g.runEvents = make(map[string][]learnedEvent)
	g.countedRunKeys = make(map[string]map[string]bool)
	g.recentEvents = make(map[string][]learnedEvent)
	g.seedRecentEvents = make(map[string][]learnedEvent)
	g.lastSequenceByRun = make(map[string]string)
	g.criticalSeeds = make(map[string]contracts.CriticalSeed)
}

func (g *GraphMemory) ObserveActionEvidence(testRunID string, evidence string, simulated bool, at time.Time) {
	testRunID = strings.TrimSpace(testRunID)
	evidence = strings.TrimSpace(evidence)
	if testRunID == "" || evidence == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	sequenceKey := g.lastSequenceByRun[testRunID]
	if sequenceKey == "" {
		return
	}
	sequence := g.learnedSequences[sequenceKey]
	if sequence == nil {
		return
	}
	g.ensureBehaviorLocked(sequence)
	id := "beh-" + shortHash(sequence.Signature)
	behavior := g.learnedBehaviors[id]
	if behavior == nil {
		return
	}
	behavior.Evidence = appendLimited(behavior.Evidence, evidence, CGEMaxEvidencePerBehavior)
	behavior.LastMatchedAt = at
	if simulated {
		behavior.Context["simulation_only"] = true
	}
	g.pruneInspectionLocked()
}

func (g *GraphMemory) Inspection() map[string]any {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return map[string]any{
		"stats":             cgeStats(sortedSequences(g.learnedSequences), sortedTransitions(g.learnedTransitions), sortedBehaviors(g.learnedBehaviors)),
		"sequences":         sortedSequences(g.learnedSequences),
		"transitions":       sortedTransitions(g.learnedTransitions),
		"learned_behaviors": sortedBehaviors(g.learnedBehaviors),
	}
}

func (g *GraphMemory) learnInspectionLocked(event *contracts.Event) {
	current := learnedEventFromCGE(event)
	if current.EventType == "" {
		return
	}

	scope := SequenceKey(event)
	if current.Simulated {
		runKey := current.TestRunID
		if runKey == "" {
			runKey = scope
		}
		events := g.runEvents[runKey]
		if len(events) > 0 {
			g.recordTransitionLocked(events[len(events)-1], current)
		}
		events = append(events, current)
		g.runEvents[runKey] = events
		if len(events) >= 2 {
			g.recordSequenceOncePerRunLocked(runKey, events)
		}
		return
	}

	events := g.recentEvents[scope]
	if len(events) > 0 {
		previous := events[len(events)-1]
		if current.Timestamp.Sub(previous.Timestamp) > SequenceBreakThreshold {
			events = nil
		} else {
			g.recordTransitionLocked(previous, current)
		}
	}
	events = append(events, current)
	if len(events) > 5 {
		events = events[len(events)-5:]
	}
	g.recentEvents[scope] = events
	for size := 2; size <= len(events) && size <= 3; size++ {
		g.recordSequenceLocked(events[len(events)-size:])
	}
}

func (g *GraphMemory) recordSequenceOncePerRunLocked(runKey string, events []learnedEvent) {
	key := sequenceKey(events)
	counted := g.countedRunKeys[runKey]
	if counted == nil {
		counted = map[string]bool{}
		g.countedRunKeys[runKey] = counted
	}
	if counted[key] {
		return
	}
	counted[key] = true
	g.recordSequenceLocked(events)
}

func (g *GraphMemory) recordSequenceLocked(events []learnedEvent) {
	if len(events) < 2 {
		return
	}
	key := sequenceKey(events)
	sequence := g.learnedSequences[key]
	if sequence == nil {
		sequence = &contracts.LearnedSequence{
			ID:          "seq-" + shortHash(key),
			Signature:   sequenceSignature(events),
			EventTypes:  uniqueStrings(eventTypes(events)),
			SourceTypes: uniqueStrings(sourceTypes(events)),
			Devices:     uniqueStrings(devices(events)),
			Nodes:       uniqueStrings(nodes(events)),
			Identities:  uniqueStrings(identities(events)),
			Examples:    []string{},
			Evidence:    []string{},
		}
		g.learnedSequences[key] = sequence
	}

	sequence.Count++
	if sequence.FirstSeen.IsZero() || events[0].Timestamp.Before(sequence.FirstSeen) {
		sequence.FirstSeen = events[0].Timestamp
	}
	last := events[len(events)-1]
	sequence.LastSeen = last.Timestamp
	sequence.AvgDeltaMs = rollingAverage(sequence.AvgDeltaMs, sequenceAverageDelta(events), sequence.Count)
	sequence.Confidence = confidence(sequence.Count)
	if allSimulated(events) {
		sequence.SimulatedCount++
		sequence.LastTestRunID = last.TestRunID
		sequence.LastScenarioID = last.ScenarioID
	} else {
		sequence.RealCount++
	}
	if last.TestRunID != "" {
		g.lastSequenceByRun[last.TestRunID] = key
	}
	sequence.Examples = appendLimited(sequence.Examples, exampleID(events), CGEMaxExamplesPerSequence)
	sequence.Evidence = appendLimited(sequence.Evidence, fmt.Sprintf("matched %s", sequence.Signature), CGEMaxEvidencePerSequence)
	g.ensureBehaviorLocked(sequence)
	g.pruneInspectionLocked()
}

func (g *GraphMemory) recordTransitionLocked(from learnedEvent, to learnedEvent) {
	key := from.Signature + ">" + to.Signature
	transition := g.learnedTransitions[key]
	if transition == nil {
		transition = &contracts.LearnedTransition{
			ID:            "tr-" + shortHash(key),
			FromEventType: from.EventType,
			ToEventType:   to.EventType,
			FromSignature: from.Signature,
			ToSignature:   to.Signature,
		}
		g.learnedTransitions[key] = transition
	}
	transition.Count++
	if transition.FirstSeen.IsZero() || from.Timestamp.Before(transition.FirstSeen) {
		transition.FirstSeen = from.Timestamp
	}
	transition.LastSeen = to.Timestamp
	transition.AvgDeltaMs = rollingAverage(transition.AvgDeltaMs, to.Timestamp.Sub(from.Timestamp).Milliseconds(), transition.Count)
	transition.Confidence = confidence(transition.Count)
	if from.Simulated && to.Simulated {
		transition.SimulatedCount++
	} else {
		transition.RealCount++
	}
	g.pruneInspectionLocked()
}

func (g *GraphMemory) ensureBehaviorLocked(sequence *contracts.LearnedSequence) {
	if sequence == nil || sequence.Count < 3 {
		return
	}
	id := "beh-" + shortHash(sequence.Signature)
	behavior := g.learnedBehaviors[id]
	if behavior == nil {
		behavior = &contracts.LearnedBehavior{
			ID:                       id,
			TriggerSequenceSignature: sequence.Signature,
			Context:                  map[string]any{"source": "cge.sequence_repetition"},
			ProposedActions:          []map[string]any{},
			Status:                   "observing",
			RequiresValidation:       true,
			Enabled:                  true,
			Evidence:                 []string{},
			CreatedAt:                sequence.LastSeen,
		}
		g.learnedBehaviors[id] = behavior
	}
	behavior.Count = sequence.Count
	behavior.Confidence = sequence.Confidence
	behavior.SimulatedCount = sequence.SimulatedCount
	behavior.RealCount = sequence.RealCount
	behavior.LastMatchedAt = sequence.LastSeen
	behavior.UpdatedAt = sequence.LastSeen
	behavior.Evidence = appendLimited(behavior.Evidence, fmt.Sprintf("sequence_count=%d", sequence.Count), CGEMaxEvidencePerBehavior)
	if sequence.RealCount == 0 {
		behavior.Context["simulation_only"] = true
	}
	g.applyBehaviorOverrideLocked(behavior)
	g.pruneInspectionLocked()
}

func learnedEventFromCGE(event *contracts.Event) learnedEvent {
	metadata := nestedMetadata(event.Metadata)
	eventType := metadataString(event.Metadata["raw_type"])
	if eventType == "" {
		eventType = event.Type
	}
	deviceID := metadataString(event.Metadata["device_id"])
	identity := metadataString(event.Metadata["identity"])
	sourceType := metadataString(event.Metadata["source_type"])
	if sourceType == "" {
		sourceType = string(event.SubjectType)
	}
	return learnedEvent{
		EventType:       eventType,
		SourceType:      sourceType,
		DeviceID:        deviceID,
		NodeID:          event.TopologyNode,
		Identity:        identity,
		Signature:       eventSignature(eventType, deviceID, event.TopologyNode, identity),
		Timestamp:       event.Timestamp,
		Simulated:       metadataBool(metadata["simulated"]),
		TestRunID:       metadataString(metadata["test_run_id"]),
		ScenarioID:      metadataString(metadata["scenario_id"]),
		ScenarioStepID:  metadataString(metadata["scenario_step_id"]),
		EventInstanceID: metadataString(metadata["event_instance_id"]),
		ZoneRole:        firstNonEmpty(metadataString(event.Metadata["zone_role"]), metadataString(event.Metadata["device_role"])),
		ZoneScope:       metadataString(event.Metadata["zone_scope"]),
		HouseState:      metadataString(event.Metadata["house_state"]),
		TimeWindow:      timeWindowFromMetadata(event.Metadata),
	}
}

func isSimulated(event *contracts.Event) bool {
	return metadataBool(nestedMetadata(event.Metadata)["simulated"])
}

func learningMode(event *contracts.Event) string {
	mode := metadataString(nestedMetadata(event.Metadata)["learning_mode"])
	if mode == "" {
		return "real"
	}
	return strings.ToLower(mode)
}

func validationLearningEnabled(event *contracts.Event) bool {
	metadata := nestedMetadata(event.Metadata)
	if !metadataBool(metadata["validation"]) {
		return true
	}
	return metadataBool(metadata["learn"])
}

func nestedMetadata(metadata map[string]any) map[string]any {
	nested, _ := metadata["metadata"].(map[string]any)
	if nested == nil {
		return map[string]any{}
	}
	return nested
}

func eventSignature(eventType string, deviceID string, nodeID string, identity string) string {
	parts := []string{eventType}
	if deviceID != "" {
		parts = append(parts, "device="+deviceID)
	}
	if nodeID != "" {
		parts = append(parts, "node="+nodeID)
	}
	if identity != "" {
		parts = append(parts, "identity="+identity)
	}
	return strings.Join(parts, "|")
}

func sequenceKey(events []learnedEvent) string {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		parts = append(parts, event.Signature)
	}
	return strings.Join(parts, " > ")
}

func sequenceSignature(events []learnedEvent) string {
	return strings.Join(eventTypes(events), " > ")
}

func eventTypes(events []learnedEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.EventType)
	}
	return out
}

func sourceTypes(events []learnedEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.SourceType)
	}
	return out
}

func devices(events []learnedEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.DeviceID)
	}
	return out
}

func nodes(events []learnedEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.NodeID)
	}
	return out
}

func identities(events []learnedEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Identity)
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func allSimulated(events []learnedEvent) bool {
	for _, event := range events {
		if !event.Simulated {
			return false
		}
	}
	return true
}

func sequenceAverageDelta(events []learnedEvent) int64 {
	if len(events) < 2 {
		return 0
	}
	var total int64
	for i := 1; i < len(events); i++ {
		total += events[i].Timestamp.Sub(events[i-1].Timestamp).Milliseconds()
	}
	return total / int64(len(events)-1)
}

func rollingAverage(previous int64, current int64, count int) int64 {
	if count <= 1 || previous == 0 {
		return current
	}
	return (previous*int64(count-1) + current) / int64(count)
}

func confidence(count int) float64 {
	if count <= 0 {
		return 0
	}
	return float64(count) / float64(count+2)
}

func exampleID(events []learnedEvent) string {
	last := events[len(events)-1]
	if last.EventInstanceID != "" {
		return last.EventInstanceID
	}
	if last.TestRunID != "" {
		return last.TestRunID
	}
	return last.Signature
}

func appendLimited(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	values = append(values, value)
	if len(values) > limit {
		return values[len(values)-limit:]
	}
	return values
}

func metadataString(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

func metadataBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func timeWindowFromMetadata(metadata map[string]any) string {
	hour := -1
	switch typed := metadata["hour"].(type) {
	case int:
		hour = typed
	case int64:
		hour = int(typed)
	case float64:
		hour = int(typed)
	}
	if hour >= 22 || (hour >= 0 && hour < 6) {
		return "night"
	}
	if hour >= 6 {
		return "day"
	}
	return ""
}

func shortHash(value string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%08x", h.Sum32())
}
