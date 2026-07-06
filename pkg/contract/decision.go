package contract

import (
	"encoding/json"
	"time"
)

type Decision struct {
	ID        string    `json:"id,omitempty"`
	Type      string    `json:"type"`
	Source    string    `json:"source,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Priority  int       `json:"priority,omitempty"`

	EventID        string  `json:"event_id,omitempty"`
	Score          float64 `json:"score,omitempty"`
	EffectiveScore float64 `json:"effective_score,omitempty"`
	Alert          bool    `json:"alert,omitempty"`
	Reason         string  `json:"reason,omitempty"`

	State  string `json:"state,omitempty"`
	NodeID string `json:"node_id,omitempty"`
}

type decisionJSON struct {
	ID             string    `json:"id,omitempty"`
	Type           string    `json:"type"`
	Source         string    `json:"source,omitempty"`
	Timestamp      time.Time `json:"timestamp,omitempty"`
	Priority       int       `json:"priority,omitempty"`
	EventID        string    `json:"event_id,omitempty"`
	Score          float64   `json:"score,omitempty"`
	EffectiveScore float64   `json:"effective_score,omitempty"`
	Alert          bool      `json:"alert,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	State          string    `json:"state,omitempty"`
	NodeID         string    `json:"node_id,omitempty"`
}

func (d *Decision) UnmarshalJSON(data []byte) error {
	var decoded decisionJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	applyLegacyDecisionFields(data, &decoded)

	*d = Decision{
		ID:             decoded.ID,
		Type:           decoded.Type,
		Source:         decoded.Source,
		Timestamp:      decoded.Timestamp,
		Priority:       decoded.Priority,
		EventID:        decoded.EventID,
		Score:          decoded.Score,
		EffectiveScore: decoded.EffectiveScore,
		Alert:          decoded.Alert,
		Reason:         decoded.Reason,
		State:          decoded.State,
		NodeID:         decoded.NodeID,
	}
	return nil
}

func applyLegacyDecisionFields(data []byte, decoded *decisionJSON) {
	legacy := struct {
		EventID        string  `json:"EventID,omitempty"`
		EffectiveScore float64 `json:"EffectiveScore,omitempty"`
		NodeID         string  `json:"NodeID,omitempty"`
	}{}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return
	}
	if decoded.EventID == "" {
		decoded.EventID = legacy.EventID
	}
	if decoded.EffectiveScore == 0 {
		decoded.EffectiveScore = legacy.EffectiveScore
	}
	if decoded.NodeID == "" {
		decoded.NodeID = legacy.NodeID
	}
}
