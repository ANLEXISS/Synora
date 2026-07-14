// Package actionpolicy owns the durable, safe-by-default reaction baseline.
package actionpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"synora/internal/configfile"
	"synora/pkg/contract"
)

var dangerLevels = []contract.DangerLevel{
	contract.DangerNone, contract.DangerLow, contract.DangerMedium,
	contract.DangerMediumHigh, contract.DangerHigh, contract.DangerCritical,
}

var knownCommands = map[string]bool{
	"observe": true, "store_evidence": true, "record.clip": true,
	"record_clip_if_available": true, "notify": true, "notify.whatsapp": true,
	"notify_owner": true, "notify_owner_urgent": true,
	"notify_owner_whatsapp": true, "mark_suspicious": true,
	"mark_intrusion_candidate": true, "increase_retention": true,
	"siren": true, "light.on": true, "device.command": true,
}

type Store struct {
	mu     sync.RWMutex
	path   string
	policy contract.ActionPolicy
}

func NewStore(path string) *Store {
	return &Store{path: strings.TrimSpace(path), policy: Defaults()}
}

func Defaults() contract.ActionPolicy {
	entry := func(id, command, target string, enabled bool, priority int) contract.ActionPolicyEntry {
		return contract.ActionPolicyEntry{ID: id, Command: command, Target: target, Enabled: enabled, Priority: priority}
	}
	levels := map[contract.DangerLevel]contract.ActionPolicyLevel{
		contract.DangerNone: {DangerLevel: contract.DangerNone, Enabled: true, Actions: []contract.ActionPolicyEntry{}},
		contract.DangerLow: {DangerLevel: contract.DangerLow, Enabled: true, Actions: []contract.ActionPolicyEntry{
			entry("observe", "observe", "", true, 10), entry("store_evidence_light", "store_evidence", "local", true, 20),
		}},
		contract.DangerMedium: {DangerLevel: contract.DangerMedium, Enabled: true, Actions: []contract.ActionPolicyEntry{
			entry("store_evidence", "store_evidence", "local", true, 30), entry("record_clip_if_available", "record.clip", "primary_device", true, 40),
		}},
		contract.DangerMediumHigh: {DangerLevel: contract.DangerMediumHigh, Enabled: true, Actions: []contract.ActionPolicyEntry{
			entry("notify_owner", "notify", "owner", true, 60), entry("record_clip_if_available", "record.clip", "primary_device", true, 50), entry("store_evidence", "store_evidence", "local", true, 40),
		}},
		contract.DangerHigh: {DangerLevel: contract.DangerHigh, Enabled: true, Actions: []contract.ActionPolicyEntry{
			{ID: "notify_owner_whatsapp", Command: "notify.whatsapp", Target: "owner", Enabled: true, Priority: 80, CooldownSeconds: 60, Template: "synora_security_alert", Message: "Alerte Synora : événement suspect détecté."},
			entry("record_clip_if_available", "record.clip", "primary_device", true, 70), entry("mark_intrusion_candidate", "mark_intrusion_candidate", "system", true, 60),
		}},
		contract.DangerCritical: {DangerLevel: contract.DangerCritical, Enabled: true, Actions: []contract.ActionPolicyEntry{
			{ID: "notify_owner_urgent", Command: "notify.whatsapp", Target: "owner", Enabled: true, Priority: 100, CooldownSeconds: 30, Template: "synora_security_alert", Message: "Alerte Synora critique."},
			entry("record_clip_if_available", "record.clip", "primary_device", true, 90), entry("store_evidence", "store_evidence", "local", true, 80), entry("mark_intrusion_candidate", "mark_intrusion_candidate", "system", true, 70), entry("increase_retention", "increase_retention", "evidence", true, 60),
			entry("optional_siren", "siren", "home", false, 100),
		}},
	}
	return contract.ActionPolicy{Levels: levels}
}

