package contract

import (
	"strings"
	"time"
)

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
	Message string    `json:"message,omitempty"`
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

// NormalizeRuntimeHealth keeps the JSON health contract useful when a
// component did not answer the probe. Missing component objects are explicit
// degraded records instead of zero-value JSON objects.
func NormalizeRuntimeHealth(health RuntimeHealth, now time.Time) RuntimeHealth {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if health.GeneratedAt.IsZero() {
		health.GeneratedAt = now
	}
	if health.Timestamp.IsZero() {
		health.Timestamp = now
	}
	if health.Services == nil {
		health.Services = map[string]RuntimeServiceHealth{}
	}
	for _, name := range []string{"synora-api", "synora-bus", "synora-core", "synora-actions", "synora-discovery", "mediamtx"} {
		if _, ok := health.Services[name]; !ok {
			health.Services[name] = unavailableRuntimeService(name, now, missingServiceMessage(name))
		}
	}
	for name, item := range health.Services {
		if item.Name == "" {
			item.Name = name
		}
		if item.Status == "" {
			item.Status = "degraded"
		}
		if item.Checked.IsZero() {
			item.Checked = now
		}
		if item.Message == "" {
			item.Message = item.Error
		}
		health.Services[name] = item
	}
	if health.Components == nil {
		health.Components = map[string]RuntimeServiceHealth{}
	}
	for _, mapping := range []struct{ alias, service string }{
		{alias: "api", service: "synora-api"}, {alias: "bus", service: "synora-bus"},
		{alias: "core", service: "synora-core"}, {alias: "actions", service: "synora-actions"},
		{alias: "discovery", service: "synora-discovery"},
	} {
		if _, ok := health.Components[mapping.alias]; !ok {
			item := health.Services[mapping.service]
			item.Name = mapping.alias
			item.Status = componentRuntimeStatus(item.Status, item.Active)
			health.Components[mapping.alias] = item
		}
	}
	if _, ok := health.Components["vision_worker"]; !ok {
		item := health.Services["synora-discovery"]
		item.Name = "vision_worker"
		if item.Active {
			item.Status = "degraded"
			item.Message = "detailed capability status is reported by discovery"
		} else if item.Message == "health probe unavailable" {
			item.Message = "discovery capability status unavailable"
		}
		health.Components["vision_worker"] = item
	}
	if _, ok := health.Components["vision_ingress"]; !ok {
		item := health.Services["synora-discovery"]
		item.Name = "vision_ingress"
		if item.Active {
			item.Status = "degraded"
			item.Message = "TLS ingress status is reported by discovery"
		} else if item.Message == "health probe unavailable" {
			item.Message = "discovery ingress status unavailable"
		}
		health.Components["vision_ingress"] = item
	}
	if health.Network.HostAPD.Name == "" {
		health.Network.HostAPD = unavailableRuntimeService("hostapd", now, "network health unavailable")
	}
	if health.Network.DNSMasq.Name == "" {
		health.Network.DNSMasq = unavailableRuntimeService("dnsmasq", now, "network health unavailable")
	}
	if health.Network.Status == "" || health.Network.Status == "unknown" {
		health.Network.Status = combinedRuntimeStatus(health.Network.HostAPD, health.Network.DNSMasq)
	}
	if health.MediaMTX.Service.Name == "" {
		health.MediaMTX.Service = health.Services["mediamtx"]
		health.MediaMTX.Service.Name = "mediamtx"
	}
	if health.MediaMTX.Status == "" || health.MediaMTX.Status == "unknown" {
		health.MediaMTX.Status = health.MediaMTX.Service.Status
	}
	if health.Disk.Path == "" {
		health.Disk.Path = "/var/lib/synora"
	}
	if health.Disk.Status == "" {
		health.Disk.Status = "unavailable"
	}
	if health.Disk.TotalBytes > 0 && health.Disk.UsedBytes > 0 && health.Disk.UsedPercent == 0 {
		health.Disk.UsedPercent = int((health.Disk.UsedBytes * 100) / health.Disk.TotalBytes)
		if health.Disk.UsedPercent == 0 {
			health.Disk.UsedPercent = 1
		}
	}
	if health.Status == "" || health.Status == "unknown" {
		health.Status = "ok"
		for _, item := range health.Services {
			if !item.Active {
				health.Status = "degraded"
				break
			}
		}
		if health.Network.Status != "ok" || health.Disk.Status == "critical" {
			health.Status = "degraded"
		}
	}
	normalizeRuntimeStatusMessages(&health)
	return health
}

