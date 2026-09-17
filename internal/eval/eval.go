// Package eval provides a small JSONL harness for comparing a future backend
// with the deterministic teacher contract.
package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitive"
)

type Fixture struct {
	ID       string                   `json:"id"`
	Input    cognitive.CognitiveInput `json:"input"`
	Expected Expectation              `json:"expected"`
}

type Expectation struct {
	Classification        string   `json:"classification,omitempty"`
	InferredState         string   `json:"inferred_state,omitempty"`
	RequestedCapabilities []string `json:"requested_capabilities,omitempty"`
	AdvisoryOnly          *bool    `json:"advisory_only,omitempty"`
	DangerLabel           string   `json:"danger_label,omitempty"`
	IncidentPhase         string   `json:"incident_phase,omitempty"`
	IncidentTags          []string `json:"incident_tags,omitempty"`
	Tasks                 []string `json:"tasks,omitempty"`
	ProposedActionIDs     []string `json:"proposed_action_ids,omitempty"`
}

type CaseResult struct {
	ID     string   `json:"id"`
	Passed bool     `json:"passed"`
	Errors []string `json:"errors,omitempty"`
}

type Report struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	Total         int          `json:"total"`
	Passed        int          `json:"passed"`
	Failed        int          `json:"failed"`
	Cases         []CaseResult `json:"cases"`
}

func Run(fixtures io.Reader, outputs io.Reader) (Report, error) {
	items, err := loadFixtures(fixtures)
	if err != nil {
		return Report{}, err
	}
	actual, unsafeOutputs, err := loadOutputs(outputs)
	if err != nil {
		return Report{}, err
	}
	report := Report{SchemaVersion: "synora-eval/v1", GeneratedAt: time.Now().UTC(), Total: len(items), Cases: make([]CaseResult, 0, len(items))}
	for _, fixture := range items {
		result := CaseResult{ID: fixture.ID, Passed: true}
		if err := fixture.Input.Validate(); err != nil {
			result.Errors = append(result.Errors, "input: "+err.Error())
		} else if fixture.Input.SchemaVersion != cognitive.SchemaVersion {
			result.Errors = append(result.Errors, "input: unsupported schema version")
		}
		output, ok := actual[fixture.ID]
		if !ok {
			output, ok = actual[fixture.Input.RequestID]
		}
		if !ok {
			result.Errors = append(result.Errors, "missing output")
		} else {
			if err := output.Validate(); err != nil {
				result.Errors = append(result.Errors, "output: "+err.Error())
			}
			if output.SchemaVersion != cognitive.SchemaVersion {
				result.Errors = append(result.Errors, "output: unsupported schema version")
			}
			if unsafeOutputs[fixture.ID] || unsafeOutputs[fixture.Input.RequestID] {
				result.Errors = append(result.Errors, "output contains executable action keys")
			}
			compare(resultErrors(&result), fixture.Expected, output)
		}
		if len(result.Errors) != 0 {
			result.Passed = false
			report.Failed++
		} else {
			report.Passed++
		}
		report.Cases = append(report.Cases, result)
	}
	return report, nil
}

func RunMock(fixtures io.Reader) (Report, error) {
	data, err := io.ReadAll(fixtures)
	if err != nil {
		return Report{}, err
	}
	items, err := loadFixtures(bytes.NewReader(data))
	if err != nil {
		return Report{}, err
	}
	var outputs bytes.Buffer
	for _, fixture := range items {
		output, runErr := cognitive.NewScheduler(cognitive.DefaultRegistry()).Run(context.Background(), fixture.Input)
		if runErr != nil {
			return Report{}, runErr
		}
		encoded, _ := json.Marshal(struct {
			FixtureID string `json:"fixture_id"`
			cognitive.CognitiveOutput
		}{fixture.ID, output})
		outputs.Write(encoded)
		outputs.WriteByte('\n')
	}
	return Run(bytes.NewReader(data), &outputs)
}

func resultErrors(result *CaseResult) *[]string { return &result.Errors }

