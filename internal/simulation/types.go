package simulation

import "time"

const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	ModeDryRun      = "dry_run"
	ModeLiveEvents  = "live_events"
	ModeRealActions = "real_actions"

	GeneratedBySynoraLab        = "synora-lab"
	GeneratedBySimulationEngine = "simulation-engine"
	GeneratedByFrontendTestMode = "frontend-test-mode"
)

type SimulationRun struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	ScenarioID string         `json:"scenario_id,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
	Status     string         `json:"status"`
	Mode       string         `json:"mode"`
	CreatedBy  string         `json:"created_by,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type SimulationEventMetadata struct {
	Simulated      bool   `json:"simulated"`
	TestRunID      string `json:"test_run_id"`
	ScenarioID     string `json:"scenario_id,omitempty"`
	ScenarioStepID string `json:"scenario_step_id,omitempty"`
	DryRun         bool   `json:"dry_run,omitempty"`
	GeneratedBy    string `json:"generated_by"`
}

type Scenario struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Steps       []ScenarioStep `json:"steps"`
}

type ScenarioStep struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	DelayMs    int            `json:"delay_ms,omitempty"`
	EventType  string         `json:"event_type"`
	DeviceID   string         `json:"device_id,omitempty"`
	CameraID   string         `json:"camera_id,omitempty"`
	NodeID     string         `json:"node_id,omitempty"`
	Identity   string         `json:"identity,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

type EventBuildOptions struct {
	Type        string
	DeviceID    string
	CameraID    string
	NodeID      string
	Identity    string
	Confidence  float64
	TrackID     string
	ClipID      string
	ClipPath    string
	Now         time.Time
	Run         *SimulationRun
	ScenarioID  string
	StepID      string
	DryRun      bool
	GeneratedBy string
	Data        map[string]any
}