func normalizeRuntimeStatusMessages(health *RuntimeHealth) {
	if health == nil {
		return
	}
	clean := func(item *RuntimeServiceHealth) {
		if item == nil {
			return
		}
		if item.Status == "ok" || item.Status == "active" {
			item.Error = ""
		}
		if item.Status == "degraded" && strings.Contains(strings.ToLower(item.Error), "status unavailable") {
			item.Error = "runtime_component_degraded"
		}
	}
	for name, item := range health.Services {
		clean(&item)
		health.Services[name] = item
	}
	for name, item := range health.Components {
		clean(&item)
		health.Components[name] = item
	}
	clean(&health.Network.HostAPD)
	clean(&health.Network.DNSMasq)
	clean(&health.MediaMTX.Service)
}

// MergeRuntimeComponentStatus applies the component state observed by Core
// over generic service probes. A concrete runtime event is more informative
// than a missing health endpoint and must be reflected consistently in both
// services and components.
func MergeRuntimeComponentStatus(health RuntimeHealth, runtimeComponents map[string]string, now time.Time) RuntimeHealth {
	return MergeRuntimeComponentStatusDetailed(health, runtimeComponents, nil, nil, now)
}

// MergeRuntimeComponentStatusDetailed applies the runtime facts collected by
// Core over generic service probes. Details and model statuses are used only
// to make degraded messages actionable; the status itself remains the single
// source of truth for both the service and component views.
func MergeRuntimeComponentStatusDetailed(
	health RuntimeHealth,
	runtimeComponents map[string]string,
	runtimeComponentInfo map[string]string,
	runtimeModels map[string]string,
	now time.Time,
) RuntimeHealth {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	health = NormalizeRuntimeHealth(health, now)
	if len(runtimeComponents) == 0 {
		return health
	}
	for component, status := range runtimeComponents {
		if status == "" {
			continue
		}
		item := health.Components[component]
		item.Name = component
		item.Status = status
		item.Active = status != "unavailable" && status != "disabled" && status != "error"
		if item.Checked.IsZero() {
			item.Checked = now
		}
		item.Message = runtimeComponentMessage(component, status, item.Message, health, runtimeComponents, runtimeComponentInfo, runtimeModels)
		item.Error = runtimeComponentError(component, status, runtimeComponentInfo, runtimeModels)
		health.Components[component] = item
		switch component {
		case "discovery":
			service := health.Services["synora-discovery"]
			service.Name = "synora-discovery"
			service.Status = status
			service.Active = item.Active
			service.Checked = item.Checked
			service.Message = item.Message
			service.Error = item.Error
			health.Services["synora-discovery"] = service
		case "actions":
			service := health.Services["synora-actions"]
			service.Name = "synora-actions"
			service.Status = status
			service.Active = item.Active
			service.Checked = item.Checked
			service.Message = item.Message
			service.Error = item.Error
			health.Services["synora-actions"] = service
		}
	}
	if health.Network.Status == "ok" && health.Components["discovery"].Status == "degraded" {
		health.Status = "degraded"
	}
	return health
}

func runtimeComponentError(component, status string, info, models map[string]string) string {
	if status == "ok" || status == "active" {
		return ""
	}
	reason := strings.ToLower(strings.TrimSpace(info[component]))
	if status == "disabled" {
		if strings.Contains(reason, "tls_cert_missing") {
			return "tls_cert_missing"
		}
		if strings.Contains(reason, "tls_key_missing") {
			return "tls_key_missing"
		}
	}
	if status == "unavailable" && reason != "" {
		return reason
	}
	if component == "discovery" && status == "degraded" {
		if strings.Contains(strings.ToLower(info["discovery"]), "hostapd") {
			return "hostapd_failed"
		}
		for _, modelStatus := range models {
			if modelStatus == "missing" || modelStatus == "unavailable" || modelStatus == "disabled" {
				return "models_missing"
			}
		}
		return "discovery_degraded"
	}
	if component == "vision_worker" && status == "degraded" && strings.Contains(reason, "model") {
		return "models_missing"
	}
	if status == "degraded" {
		return component + "_degraded"
	}
	if status == "error" {
		return component + "_error"
	}
	if status == "unavailable" {
		return component + "_unavailable"
	}
	return ""
}

