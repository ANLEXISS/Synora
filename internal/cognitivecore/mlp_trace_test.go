package cognitivecore

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestCPUBundleEmitsBoundedRedactedTraceFromRealLayers(t *testing.T) {
	bundle, err := LoadCPUBundle("../../build/cognitive-mlp-v1")
	if err != nil {
		t.Skipf("V1 bundle not available in this checkout: %v", err)
	}
	encoded, err := (SnapshotEncoder{}).Encode(context.Background(), CognitiveSnapshot{Topology: "protected_interior"})
	if err != nil {
		t.Fatal(err)
	}
	output, _, err := bundle.Run(context.Background(), encoded, CognitiveSnapshot{Topology: "protected_interior"})
	if err != nil {
		t.Fatal(err)
	}
	if output.Trace == nil || !output.Trace.Redacted || output.Trace.SchemaVersion != MLPTraceSchemaVersion {
		t.Fatalf("missing redacted trace: %#v", output.Trace)
	}
	if len(output.Trace.Topology.Heads) != len(HeadOrder) || len(output.Trace.Activations) != len(HeadOrder)*2 {
		t.Fatalf("unexpected topology/activation count: %#v", output.Trace)
	}
	if len(output.Trace.ActivePaths) > maxMLPActivePaths {
		t.Fatalf("active path limit exceeded: %d", len(output.Trace.ActivePaths))
	}
	seenHeads := make(map[string]bool)
	actionOutputLayer := ""
	for _, head := range output.Trace.Topology.Heads {
		if len(head.Layers) == 0 {
			continue
		}
		if head.Name == "action" {
			actionOutputLayer = head.Layers[len(head.Layers)-1].ID
		}
	}
	for _, path := range output.Trace.ActivePaths {
		_, pathHead, _, _, ok := activePathGroupFor(path)
		if !ok {
			t.Fatalf("trace contains an unresolvable active path: %#v", path)
		}
		seenHeads[pathHead] = true
	}
	for _, head := range HeadOrder {
		if !seenHeads[head] {
			t.Fatalf("trace has no active path for head %q", head)
		}
	}
	if actionOutputLayer == "" {
		t.Fatal("action output layer is missing from topology")
	}
	actionOutputPath := false
	for _, path := range output.Trace.ActivePaths {
		if strings.HasPrefix(path.To, actionOutputLayer+".") {
			actionOutputPath = true
			break
		}
	}
	if !actionOutputPath {
		t.Fatalf("trace has action output nodes but no active path to %s", actionOutputLayer)
	}
	encodedTrace, err := json.Marshal(output.Trace)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ToLower(string(encodedTrace))
	for _, forbidden := range []string{"\"weights\"", "\"embedding\"", "\"media\"", "\"secret\"", "\"password\""} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("trace contains forbidden field %q: %s", forbidden, body)
		}
	}
	for _, summary := range output.Trace.Activations {
		if summary.Count == 0 || len(summary.ActiveNodes) > 5 {
			t.Fatalf("unbounded layer summary: %#v", summary)
		}
	}
}

func TestSelectBoundedActivePathsFairlyCoversHeadsAndTransitions(t *testing.T) {
	type transition struct {
		head     string
		from, to string
	}
	transitions := []transition{
		{head: "danger", from: "layer-1", to: "layer-2"},
		{head: "incident", from: "layer-1", to: "layer-2"},
		{head: "task", from: "layer-1", to: "layer-2"},
		{head: "action", from: "layer-1", to: "layer-2"},
		{head: "danger", from: "layer-2", to: "layer-3"},
		{head: "action", from: "layer-2", to: "layer-3"},
	}
	const pathsPerGroup = 20
	input := make([]MLPActivePath, 0, len(transitions)*pathsPerGroup)
	inputIDs := make(map[string]bool, cap(input))
	for _, item := range transitions {
		for index := 0; index < pathsPerGroup; index++ {
			path := MLPActivePath{
				From:     fmt.Sprintf("%s.%s.node-%d", item.head, item.from, index),
				To:       fmt.Sprintf("%s.%s.node-%d", item.head, item.to, index),
				Strength: 1000 - float32(index),
			}
			input = append(input, path)
			inputIDs[path.From+"\x00"+path.To] = true
		}
	}

	first := selectBoundedActivePaths(input, maxMLPActivePaths)
	second := selectBoundedActivePaths(input, maxMLPActivePaths)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("selection is not deterministic:\nfirst: %#v\nsecond: %#v", first, second)
	}
	if len(first) > maxMLPActivePaths {
		t.Fatalf("selection exceeded global limit: %d", len(first))
	}

	counts := make(map[string]int)
	minimumStrength := make(map[string]float32)
	for _, path := range first {
		if !inputIDs[path.From+"\x00"+path.To] {
			t.Fatalf("selector created a path not present in input: %#v", path)
		}
		groupKey, _, _, _, ok := activePathGroupFor(path)
		if !ok {
			t.Fatalf("selector returned an unresolvable path: %#v", path)
		}
		counts[groupKey]++
		if current, ok := minimumStrength[groupKey]; !ok || path.Strength < current {
			minimumStrength[groupKey] = path.Strength
		}
	}
	if len(counts) != len(transitions) {
		t.Fatalf("not every non-empty head/transition group was represented: %#v", counts)
	}
	minCount, maxCount := maxMLPActivePaths, 0
	for _, item := range transitions {
		groupKey := item.head + "\x00" + item.from + "->" + item.to
		count := counts[groupKey]
		if count == 0 {
			t.Fatalf("group %q was not reserved a path", groupKey)
		}
		if count < minCount {
			minCount = count
		}
		if count > maxCount {
			maxCount = count
		}
		if minimumStrength[groupKey] < 1000-float32(count-1) {
			t.Fatalf("group %q was not selected by redacted strength: count=%d minimum=%v", groupKey, count, minimumStrength[groupKey])
		}
	}
	if maxCount-minCount > 1 {
		t.Fatalf("group allocation is not equitable: min=%d max=%d counts=%#v", minCount, maxCount, counts)
	}
	if counts["action\x00layer-2->layer-3"] == 0 {
		t.Fatal("action output transition was not represented")
	}
}
