package main

import (
	"fmt"
	"strings"

	"synora/pkg/contract"
)

func renderDanger(snapshot *contract.PublicSnapshot) string {
	if snapshot == nil {
		return "Danger assessments unavailable: snapshot unavailable\n"
	}
	var out strings.Builder
	out.WriteString("CGE danger assessments\n")
	assessments := dangerAssessments(snapshot)
	if len(assessments) == 0 {
		out.WriteString("- none\n")
		return out.String()
	}
	start := 0
	if len(assessments) > 10 {
		start = len(assessments) - 10
	}
	for _, assessment := range assessments[start:] {
		out.WriteString(fmt.Sprintf(
			"- level: %d score: %.2f category: %s validation_required: %v\n",
			int(numberValue(assessment["level"])),
			numberValue(assessment["score"]),
			valueString(assessment["category"]),
			assessment["validation_required"],
		))
		if explanation := valueString(assessment["explanation"]); explanation != "" {
			out.WriteString("  explanation: " + explanation + "\n")
		}
		if actions := systemActionTypes(assessment); len(actions) > 0 {
			out.WriteString("  recommended_system_actions: " + strings.Join(actions, ", ") + "\n")
		}
	}
	return out.String()
}

func expectDanger(snapshot *contract.PublicSnapshot, cfg Config) error {
	if !hasDangerExpectations(cfg) {
		return nil
	}
	latest := latestDangerAssessment(snapshot)
	if latest == nil {
		return fmt.Errorf("expected danger assessment, got none")
	}
	if cfg.ExpectDangerLevel >= 0 && int(numberValue(latest["level"])) != cfg.ExpectDangerLevel {
		return fmt.Errorf("expected danger level %d, got %d", cfg.ExpectDangerLevel, int(numberValue(latest["level"])))
	}
	if cfg.ExpectCategory != "" && valueString(latest["category"]) != cfg.ExpectCategory {
		return fmt.Errorf("expected danger category %s, got %s", cfg.ExpectCategory, valueString(latest["category"]))
	}
	if cfg.ExpectSystemAction != "" && !containsSystemAction(latest, cfg.ExpectSystemAction) {
		return fmt.Errorf("expected system action %s, got %v", cfg.ExpectSystemAction, systemActionTypes(latest))
	}
	return nil
}

func latestDangerAssessment(snapshot *contract.PublicSnapshot) map[string]any {
	assessments := dangerAssessments(snapshot)
	if len(assessments) == 0 {
		return nil
	}
	return assessments[len(assessments)-1]
}

func dangerAssessments(snapshot *contract.PublicSnapshot) []map[string]any {
	return cgeCollection(snapshot, "danger_assessments")
}

func containsSystemAction(assessment map[string]any, actionType string) bool {
	for _, current := range systemActionTypes(assessment) {
		if current == actionType {
			return true
		}
	}
	return false
}

func systemActionTypes(assessment map[string]any) []string {
	raw, ok := assessment["recommended_system_actions"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		if mappedItems, ok := raw.([]map[string]any); ok {
			out := make([]string, 0, len(mappedItems))
			for _, item := range mappedItems {
				if actionType := valueString(item["type"]); actionType != "" {
					out = append(out, actionType)
				}
			}
			return out
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		mapped, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if actionType := valueString(mapped["type"]); actionType != "" {
			out = append(out, actionType)
		}
	}
	return out
}
