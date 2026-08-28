package vision

import (
	"encoding/json"
	"errors"
	"fmt"
)

const (
	VisionProtocolVersion     = "synora.vision.v1"
	VisionProtocolHello       = "protocol.hello"
	VisionClipProcess         = "clip.process"
	ArcFaceEmbeddingDimension = 512
)

var (
	ErrVisionProtocolMismatch  = errors.New("vision worker protocol mismatch")
	ErrVisionWorkerDegraded    = errors.New("vision worker degraded")
	ErrVisionMalformedResponse = errors.New("malformed vision worker response")
)

type ProtocolHelloRequest struct {
	RequestID       string `json:"request_id"`
	Operation       string `json:"operation"`
	ProtocolVersion string `json:"protocol_version"`
}

// ProtocolHelloResponse is deliberately internal: it describes the local Go
// to Python runtime boundary and is not a public business contract.
type ProtocolHelloResponse struct {
	RequestID          string                    `json:"request_id"`
	Operation          string                    `json:"operation"`
	ProtocolVersion    string                    `json:"protocol_version"`
	Status             string                    `json:"status"`
	Backend            string                    `json:"backend"`
	EmbeddingDimension int                       `json:"embedding_dimension"`
	Models             map[string]map[string]any `json:"models"`
	Capabilities       map[string]map[string]any `json:"capabilities"`
	FaceDataset        map[string]any            `json:"face_dataset"`
	Error              string                    `json:"error,omitempty"`
	FailureCode        string                    `json:"failure_code,omitempty"`
}

func (r ProtocolHelloResponse) degradedReason() string {
	if r.Error != "" {
		if r.FailureCode != "" {
			return fmt.Sprintf("code=%s: %s", r.FailureCode, r.Error)
		}
		return r.Error
	}
	if r.Status == "" || r.Status == "degraded" {
		return "worker capabilities are degraded"
	}
	return ""
}

func validateProtocolHello(requestID string, response ProtocolHelloResponse) error {
	if response.RequestID != requestID || response.Operation != VisionProtocolHello {
		return fmt.Errorf("%w: hello correlation mismatch", ErrVisionProtocolMismatch)
	}
	if response.ProtocolVersion != VisionProtocolVersion {
		return fmt.Errorf("%w: got %q want %q", ErrVisionProtocolMismatch, response.ProtocolVersion, VisionProtocolVersion)
	}
	if response.Status != "normal" && response.Status != "degraded" {
		return fmt.Errorf("%w: invalid worker status %q", ErrVisionProtocolMismatch, response.Status)
	}
	if response.Backend == "" || response.Models == nil || response.Capabilities == nil || response.FaceDataset == nil {
		return fmt.Errorf("%w: incomplete worker capabilities", ErrVisionProtocolMismatch)
	}
	if response.EmbeddingDimension != ArcFaceEmbeddingDimension {
		return fmt.Errorf("%w: ArcFace dimension=%d want=%d", ErrVisionWorkerDegraded, response.EmbeddingDimension, ArcFaceEmbeddingDimension)
	}
	return nil
}

func decodeWorkerResponse(raw json.RawMessage) (WorkerResponse, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return WorkerResponse{}, fmt.Errorf("%w: invalid JSON: %v", ErrVisionMalformedResponse, err)
	}
	if fields == nil {
		return WorkerResponse{}, fmt.Errorf("%w: response must be an object", ErrVisionMalformedResponse)
	}
	requestIDRaw, ok := fields["request_id"]
	if !ok {
		return WorkerResponse{}, fmt.Errorf("%w: missing request_id", ErrVisionMalformedResponse)
	}
	var requestID string
	if err := json.Unmarshal(requestIDRaw, &requestID); err != nil || requestID == "" {
		return WorkerResponse{}, fmt.Errorf("%w: invalid request_id", ErrVisionMalformedResponse)
	}
	if errorRaw, ok := fields["error"]; ok {
		var message string
		if err := json.Unmarshal(errorRaw, &message); err != nil {
			return WorkerResponse{}, fmt.Errorf("%w: invalid error", ErrVisionMalformedResponse)
		}
		var response WorkerResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			return WorkerResponse{}, fmt.Errorf("%w: invalid error response", ErrVisionMalformedResponse)
		}
		return response, nil
	}
	eventsRaw, ok := fields["events"]
	if !ok || string(eventsRaw) == "null" || len(eventsRaw) == 0 || eventsRaw[0] != '[' {
		return WorkerResponse{}, fmt.Errorf("%w: events array is required", ErrVisionMalformedResponse)
	}
	var response WorkerResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return WorkerResponse{}, fmt.Errorf("%w: invalid event response: %v", ErrVisionMalformedResponse, err)
	}
	return response, nil
}