func compare(errorsOut *[]string, expected Expectation, actual cognitive.CognitiveOutput) {
	if expected.Classification != "" && expected.Classification != actual.Classification {
		*errorsOut = append(*errorsOut, fmt.Sprintf("classification: expected %q got %q", expected.Classification, actual.Classification))
	}
	if expected.InferredState != "" && expected.InferredState != actual.InferredState {
		*errorsOut = append(*errorsOut, fmt.Sprintf("inferred_state: expected %q got %q", expected.InferredState, actual.InferredState))
	}
	if expected.AdvisoryOnly != nil && *expected.AdvisoryOnly != actual.AdvisoryOnly {
		*errorsOut = append(*errorsOut, "advisory_only mismatch")
	}
	if expected.DangerLabel != "" && expected.DangerLabel != actual.DangerLabel {
		*errorsOut = append(*errorsOut, "danger_label mismatch")
	}
	if expected.IncidentPhase != "" && expected.IncidentPhase != actual.IncidentPhase {
		*errorsOut = append(*errorsOut, "incident_phase mismatch")
	}
	if len(expected.IncidentTags) > 0 && !sameStrings(expected.IncidentTags, selectedLabels(actual.IncidentTags)) {
		*errorsOut = append(*errorsOut, "incident_tags mismatch")
	}
	if len(expected.Tasks) > 0 && !sameStrings(expected.Tasks, selectedLabels(actual.TaskScores)) {
		*errorsOut = append(*errorsOut, "tasks mismatch")
	}
	if len(expected.ProposedActionIDs) > 0 && !sameStrings(expected.ProposedActionIDs, actual.ProposedActionIDs) {
		*errorsOut = append(*errorsOut, "proposed_action_ids mismatch")
	}
	want := append([]string(nil), expected.RequestedCapabilities...)
	got := append([]string(nil), actual.RequestedCapabilities...)
	sort.Strings(want)
	sort.Strings(got)
	if len(want) != 0 && strings.Join(want, "\x00") != strings.Join(got, "\x00") {
		*errorsOut = append(*errorsOut, "requested_capabilities mismatch")
	}
}

func selectedLabels(values []cognitive.ScoredLabel) []string {
	result := []string{}
	for _, value := range values {
		if value.Selected {
			result = append(result, value.Label)
		}
	}
	return result
}
func sameStrings(left, right []string) bool {
	a := append([]string(nil), left...)
	b := append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func loadFixtures(reader io.Reader) ([]Fixture, error) {
	if reader == nil {
		return nil, fmt.Errorf("fixtures reader is nil")
	}
	var items []Fixture
	scanner := bufio.NewScanner(reader)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var fixture Fixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			return nil, fmt.Errorf("fixture line %d: %w", line, err)
		}
		if fixture.ID == "" {
			return nil, fmt.Errorf("fixture line %d: id is required", line)
		}
		items = append(items, fixture)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func loadOutputs(reader io.Reader) (map[string]cognitive.CognitiveOutput, map[string]bool, error) {
	if reader == nil {
		return map[string]cognitive.CognitiveOutput{}, map[string]bool{}, nil
	}
	outputs := map[string]cognitive.CognitiveOutput{}
	unsafe := map[string]bool{}
	scanner := bufio.NewScanner(reader)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var envelope struct {
			FixtureID string `json:"fixture_id"`
			cognitive.CognitiveOutput
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return nil, nil, fmt.Errorf("output line %d: %w", line, err)
		}
		key := envelope.FixtureID
		if key == "" {
			key = envelope.RequestID
		}
		if key == "" {
			return nil, nil, fmt.Errorf("output line %d: fixture_id or request_id is required", line)
		}
		outputs[key] = envelope.CognitiveOutput
		unsafe[key] = unsafe[key] || hasExecutableKeys(scanner.Bytes())
	}
	return outputs, unsafe, scanner.Err()
}

func hasExecutableKeys(data []byte) bool {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return true
	}
	return walkExecutableKeys(value)
}

func walkExecutableKeys(value any) bool {
	for key, child := range asMap(value) {
		switch strings.ToLower(key) {
		case "actions", "commands", "command", "executable_actions", "execute", "execution", "dispatch", "tool_calls":
			return true
		}
		if walkExecutableKeys(child) {
			return true
		}
	}
	if list, ok := value.([]any); ok {
		for _, child := range list {
			if walkExecutableKeys(child) {
				return true
			}
		}
	}
	return false
}

func asMap(value any) map[string]any {
	mapValue, _ := value.(map[string]any)
	return mapValue
}
