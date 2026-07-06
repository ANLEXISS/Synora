package automation

import (
	"sync"

	"synora/pkg/contract"
)

type Condition = contract.Condition

type Rule struct {
	ID string `json:"id" yaml:"id"`

	EventType string `json:"event" yaml:"event"`
	State     string `json:"state" yaml:"state"`
	Node      string `json:"node" yaml:"node"`

	MinScore        float64 `json:"min_score" yaml:"min_score"`
	ScoreMultiplier float64 `json:"score_multiplier" yaml:"score_multiplier"`
	ScoreOffset     float64 `json:"score_offset" yaml:"score_offset"`

	Conditions []Condition        `json:"conditions" yaml:"conditions"`
	Actions    []contract.Action `json:"actions" yaml:"actions"`
	Schedule   *Schedule          `json:"schedule" yaml:"schedule"`
}

type Schedule struct {
	Always bool   `json:"always" yaml:"always"`
	Start  string `json:"start" yaml:"start"`
	End    string `json:"end" yaml:"end"`
}

type Store struct {
	rules map[string]Rule
	mu    sync.RWMutex
}
