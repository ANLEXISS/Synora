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

	DangerLevel  string  `json:"danger_level,omitempty"`
	DangerScore  float64 `json:"danger_score,omitempty"`
	DangerSource string  `json:"danger_source,omitempty"`

	ClipID      string `json:"clip_id,omitempty"`
	TrackID     string `json:"track_id,omitempty"`
	GroupKey    string `json:"group_key,omitempty"`
	SequenceKey string `json:"sequence_key,omitempty"`

	GraphUsed                    bool                   `json:"graph_used,omitempty"`
	ValidationRequired           bool                   `json:"validation_required,omitempty"`
	ValidationReason             string                 `json:"validation_reason,omitempty"`
	ActionDecision               string                 `json:"action_decision,omitempty"`
	BlockedActions               []string               `json:"blocked_actions,omitempty"`
	RecommendedActionsFromCGE    []string               `json:"recommended_actions_from_cge,omitempty"`
	RecommendedActionsFromPolicy []string               `json:"recommended_actions_from_policy,omitempty"`
	PolicyActions                []PolicyActionDecision `json:"policy_actions,omitempty"`
	FinalActionPlan              []ActionPlanItem       `json:"final_action_plan,omitempty"`
	ActionDecisionReason         string                 `json:"action_decision_reason,omitempty"`
}

type decisionJSON struct {
	ID                           string                 `json:"id,omitempty"`
	Type                         string                 `json:"type"`
	Source                       string                 `json:"source,omitempty"`
	Timestamp                    time.Time              `json:"timestamp,omitempty"`
	Priority                     int                    `json:"priority,omitempty"`
	EventID                      string                 `json:"event_id,omitempty"`
	Score                        float64                `json:"score,omitempty"`
	EffectiveScore               float64                `json:"effective_score,omitempty"`
	Alert                        bool                   `json:"alert,omitempty"`
	Reason                       string                 `json:"reason,omitempty"`
	State                        string                 `json:"state,omitempty"`
	NodeID                       string                 `json:"node_id,omitempty"`
	DangerLevel                  string                 `json:"danger_level,omitempty"`
	DangerScore                  float64                `json:"danger_score,omitempty"`
	DangerSource                 string                 `json:"danger_source,omitempty"`
	ClipID                       string                 `json:"clip_id,omitempty"`
	TrackID                      string                 `json:"track_id,omitempty"`
	GroupKey                     string                 `json:"group_key,omitempty"`
	SequenceKey                  string                 `json:"sequence_key,omitempty"`
	GraphUsed                    bool                   `json:"graph_used,omitempty"`
	ValidationRequired           bool                   `json:"validation_required,omitempty"`
	ValidationReason             string                 `json:"validation_reason,omitempty"`
	ActionDecision               string                 `json:"action_decision,omitempty"`
	BlockedActions               []string               `json:"blocked_actions,omitempty"`
	RecommendedActionsFromCGE    []string               `json:"recommended_actions_from_cge,omitempty"`
	RecommendedActionsFromPolicy []string               `json:"recommended_actions_from_policy,omitempty"`
	PolicyActions                []PolicyActionDecision `json:"policy_actions,omitempty"`
	FinalActionPlan              []ActionPlanItem       `json:"final_action_plan,omitempty"`
	ActionDecisionReason         string                 `json:"action_decision_reason,omitempty"`
}

func (d *Decision) UnmarshalJSON(data []byte) error {
	var decoded decisionJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	applyLegacyDecisionFields(data, &decoded)

	*d = Decision{
		ID:                           decoded.ID,
		Type:                         decoded.Type,
		Source:                       decoded.Source,
		Timestamp:                    decoded.Timestamp,
		Priority:                     decoded.Priority,
		EventID:                      decoded.EventID,
		Score:                        decoded.Score,
		EffectiveScore:               decoded.EffectiveScore,
		Alert:                        decoded.Alert,
		Reason:                       decoded.Reason,
		State:                        decoded.State,
		NodeID:                       decoded.NodeID,
		DangerLevel:                  decoded.DangerLevel,
		DangerScore:                  decoded.DangerScore,
		DangerSource:                 decoded.DangerSource,
		ClipID:                       decoded.ClipID,
		TrackID:                      decoded.TrackID,
		GroupKey:                     decoded.GroupKey,
		SequenceKey:                  decoded.SequenceKey,
		GraphUsed:                    decoded.GraphUsed,
		ValidationRequired:           decoded.ValidationRequired,
		ValidationReason:             decoded.ValidationReason,
		ActionDecision:               decoded.ActionDecision,
		BlockedActions:               decoded.BlockedActions,
		RecommendedActionsFromCGE:    decoded.RecommendedActionsFromCGE,
		RecommendedActionsFromPolicy: decoded.RecommendedActionsFromPolicy,
		PolicyActions:                decoded.PolicyActions,
		FinalActionPlan:              decoded.FinalActionPlan,
		ActionDecisionReason:         decoded.ActionDecisionReason,
	}
	return nil
}

func applyLegacyDecisionFields(data []byte, decoded *decisionJSON) {
	legacy := struct {
		EventID            string  `json:"EventID,omitempty"`
		EffectiveScore     float64 `json:"EffectiveScore,omitempty"`
		NodeID             string  `json:"NodeID,omitempty"`
		ClipID             string  `json:"ClipID,omitempty"`
		TrackID            string  `json:"TrackID,omitempty"`
		GroupKey           string  `json:"GroupKey,omitempty"`
		SequenceKey        string  `json:"SequenceKey,omitempty"`
		GraphUsed          bool    `json:"GraphUsed,omitempty"`
		ValidationRequired bool    `json:"ValidationRequired,omitempty"`
		ValidationReason   string  `json:"ValidationReason,omitempty"`
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
	if decoded.ClipID == "" {
		decoded.ClipID = legacy.ClipID
	}
	if decoded.TrackID == "" {
		decoded.TrackID = legacy.TrackID
	}
	if decoded.GroupKey == "" {
		decoded.GroupKey = legacy.GroupKey
	}
	if decoded.SequenceKey == "" {
		decoded.SequenceKey = legacy.SequenceKey
	}
	if !decoded.GraphUsed {
		decoded.GraphUsed = legacy.GraphUsed
	}
	if !decoded.ValidationRequired {
		decoded.ValidationRequired = legacy.ValidationRequired
	}
	if decoded.ValidationReason == "" {
		decoded.ValidationReason = legacy.ValidationReason
	}
}
