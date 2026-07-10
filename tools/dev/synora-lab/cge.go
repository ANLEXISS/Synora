package main

import (
	"fmt"
	"sort"
	"strings"

	"synora/internal/simulation"
	"synora/pkg/contract"
)

func renderCGE(snapshot *contract.PublicSnapshot) string {
	if snapshot == nil {
		return "CGE unavailable: snapshot unavailable\n"
	}
	var out strings.Builder
	out.WriteString("CGE learned sequences\n")
	sequences := cgeCollection(snapshot, "sequences")
	if len(sequences) == 0 {
		out.WriteString("- none\n")
	} else {
		sort.Slice(sequences, func(i, j int) bool {
			return numberValue(sequences[i]["count"]) > numberValue(sequences[j]["count"])
		})
		for _, sequence := range sequences {
			out.WriteString(fmt.Sprintf(
				"- signature: %s count: %d confidence: %.2f simulated_count: %d real_count: %d status: %s\n",
				valueString(sequence["signature"]),
				int(numberValue(sequence["count"])),
				numberValue(sequence["confidence"]),
				int(numberValue(sequence["simulated_count"])),
				int(numberValue(sequence["real_count"])),
				learningStatus(sequence),
			))
			if evidence := stringSlice(sequence["evidence"]); len(evidence) > 0 {
				out.WriteString("  evidence: " + strings.Join(evidence, "; ") + "\n")
			}
		}
	}

	out.WriteString("CGE learned transitions\n")
	transitions := cgeCollection(snapshot, "transitions")
	if len(transitions) == 0 {
		out.WriteString("- none\n")
	} else {
		sort.Slice(transitions, func(i, j int) bool {
			return numberValue(transitions[i]["count"]) > numberValue(transitions[j]["count"])
		})
		for _, transition := range transitions {
			out.WriteString(fmt.Sprintf(
				"- %s -> %s count: %d confidence: %.2f simulated_count: %d real_count: %d\n",
				valueString(transition["from_event_type"]),
				valueString(transition["to_event_type"]),
				int(numberValue(transition["count"])),
				numberValue(transition["confidence"]),
				int(numberValue(transition["simulated_count"])),
				int(numberValue(transition["real_count"])),
			))
		}
	}

	out.WriteString("CGE learned behaviors\n")
	behaviors := cgeCollection(snapshot, "learned_behaviors")
	if len(behaviors) == 0 {
		out.WriteString("- none\n")
	} else {
		for _, behavior := range behaviors {
			out.WriteString(fmt.Sprintf(
				"- trigger: %s status: %s count: %d confidence: %.2f simulated_count: %d real_count: %d requires_validation: %v\n",
				valueString(behavior["trigger_sequence_signature"]),
				valueString(behavior["status"]),
				int(numberValue(behavior["count"])),
				numberValue(behavior["confidence"]),
				int(numberValue(behavior["simulated_count"])),
				int(numberValue(behavior["real_count"])),
				behavior["requires_validation"],
			))
		}
	}

	return out.String()
}

func expectScenarioSequence(snapshot *contract.PublicSnapshot, scenarioID string) error {
	expected, err := scenarioSignature(scenarioID)
	if err != nil {
		return err
	}
	for _, sequence := range cgeCollection(snapshot, "sequences") {
		if valueString(sequence["signature"]) == expected && numberValue(sequence["count"]) > 0 {
			return nil
		}
	}
	return fmt.Errorf("expected CGE sequence %q was not learned", expected)
}

func scenarioSignature(scenarioID string) (string, error) {
	scenario, ok := simulation.ScenarioByID(scenarioID)
	if !ok {
		return "", fmt.Errorf("unknown scenario %q", scenarioID)
	}
	types := make([]string, 0, len(scenario.Steps))
	for _, step := range scenario.Steps {
		types = append(types, step.EventType)
	}
	return strings.Join(types, " > "), nil
}

func cgeCollection(snapshot *contract.PublicSnapshot, key string) []map[string]any {
	if snapshot == nil || snapshot.CGE == nil {
		return nil
	}
	raw, ok := snapshot.CGE[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func learningStatus(sequence map[string]any) string {
	if numberValue(sequence["simulated_count"]) > 0 && numberValue(sequence["real_count"]) == 0 {
		return "learned_in_simulation"
	}
	if numberValue(sequence["real_count"]) > 0 {
		return "learned_in_production"
	}
	return "observing"
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if current := valueString(item); current != "" {
				out = append(out, current)
			}
		}
		return out
	default:
		return nil
	}
}