func runtimeComponentMessage(
	component string,
	status string,
	current string,
	health RuntimeHealth,
	runtimeComponents map[string]string,
	info map[string]string,
	models map[string]string,
) string {
	if component == "discovery" && status == "degraded" {
		reasons := discoveryDegradationReasons(health, runtimeComponents, info, models)
		if len(reasons) > 0 {
			return "running degraded: " + strings.Join(reasons, ", ")
		}
		return "running degraded: runtime component degraded"
	}
	if component == "vision_worker" && status == "degraded" && strings.Contains(strings.ToLower(info[component]), "model") {
		return "running with missing models"
	}
	if component == "vision_ingress" {
		reason := strings.ToLower(strings.TrimSpace(info[component]))
		if status == "disabled" && (strings.Contains(reason, "tls_cert_missing") || strings.Contains(reason, "tls_key_missing")) {
			return "disabled: tls_cert_missing"
		}
		if strings.Contains(reason, "listening insecure") {
			return "listening insecure local mode"
		}
		if status == "ok" {
			return "listening"
		}
	}
	if reason := strings.TrimSpace(info[component]); reason != "" {
		return reason
	}
	if component == "vision_ingress" {
		if status == "disabled" {
			return "disabled: tls_cert_missing"
		}
		if status == "ok" {
			return "listening"
		}
	}
	if current != "" && current != "last runtime component status" {
		return current
	}
	return "last runtime component status"
}

func discoveryDegradationReasons(
	health RuntimeHealth,
	components map[string]string,
	info map[string]string,
	models map[string]string,
) []string {
	reasons := []string{}
	add := func(reason string) {
		if reason == "" {
			return
		}
		for _, existing := range reasons {
			if existing == reason {
				return
			}
		}
		reasons = append(reasons, reason)
	}
	if health.Network.HostAPD.Status != "" && health.Network.HostAPD.Status != "active" && health.Network.HostAPD.Status != "ok" {
		message := strings.ToLower(health.Network.HostAPD.Message)
		if !(health.Network.HostAPD.Status == "unavailable" && (message == "" || message == "network health unavailable" || message == "health probe unavailable")) {
			add("hostapd failed")
		}
	}
	if components["network"] == "degraded" {
		add("network degraded")
	}
	if status := components["vision_worker"]; status == "unavailable" || status == "degraded" {
		if strings.Contains(strings.ToLower(info["vision_worker"]), "model") {
			add("vision models missing")
		} else {
			add("vision worker unavailable")
		}
	}
	if status := components["vision_ingress"]; status == "disabled" || status == "degraded" {
		reason := strings.ToLower(info["vision_ingress"])
		if strings.Contains(reason, "tls_cert_missing") || strings.Contains(reason, "tls_key_missing") {
			add("TLS cert missing for vision ingress")
		} else {
			add("vision ingress degraded")
		}
	}
	missingModels := 0
	for _, status := range models {
		if status == "missing" || status == "unavailable" || status == "disabled" {
			missingModels++
		}
	}
	if missingModels > 0 {
		add("vision models missing")
	}
	if reason := strings.ToLower(info["discovery"]); strings.Contains(reason, "hostapd") {
		add("hostapd failed")
	}
	return reasons
}

func componentRuntimeStatus(status string, active bool) string {
	switch status {
	case "active", "ok":
		if active || status == "ok" {
			return "ok"
		}
	case "inactive", "failed", "unknown", "":
		return "degraded"
	}
	return status
}

func unavailableRuntimeService(name string, checked time.Time, message string) RuntimeServiceHealth {
	return RuntimeServiceHealth{Name: name, Status: "unavailable", Active: false, Checked: checked, Error: message, Message: message}
}

func missingServiceMessage(name string) string {
	switch name {
	case "mediamtx":
		return "optional component not running"
	case "synora-actions":
		return "action service status unavailable"
	case "synora-discovery":
		return "discovery status unavailable"
	case "synora-bus":
		return "bus status unavailable"
	default:
		return "service status unavailable"
	}
}

func combinedRuntimeStatus(items ...RuntimeServiceHealth) string {
	for _, item := range items {
		if item.Active && item.Status == "active" {
			continue
		}
		return "degraded"
	}
	return "ok"
}
