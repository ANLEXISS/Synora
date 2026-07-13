package contract

import "time"

const (
	RPCRuntimeHealth         = "runtime.health"
	RPCRuntimeRestartService = "runtime.restart_service"
	RPCRuntimeSnapshot       = "runtime.snapshot"
	RPCRuntimeRollback       = "runtime.rollback"
	RPCSystemResetState      = "system.reset_state"
	RPCManualRisk            = "system.manual_risk"
)

type RuntimeHealth struct {
	Status      string                          `json:"status"`
	GeneratedAt time.Time                       `json:"generated_at"`
	Services    map[string]RuntimeServiceHealth `json:"services"`
	Components  map[string]RuntimeServiceHealth `json:"components,omitempty"`
	Network     RuntimeNetworkHealth            `json:"network"`
	MediaMTX    RuntimeMediaMTXHealth           `json:"mediamtx"`
	Disk        RuntimeDiskHealth               `json:"disk"`
	Uptime      int64                           `json:"uptime"`
	Timestamp   time.Time                       `json:"timestamp"`
}

type RuntimeServiceHealth struct {
	Name    string    `json:"name"`
	Status  string    `json:"status"`
	Active  bool      `json:"active"`
	Checked time.Time `json:"checked_at"`
	Error   string    `json:"error,omitempty"`
}

type RuntimeNetworkHealth struct {
	Status  string                          `json:"status"`
	HostAPD RuntimeServiceHealth            `json:"hostapd"`
	DNSMasq RuntimeServiceHealth            `json:"dnsmasq"`
	Details map[string]RuntimeServiceHealth `json:"details,omitempty"`
}

type RuntimeMediaMTXHealth struct {
	Status  string               `json:"status"`
	Service RuntimeServiceHealth `json:"service"`
}

type RuntimeDiskHealth struct {
	Path        string `json:"path"`
	TotalBytes  uint64 `json:"total_bytes"`
	FreeBytes   uint64 `json:"free_bytes"`
	UsedBytes   uint64 `json:"used_bytes"`
	UsedPercent int    `json:"used_percent"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

type RuntimeRestartServiceRequest struct {
	Service string `json:"service"`
}

type RuntimeRestartServiceResult struct {
	Service   string    `json:"service"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type RuntimeSnapshotRequest struct {
	Name string `json:"name,omitempty"`
}

type RuntimeSnapshotResult struct {
	Path      string    `json:"path"`
	Source    string    `json:"source"`
	SizeBytes int64     `json:"size_bytes"`
	Timestamp time.Time `json:"timestamp"`
}

type RuntimeRollbackRequest struct {
	Snapshot string `json:"snapshot,omitempty"`
}

type RuntimeRollbackResult struct {
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

type SystemStateResetRequest struct {
	TargetState string `json:"target_state"`
	Reason      string `json:"reason"`
	CreatedBy   string `json:"created_by,omitempty"`
}

type ManualRiskRequest struct {
	DangerLevel     string `json:"danger_level"`
	DurationSeconds int    `json:"duration_seconds"`
	Reason          string `json:"reason"`
	Test            bool   `json:"test,omitempty"`
	CreatedBy       string `json:"created_by,omitempty"`
}
