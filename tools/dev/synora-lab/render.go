package main

import (
	"fmt"
	"strings"

	"synora/pkg/contract"
)

func renderSnapshot(snapshot *contract.PublicSnapshot, health *contract.RuntimeHealth, status string) string {
	var b strings.Builder
	b.WriteString("\033[H\033[2J")
	b.WriteString("SIMULATION - Synora Lab is a development simulator. Do not run in production.\n")
	if status != "" {
		b.WriteString("Status: " + status + "\n")
	}
	b.WriteString(strings.Repeat("=", 96) + "\n")
	left := section("SYSTEM / DEVICES", systemLines(snapshot, health))
	center := section("EVENTS / CLIPS / PRESENCE", eventLines(snapshot))
	right := section("ACTIONS / VALIDATIONS", actionLines(snapshot))
	b.WriteString(threeColumns(left, center, right, 30))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("=", 96) + "\n")
	b.WriteString("r refresh | 1 identity | 2 unknown | 3 uncertain | 4 motion | 5 weapon | 6 fall | 7 tamper | o online | x offline | s scenario | q quit\n")
	return b.String()
}

func section(title string, lines []string) []string {
	out := []string{title, strings.Repeat("-", len(title))}
	out = append(out, lines...)
	return out
}

func systemLines(snapshot *contract.PublicSnapshot, health *contract.RuntimeHealth) []string {
	if snapshot == nil {
		return []string{"snapshot unavailable"}
	}
	out := []string{
		"system: " + valueString(snapshot.System["last_state"]),
		fmt.Sprintf("metrics: %d keys", len(snapshot.Metrics)),
		fmt.Sprintf("devices: %d cameras: %d nodes: %d", len(snapshot.Devices), len(snapshot.Cameras), len(snapshot.Nodes)),
	}
	if health != nil {
		out = append(out, fmt.Sprintf("runtime services: %d", len(health.Services)))
		out = append(out, serviceLines(health)...)
	}
	out = append(out, compactItems("dev", snapshot.Devices, "id", "online", 8)...)
	out = append(out, compactItems("node", snapshot.Nodes, "id", "dynamic_score", 6)...)
	return out
}

func serviceLines(health *contract.RuntimeHealth) []string {
	if health == nil {
		return nil
	}
	out := make([]string, 0, len(health.Services))
	for name, service := range health.Services {
		status := service.Status
		if status == "" {
			if service.Active {
				status = "active"
			} else {
				status = "inactive"
			}
		}
		out = append(out, fmt.Sprintf("svc: %s %s", name, status))
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func eventLines(snapshot *contract.PublicSnapshot) []string {
	if snapshot == nil {
		return []string{"snapshot unavailable"}
	}
	out := []string{
		fmt.Sprintf("events: %d clips: %d", len(snapshot.Events), len(snapshot.Clips)),
		fmt.Sprintf("presence: %d identities: %d", len(snapshot.Presence), len(snapshot.Identities)),
	}
	out = append(out, compactItems("evt", snapshot.Events, "type", "device_id", 8)...)
	out = append(out, compactItems("clip", snapshot.Clips, "id", "camera_id", 4)...)
	out = append(out, compactItems("presence", snapshot.Presence, "id", "state", 4)...)
	return out
}

func actionLines(snapshot *contract.PublicSnapshot) []string {
	if snapshot == nil {
		return []string{"snapshot unavailable"}
	}
	pending := 0
	for _, item := range snapshot.Validations {
		if valueString(item["status"]) == contract.ValidationStatusPending {
			pending++
		}
	}
	out := []string{
		fmt.Sprintf("validations: %d pending: %d", len(snapshot.Validations), pending),
		fmt.Sprintf("action_results: %d", len(snapshot.ActionResults)),
	}
	out = append(out, compactItems("val", snapshot.Validations, "id", "status", 8)...)
	out = append(out, compactItems("act", snapshot.ActionResults, "type", "status", 8)...)
	return out
}

func compactItems(prefix string, items []map[string]any, keyA string, keyB string, limit int) []string {
	if len(items) == 0 {
		return nil
	}
	start := 0
	if len(items) > limit {
		start = len(items) - limit
	}
	out := make([]string, 0, len(items)-start)
	for _, item := range items[start:] {
		a := valueString(item[keyA])
		b := valueString(item[keyB])
		if a == "" || a == "<nil>" {
			a = valueString(item["id"])
		}
		out = append(out, fmt.Sprintf("%s: %s %s", prefix, a, b))
	}
	return out
}

func threeColumns(left []string, center []string, right []string, width int) string {
	max := len(left)
	if len(center) > max {
		max = len(center)
	}
	if len(right) > max {
		max = len(right)
	}
	var b strings.Builder
	for i := 0; i < max; i++ {
		b.WriteString(pad(lineAt(left, i), width))
		b.WriteString("  ")
		b.WriteString(pad(lineAt(center, i), width))
		b.WriteString("  ")
		b.WriteString(pad(lineAt(right, i), width))
		b.WriteString("\n")
	}
	return b.String()
}

func lineAt(lines []string, index int) string {
	if index < 0 || index >= len(lines) {
		return ""
	}
	return lines[index]
}

func pad(value string, width int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > width {
		if width <= 1 {
			return value[:width]
		}
		return value[:width-1] + "."
	}
	return value + strings.Repeat(" ", width-len(value))
}
