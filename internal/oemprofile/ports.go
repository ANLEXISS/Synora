// Package oemprofile exposes narrow adapter ports. Implementations are
// explicitly linked and selected by validated profile; results contain only
// redacted aggregate metadata and never provide a path around Discovery,
// Evidence V1, Core, Store, or Safety Gate.
package oemprofile

import (
	"context"
	"math"
	"regexp"
)

var opaqueRef = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Capability struct {
	Name          string `json:"name"`
	Availability  string `json:"availability"`
	Health        string `json:"health"`
	Version       string `json:"version"`
	Compatibility string `json:"compatibility"`
}
type RedactedResult struct {
	Status            string             `json:"status"`
	ErrorCategory     string             `json:"error_category,omitempty"`
	AggregatedMetrics map[string]float64 `json:"aggregated_metrics,omitempty"`
}
type Adapter interface{ Capability() Capability }

// CameraInput may deliver an opaque clip reference to Discovery only. It must
// not publish media, paths, hardware identifiers, or write to Core/Store.
type IngressMetadata struct {
	ContractVersion string
	InputKind       string
	CorrelationRef  string
}
type VisionRequest struct {
	ContractVersion  string
	Module           string
	CorrelationRef   string
	RequestedSignals []string
}
type CameraInput interface {
	Adapter
	Submit(context.Context, IngressMetadata) (RedactedResult, error)
}

// VisionWorker returns aggregate functional evidence. Discovery validates and
// converts it to Evidence V1 before any Core boundary.
type VisionWorker interface {
	Adapter
	Evaluate(context.Context, VisionRequest) (RedactedResult, error)
}

// ComputeRuntime encapsulates the selected numerical backend behind a module.
type ComputeRuntime interface {
	Adapter
	Run(context.Context, AggregateFeatureInput) (RedactedResult, error)
}

type AggregateFeatureInput struct {
	ContractVersion string             `json:"contract_version"`
	Features        map[string]float64 `json:"features"`
}

// DeviceDriver emits allowlisted abstract proposals only; Discovery and
// Safety Gate remain owners. No target or hardware identifier is accepted.
type DeviceDriver interface {
	Adapter
	Propose(context.Context, DeviceRequest) (RedactedResult, error)
}

type DeviceRequest struct {
	ActionClass string `json:"action_class"`
	RequestRef  string `json:"request_ref"`
}

// ActionExecutor is a dry-run boundary unless separately qualified and
// authorized by a future product version; V1 profiles require active_dry_run.
type ActionExecutor interface {
	Adapter
	Execute(context.Context, ActionProposal) (RedactedResult, error)
}

type ActionProposal struct {
	ActionClass    string `json:"action_class"`
	Mode           string `json:"mode"`
	CorrelationRef string `json:"correlation_ref"`
}
type CommunicationOutput interface {
	Adapter
	Deliver(context.Context, CommunicationRequest) (RedactedResult, error)
}

type CommunicationRequest struct {
	ContentClass string `json:"content_class"`
	OutputMode   string `json:"output_mode"`
}

// ValidateRedactedMetric rejects sensitive field names at adapter boundaries.
func ValidateRedactedMetric(name string) bool {
	switch name {
	case "aggregate_latency_ms", "aggregate_confidence", "availability_ratio", "error_count", "queue_depth", "observation_count", "uptime_ratio", "worker_count", "store_revision_delta":
		return true
	}
	return false
}

func ValidateAggregateFeatureInput(in AggregateFeatureInput) bool {
	if in.ContractVersion != "synora.aggregate-features/v1" || len(in.Features) == 0 {
		return false
	}
	for key, value := range in.Features {
		if !ValidateRedactedMetric(key) || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return false
		}
	}
	return true
}

func ValidateIngressMetadata(in IngressMetadata) bool {
	return in.ContractVersion == "synora.camera.input/v1" && (in.InputKind == "v4l2" || in.InputKind == "rtsp" || in.InputKind == "mock" || in.InputKind == "replay") && opaqueRef.MatchString(in.CorrelationRef)
}

func ValidateVisionRequest(in VisionRequest) bool {
	if in.ContractVersion != "synora.vision.request/v1" || !knownModule(in.Module) || !opaqueRef.MatchString(in.CorrelationRef) || len(in.RequestedSignals) == 0 {
		return false
	}
	allowed := map[string]bool{"person.presence": true, "pose.posture": true, "face.presence": true, "camera.health": true, "vehicle.presence": true, "animal.presence": true, "plate.readability": true, "motion.tier": true, "communication.status": true}
	for _, signal := range in.RequestedSignals {
		if !allowed[signal] {
			return false
		}
	}
	return true
}

func ValidateRedactedResult(result RedactedResult) bool {
	switch result.Status {
	case "available", "unavailable", "degraded", "blocked", "completed", "suppressed", "failed":
	default:
		return false
	}
	for name, value := range result.AggregatedMetrics {
		if !ValidateRedactedMetric(name) || math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
		if name == "aggregate_confidence" || name == "availability_ratio" || name == "uptime_ratio" {
			if value < 0 || value > 1 {
				return false
			}
		}
		if name == "aggregate_latency_ms" || name == "queue_depth" || name == "error_count" || name == "observation_count" || name == "worker_count" || name == "store_revision_delta" {
			if value < 0 {
				return false
			}
		}
	}
	if result.ErrorCategory != "" && !opaqueRef.MatchString(result.ErrorCategory) {
		return false
	}
	return true
}

func ValidateDryRunDeviceRequest(in DeviceRequest) bool {
	return opaqueRef.MatchString(in.RequestRef) && (in.ActionClass == "notify_abstract" || in.ActionClass == "record_event" || in.ActionClass == "device_health_check")
}

func ValidateActionProposal(in ActionProposal) bool {
	return in.Mode == "active_dry_run" && opaqueRef.MatchString(in.CorrelationRef) && (in.ActionClass == "notify_abstract" || in.ActionClass == "record_event" || in.ActionClass == "device_health_check")
}

func ValidateCommunicationRequest(in CommunicationRequest) bool {
	return in.OutputMode == "dry_run" && (in.ContentClass == "neutral" || in.ContentClass == "safety_notice" || in.ContentClass == "status")
}
