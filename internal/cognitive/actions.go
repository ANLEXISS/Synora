package cognitive

import (
	"fmt"
	"sort"
	"strings"
)

const ActionCatalogSchemaVersion = "action-catalog/v1"

// ActionDescriptor is an action identity and safety envelope, never an
// executable command. Selection only references IDs from this catalog.
type ActionDescriptor struct {
	ID                      string   `json:"id"`
	Capability              string   `json:"capability"`
	RiskClass               string   `json:"risk_class"`
	Enabled                 bool     `json:"enabled"`
	RequiresTeacherApproval bool     `json:"requires_teacher_approval"`
	ParameterNames          []string `json:"parameter_names,omitempty"`
	// Slot is a generic token@scope identity, never an executable command.
	// It lets the model head consume the deterministic Device Store snapshot.
	Slot string `json:"slot,omitempty"`
}

type ActionCatalog struct {
	SchemaVersion string             `json:"schema_version"`
	Revision      uint64             `json:"revision"`
	Actions       []ActionDescriptor `json:"actions"`
}

func (c ActionCatalog) Validate() error {
	if c.SchemaVersion != ActionCatalogSchemaVersion {
		return fmt.Errorf("invalid action catalog schema version %q", c.SchemaVersion)
	}
	seen := map[string]struct{}{}
	seenSlots := map[string]struct{}{}
	for _, action := range c.Actions {
		if strings.TrimSpace(action.ID) == "" || strings.TrimSpace(action.Capability) == "" || strings.TrimSpace(action.RiskClass) == "" {
			return fmt.Errorf("invalid action descriptor")
		}
		if _, exists := seen[action.ID]; exists {
			return fmt.Errorf("duplicate action descriptor %q", action.ID)
		}
		seen[action.ID] = struct{}{}
		if slot := actionSlot(action); slot != "" {
			if _, exists := seenSlots[slot]; exists {
				return fmt.Errorf("duplicate action slot %q", slot)
			}
			seenSlots[slot] = struct{}{}
		}
	}
	return nil
}

func (c ActionCatalog) Normalized() ActionCatalog {
	c.SchemaVersion = ActionCatalogSchemaVersion
	c.Actions = append([]ActionDescriptor(nil), c.Actions...)
	sort.Slice(c.Actions, func(i, j int) bool { return c.Actions[i].ID < c.Actions[j].ID })
	for i := range c.Actions {
		c.Actions[i].ParameterNames = append([]string(nil), c.Actions[i].ParameterNames...)
		sort.Strings(c.Actions[i].ParameterNames)
	}
	return c
}

func (c ActionCatalog) Contains(id string) bool {
	for _, action := range c.Actions {
		if actionSlot(action) == id && action.Enabled {
			return true
		}
	}
	return false
}

const ActionLedgerSchemaVersion = "action-ledger/v1"

type ActionLedgerEntry struct {
	Slot   string `json:"slot"`
	Status string `json:"status"`
}

type ActionLedgerSnapshot struct {
	SchemaVersion string              `json:"schema_version"`
	IncidentOpen  bool                `json:"incident_open"`
	RecentActions []ActionLedgerEntry `json:"recent_actions,omitempty"`
}

func (l ActionLedgerSnapshot) Completed(slot string) (status string, ok bool) {
	for _, entry := range l.RecentActions {
		if entry.Slot == slot && (entry.Status == "executed" || entry.Status == "rejected") {
			return entry.Status, true
		}
	}
	return "", false
}

func actionSlot(action ActionDescriptor) string {
	if strings.TrimSpace(action.Slot) != "" {
		return strings.TrimSpace(action.Slot)
	}
	return strings.TrimSpace(action.ID)
}

type ActionLogit struct {
	ActionID    string  `json:"action_id"`
	Logit       float32 `json:"logit"`
	Probability float32 `json:"probability"`
}

type ScoredLabel struct {
	Label       string  `json:"label"`
	Probability float32 `json:"probability"`
	Threshold   float32 `json:"threshold,omitempty"`
	Selected    bool    `json:"selected"`
}

type FilteredAction struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type DangerLogits struct {
	None       float32 `json:"none"`
	Low        float32 `json:"low"`
	Medium     float32 `json:"medium"`
	MediumHigh float32 `json:"medium_high"`
	High       float32 `json:"high"`
	Critical   float32 `json:"critical"`
}

func (d DangerLogits) Values() []float32 {
	return []float32{d.None, d.Low, d.Medium, d.MediumHigh, d.High, d.Critical}
}

// ValidateActionSelection guarantees that a model can only name an enabled
// catalog entry. It still does not execute or dispatch anything.
func (o CognitiveOutput) ValidateActionSelection(catalog ActionCatalog) error {
	for _, selection := range o.SelectedActionIDs {
		if !catalog.Contains(selection) {
			return fmt.Errorf("selected action %q is not enabled in catalog", selection)
		}
	}
	for _, logit := range o.ActionLogits {
		if !catalog.Contains(logit.ActionID) {
			return fmt.Errorf("action logit %q is not enabled in catalog", logit.ActionID)
		}
	}
	return nil
}