func (s *Store) Load() error {
	if s == nil || s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		s.policy = Defaults()
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		return err
	}
	policy, err := decode(data)
	if err != nil {
		return err
	}
	if err := Validate(policy); err != nil {
		return err
	}
	s.mu.Lock()
	s.policy = normalize(policy)
	s.mu.Unlock()
	return nil
}

func (s *Store) Get() contract.ActionPolicy {
	if s == nil {
		return Defaults()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.policy)
}

func (s *Store) Update(raw []byte) (contract.ActionPolicy, error) {
	if s == nil {
		return contract.ActionPolicy{}, errors.New("action policy unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	merged, err := mergePatch(s.policy, raw)
	if err != nil {
		return contract.ActionPolicy{}, err
	}
	merged = normalize(merged)
	if err := Validate(merged); err != nil {
		return contract.ActionPolicy{}, err
	}
	if err := s.write(merged); err != nil {
		return contract.ActionPolicy{}, err
	}
	s.policy = merged
	return clone(merged), nil
}

func (s *Store) Reset() (contract.ActionPolicy, error) {
	if s == nil {
		return contract.ActionPolicy{}, errors.New("action policy unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defaults := Defaults()
	if err := s.write(defaults); err != nil {
		return contract.ActionPolicy{}, err
	}
	s.policy = defaults
	return clone(defaults), nil
}

func (s *Store) write(policy contract.ActionPolicy) error {
	if s.path == "" {
		return errors.New("action policy path is empty")
	}
	data, err := yaml.Marshal(policy)
	if err != nil {
		return err
	}
	return configfile.WriteAtomicWithBackup(s.path, data, 0o640)
}

func decode(data []byte) (contract.ActionPolicy, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return contract.ActionPolicy{}, err
	}
	if nested, ok := raw["action_policy"].(map[string]any); ok {
		raw = nested
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return contract.ActionPolicy{}, err
	}
	var policy contract.ActionPolicy
	if err := json.Unmarshal(jsonData, &policy); err != nil {
		return contract.ActionPolicy{}, err
	}
	return mergeDefaults(policy), nil
}

func mergePatch(current contract.ActionPolicy, raw []byte) (contract.ActionPolicy, error) {
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(raw, &patch); err != nil {
		return contract.ActionPolicy{}, fmt.Errorf("invalid action policy JSON: %w", err)
	}
	baseBytes, _ := json.Marshal(current)
	var base map[string]any
	_ = json.Unmarshal(baseBytes, &base)
	levels, _ := base["levels"].(map[string]any)
	if levels == nil {
		levels = map[string]any{}
	}
	for key, value := range patch {
		if key == "levels" {
			var changes map[string]json.RawMessage
			if err := json.Unmarshal(value, &changes); err != nil {
				return contract.ActionPolicy{}, errors.New("levels must be an object")
			}
			for level, change := range changes {
				if !validLevel(level) {
					return contract.ActionPolicy{}, fmt.Errorf("unknown danger_level %q", level)
				}
				var currentLevel map[string]any
				levelBytes, _ := json.Marshal(levels[level])
				_ = json.Unmarshal(levelBytes, &currentLevel)
				var changeMap map[string]any
				if err := json.Unmarshal(change, &changeMap); err != nil {
					return contract.ActionPolicy{}, fmt.Errorf("level %s must be an object", level)
				}
				for field, fieldValue := range changeMap {
					currentLevel[field] = fieldValue
				}
				levels[level] = currentLevel
			}
			continue
		}
		if validLevel(key) {
			var currentLevel map[string]any
			levelBytes, _ := json.Marshal(levels[key])
			_ = json.Unmarshal(levelBytes, &currentLevel)
			var changeMap map[string]any
			if err := json.Unmarshal(value, &changeMap); err != nil {
				return contract.ActionPolicy{}, fmt.Errorf("level %s must be an object", key)
			}
			for field, fieldValue := range changeMap {
				currentLevel[field] = fieldValue
			}
			levels[key] = currentLevel
			continue
		}
		return contract.ActionPolicy{}, fmt.Errorf("unknown action policy field %q", key)
	}
	base["levels"] = levels
	mergedBytes, _ := json.Marshal(base)
	var updated contract.ActionPolicy
	if err := json.Unmarshal(mergedBytes, &updated); err != nil {
		return contract.ActionPolicy{}, err
	}
	return updated, nil
}

func mergeDefaults(policy contract.ActionPolicy) contract.ActionPolicy {
	defaults := Defaults()
	if policy.Levels == nil {
		policy.Levels = map[contract.DangerLevel]contract.ActionPolicyLevel{}
	}
	for _, level := range dangerLevels {
		item, ok := policy.Levels[level]
		if !ok {
			policy.Levels[level] = defaults.Levels[level]
			continue
		}
		item.DangerLevel = level
		if item.Actions == nil {
			item.Actions = []contract.ActionPolicyEntry{}
		}
		policy.Levels[level] = item
	}
	return policy
}

func normalize(policy contract.ActionPolicy) contract.ActionPolicy {
	policy = mergeDefaults(policy)
	for level, item := range policy.Levels {
		for i := range item.Actions {
			item.Actions[i].Command = strings.ToLower(strings.TrimSpace(item.Actions[i].Command))
			item.Actions[i].ID = strings.TrimSpace(item.Actions[i].ID)
			item.Actions[i].Target = strings.TrimSpace(item.Actions[i].Target)
		}
		policy.Levels[level] = item
	}
	return policy
}

func Validate(policy contract.ActionPolicy) error {
	policy = mergeDefaults(policy)
	if len(policy.Levels) != len(dangerLevels) {
		return errors.New("action policy must define every danger level")
	}
	for _, level := range dangerLevels {
		item, ok := policy.Levels[level]
		if !ok || item.DangerLevel != level {
			return fmt.Errorf("danger_level %q is missing or invalid", level)
		}
		if len(item.Actions) > 50 {
			return fmt.Errorf("too many actions for %s", level)
		}
		for _, action := range item.Actions {
			if strings.TrimSpace(action.ID) == "" {
				return errors.New("action id is required")
			}
			if !knownCommands[strings.ToLower(strings.TrimSpace(action.Command))] {
				return fmt.Errorf("unknown action command %q", action.Command)
			}
			if action.CooldownSeconds < 0 || action.CooldownSeconds > 86400 {
				return errors.New("cooldown_seconds must be between 0 and 86400")
			}
			if action.Priority < 0 || action.Priority > 100 {
				return errors.New("priority must be between 0 and 100")
			}
			for _, condition := range action.Conditions {
				if strings.TrimSpace(condition.Field) == "" {
					return errors.New("action condition field is required")
				}
				if !validOperator(condition.Op) {
					return fmt.Errorf("invalid action condition operator %q", condition.Op)
				}
			}
		}
	}
	return nil
}

func validLevel(value string) bool {
	for _, level := range dangerLevels {
		if string(level) == value {
			return true
		}
	}
	return false
}
func validOperator(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "==", "!=", ">", ">=", "<", "<=", "exists", "not_exists":
		return true
	default:
		return false
	}
}

func clone(policy contract.ActionPolicy) contract.ActionPolicy {
	data, _ := json.Marshal(policy)
	var copy contract.ActionPolicy
	_ = json.Unmarshal(data, &copy)
	return copy
}

func Catalog() []contract.ActionCatalogEntry {
	return []contract.ActionCatalogEntry{
		{ID: "observe", Command: "observe", Description: "Conserver une observation sans action externe.", Available: true},
		{ID: "store_evidence", Command: "store_evidence", Description: "Conserver les éléments de preuve.", Available: true},
		{ID: "record_clip", Command: "record.clip", Description: "Demander l’enregistrement d’un clip.", Available: true},
		{ID: "record_clip_if_available", Command: "record_clip_if_available", Description: "Enregistrer si un recorder est disponible.", Available: true},
		{ID: "notify", Command: "notify", Description: "Notification abstraite via un provider.", Available: true},
		{ID: "notify_whatsapp", Command: "notify.whatsapp", Description: "Notification WhatsApp Cloud API.", Available: true},
		{ID: "notify_owner", Command: "notify_owner", Description: "Alias de notification du propriétaire.", Available: true},
		{ID: "notify_owner_urgent", Command: "notify_owner_urgent", Description: "Alias de notification urgente.", Available: true},
		{ID: "mark_suspicious", Command: "mark_suspicious", Description: "Marquer l’événement comme suspect.", Available: true},
		{ID: "mark_intrusion_candidate", Command: "mark_intrusion_candidate", Description: "Marquer un candidat à l’intrusion.", Available: true},
		{ID: "increase_retention", Command: "increase_retention", Description: "Augmenter la rétention des preuves.", Available: true},
		{ID: "siren", Command: "siren", Description: "Action physique sirène — confirmation explicite requise.", Dangerous: true, Available: false},
		{ID: "light.on", Command: "light.on", Description: "Allumer une lumière si un device est configuré.", Available: true},
		{ID: "device.command", Command: "device.command", Description: "Envoyer une commande générique à un device.", Dangerous: true, Available: true},
	}
}

func (s *Store) Evaluate(level contract.DangerLevel, event *contract.Event, decision *contract.Decision, security contract.SecurityModeState) []contract.PolicyActionDecision {
	if s == nil {
		return nil
	}
	item, ok := s.Get().Levels[level]
	if !ok {
		return nil
	}
	result := make([]contract.PolicyActionDecision, 0, len(item.Actions))
	for _, action := range item.Actions {
		value := contract.PolicyActionDecision{ID: action.ID, Command: action.Command, Target: action.Target, Source: "policy", Priority: action.Priority, CooldownSeconds: action.CooldownSeconds, Reason: fmt.Sprintf("danger_level %s policy", level), Enabled: action.Enabled, Template: action.Template, Message: action.Message}
		if !item.Enabled {
			value.Blocked = true
			value.BlockedReason = "policy_level_disabled"
		}
		if !action.Enabled && value.BlockedReason == "" {
			value.Blocked = true
			value.BlockedReason = "action_disabled"
		}
		if value.Command == "siren" && !action.Enabled && value.BlockedReason == "" {
			value.Blocked = true
			value.BlockedReason = "action_disabled"
		}
		if value.BlockedReason == "" && !conditionsMatch(action.Conditions, event, decision, security) {
			value.Blocked = true
			value.BlockedReason = "condition_not_met"
		}
		result = append(result, value)
	}
	return result
}

func conditionsMatch(conditions []contract.Condition, event *contract.Event, decision *contract.Decision, security contract.SecurityModeState) bool {
	for _, condition := range conditions {
		actual, exists := conditionValue(condition.Field, event, decision, security)
		matched := compare(condition.Op, actual, exists, condition.Value)
		if condition.Negate {
			matched = !matched
		}
		if !matched {
			return false
		}
	}
	return true
}

func conditionValue(field string, event *contract.Event, decision *contract.Decision, security contract.SecurityModeState) (any, bool) {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "security.armed", "armed", "is_armed":
		return security.Armed, true
	case "security.mode", "security_mode", "mode":
		return string(security.Mode), true
	case "danger.level", "danger_level", "cge.danger_level":
		if decision != nil {
			return decision.DangerLevel, decision.DangerLevel != ""
		}
	case "danger.score", "danger_score":
		if decision != nil {
			return decision.DangerScore, true
		}
	case "event.type", "event_type", "type":
		if event != nil {
			return event.Type, event.Type != ""
		}
	}
	if event != nil && event.Payload != nil {
		value, ok := event.Payload[field]
		return value, ok
	}
	return nil, false
}

func compare(op string, actual any, exists bool, expected any) bool {
	if op == "" {
		op = "=="
	}
	if op == "exists" {
		return exists
	}
	if op == "not_exists" {
		return !exists
	}
	if !exists {
		return false
	}
	if op == "==" {
		return fmt.Sprint(actual) == fmt.Sprint(expected)
	}
	if op == "!=" {
		return fmt.Sprint(actual) != fmt.Sprint(expected)
	}
	return false
}
