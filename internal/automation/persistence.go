package automation

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
	"synora/pkg/contract"
)

type yamlAutomations struct {
	Automations []Rule `yaml:"automations"`
}

type legacyAutomations struct {
	Automations []legacyRule `yaml:"automations"`
}

type legacyRule struct {
	ID         string            `yaml:"id"`
	Trigger    legacyTrigger     `yaml:"trigger"`
	Conditions []legacyCondition `yaml:"conditions"`
	Actions    []map[string]any  `yaml:"actions"`
	State      string            `yaml:"state"`
	Node       string            `yaml:"node"`
	EventType  string            `yaml:"event"`
	MinScore   float64           `yaml:"min_score"`
}

type legacyTrigger struct {
	Type string `yaml:"type"`
	Node string `yaml:"node"`
}

func (t *legacyTrigger) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		t.Type = value.Value
		return nil
	case yaml.MappingNode:
		type alias legacyTrigger
		var out alias
		if err := value.Decode(&out); err != nil {
			return err
		}
		*t = legacyTrigger(out)
		return nil
	default:
		return nil
	}
}

type legacyCondition struct {
	Field string `yaml:"field"`
	Type  string `yaml:"type"`
	Op    string `yaml:"op"`
	Value any    `yaml:"value"`
	From  string `yaml:"from"`
	To    string `yaml:"to"`
}

func LoadFromFile(path string) ([]Rule, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// ---- Nouveau format ----

	var current yamlAutomations
	if err := yaml.Unmarshal(data, &current); err == nil && len(current.Automations) > 0 {
		if isCurrentFormat(current.Automations) {
			for i := range current.Automations {
				current.Automations[i] = normalizeRule(current.Automations[i])
			}
			return current.Automations, nil
		}
	}

	// ---- Ancien format (compatibilité) ----

	var legacy legacyAutomations
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}

	out := make([]Rule, 0, len(legacy.Automations))

	for _, item := range legacy.Automations {

		rule := Rule{
			ID:        item.ID,
			Enabled:   true,
			EventType: item.EventType,
			State:     item.State,
			Node:      item.Node,
			MinScore:  item.MinScore,
		}

		if rule.EventType == "" {
			rule.EventType = item.Trigger.Type
		}

		if rule.Node == "" {
			rule.Node = item.Trigger.Node
		}

		// ---- Conditions / schedule ----

		for _, condition := range item.Conditions {

			field := condition.Field
			if field == "" {
				field = condition.Type
			}

			if field == "time" {
				rule.Schedule = &Schedule{
					Start: condition.From,
					End:   condition.To,
				}
				continue
			}

			op := condition.Op
			if op == "" {
				op = "=="
			}

			if strings.TrimSpace(field) == "" {
				continue
			}

			rule.Conditions = append(
				rule.Conditions,
				contract.Condition{
					Field: field,
					Op:    op,
					Value: condition.Value,
				},
			)
		}

		// ---- Actions ----

		for _, action := range item.Actions {

			a := AutomationAction{Enabled: true}

			if t, ok := action["type"].(string); ok {
				a.Type = t
			}

			if d, ok := action["device"].(string); ok {
				a.Device = d
				if a.Target == "" {
					a.Target = d
				}
			}

			if c, ok := action["command"].(string); ok {
				a.Command = c
			}

			if v, ok := action["value"]; ok {
				a.Value = v
			}

			if ch, ok := action["channel"].(string); ok {
				a.Channel = ch
				if a.Target == "" {
					a.Target = ch
				}
			}

			if r, ok := action["residents"].([]any); ok {
				for _, x := range r {
					if s, ok := x.(string); ok {
						a.Residents = append(a.Residents, s)
					}
				}
			}

			rule.Actions = append(rule.Actions, a)
		}

		out = append(out, normalizeRule(rule))
	}

	return out, nil
}

func isCurrentFormat(rules []Rule) bool {
	for _, rule := range rules {
		if rule.EventType != "" ||
			rule.State != "" ||
			rule.Node != "" ||
			rule.MinScore != 0 ||
			rule.Schedule != nil {
			continue
		}
		for _, condition := range rule.Conditions {
			if strings.TrimSpace(condition.Field) == "" {
				return false
			}
		}
	}
	return true
}

func SaveToFile(path string, rules []Rule) error {

	y := yamlAutomations{
		Automations: rules,
	}

	data, err := yaml.Marshal(&y)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
