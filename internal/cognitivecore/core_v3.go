package cognitivecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"synora/pkg/contract"
)

type ProcessResultV3 struct {
	Commit  Commit
	Result  CommitResult
	Encoded EncodedSnapshotV3
}

// CoreV3 is the sole candidate cognitive path. It accepts only the aggregate
// V3 snapshot, persists through UniversalStore, and remains active_dry_run.
type CoreV3 struct {
	Store        *UniversalStore
	Encoder      SnapshotEncoderV3
	MLP          MLPBackendV3
	Now          func() time.Time
	ActiveDryRun bool
}

func (c *CoreV3) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *CoreV3) Validate() error {
	if c == nil || c.Store == nil {
		return errors.New("V3 cognitive core store unavailable")
	}
	if !c.ActiveDryRun {
		return errors.New("V3 requires active_dry_run")
	}
	if c.MLP == nil {
		return errors.New("V3 MLP is unavailable")
	}
	return c.Store.ValidateBounds()
}

func (c *CoreV3) Process(ctx context.Context, event contract.Event, snapshot CognitiveSnapshotV3) (ProcessResultV3, error) {
	if err := c.Validate(); err != nil {
		return ProcessResultV3{}, err
	}
	if event.ID == "" || event.Type == "" || event.Source == "" {
		return ProcessResultV3{}, errors.New("V3 event id, type and source are required")
	}
	if event.Type != contract.EventVisionEvidenceV1 {
		return ProcessResultV3{}, fmt.Errorf("Core V3 rejects non-Evidence Vision event type %q", event.Type)
	}
	snapshot = snapshot.Normalized()
	if snapshot.VisionEvidence != nil {
		mapped, mapErr := snapshot.ApplyVisionEvidenceV1(*snapshot.VisionEvidence)
		if mapErr != nil {
			return ProcessResultV3{}, mapErr
		}
		snapshot = mapped.Normalized()
	}
	if snapshot.CapturedAt.IsZero() || snapshot.CapturedAt.Equal(time.Unix(0, 0).UTC()) {
		snapshot.CapturedAt = event.Timestamp.UTC()
		if snapshot.CapturedAt.IsZero() {
			snapshot.CapturedAt = c.now()
		}
	}
	if err := snapshot.Validate(); err != nil {
		return ProcessResultV3{}, err
	}
	encoded, err := c.Encoder.Encode(ctx, snapshot)
	if err != nil {
		return ProcessResultV3{}, err
	}
	output, latencies, err := c.MLP.RunV3(ctx, encoded, snapshot)
	if err != nil {
		return ProcessResultV3{}, err
	}
	decision := BuildDecisionV3(snapshot, output, latencies, c.now())
	var action *ActionRequest
	if decision.Action.Status == "allowed_dry_run" && decision.Action.Proposed.Action != "no_action" {
		action = &ActionRequest{SchemaVersion: "action-request/v1", RequestID: event.ID, EpisodeID: episodeID(event), Action: decision.Action.Proposed, DryRun: true}
	}
	commit := Commit{
		Event: event, SnapshotV3: &snapshot, DecisionV3: &decision,
		Snapshot: projectV3ToV1(snapshot),
		Decision: Decision{SchemaVersion: DecisionSchemaVersion, Status: "available", Mode: "active_dry_run", Source: "mlp-v3-candidate", HeadOrder: append([]string(nil), HeadOrder[:]...), InputDimension: CognitiveVectorSize, Action: ActionAssessment{Proposed: ActionIntent{Action: "no_action"}, Status: "not_requested", PhysicalActionExecuted: false}, GeneratedAt: c.now()},
		Action:   action, CommittedAt: c.now(),
	}
	result, err := c.Store.Commit(commit)
	if err != nil {
		return ProcessResultV3{}, err
	}
	commit.Snapshot.Revision = result.Revision
	snapshot.Revision = result.Revision
	commit.SnapshotV3 = &snapshot
	return ProcessResultV3{Commit: commit, Result: result, Encoded: encoded}, nil
}

// RecordActionResult folds a Discovery action result into the durable V1 view
// of the Store. It deliberately does not encode or run the V3 MLP again: an
// action result is a fact, not a new Vision trigger. Request correlation and
// duplicate handling are checked against the committed journal.
func (c *CoreV3) RecordActionResult(event contract.Event) (CommitResult, error) {
	if c == nil || c.Store == nil {
		return CommitResult{}, errors.New("V3 cognitive core store unavailable")
	}
	if event.ID == "" || event.Type == "" || event.Source == "" {
		return CommitResult{}, errors.New("action result event id, type and source are required")
	}
	requestID := actionResultRequestID(event.Payload)
	if requestID == "" {
		return CommitResult{}, errors.New("action result request_id is required")
	}
	requestFound, duplicate, err := actionResultState(c.Store, requestID)
	if err != nil {
		return CommitResult{}, err
	}
	if duplicate {
		return CommitResult{Revision: c.Store.Revision(), Duplicate: true}, nil
	}
	if !requestFound {
		return CommitResult{}, fmt.Errorf("orphan action result rejected: request_id=%s", requestID)
	}
	snapshot := c.Store.Snapshot()
	snapshot.ActionResults = append(snapshot.ActionResults, actionResultFromPayload(event.Payload))
	commit := Commit{
		Event:    event,
		Snapshot: snapshot,
		Decision: Decision{
			SchemaVersion: DecisionSchemaVersion, Status: "not_requested", Mode: "active_dry_run", Source: "discovery-action-result",
			HeadOrder: append([]string(nil), HeadOrder[:]...), InputDimension: CognitiveVectorSize,
			Action:      ActionAssessment{Proposed: ActionIntent{Action: "no_action"}, Status: "not_requested", Reasons: []string{"action_result_observed"}, PhysicalActionExecuted: false},
			GeneratedAt: c.now(),
		},
		CommittedAt: c.now(),
	}
	return c.Store.Commit(commit)
}

func actionResultState(store *UniversalStore, requestID string) (bool, bool, error) {
	if store == nil {
		return false, false, errors.New("universal store is nil")
	}
	history, err := store.History()
	if err != nil {
		return false, false, err
	}
	requestFound := false
	for _, commit := range history {
		if commit.Action != nil && commit.Action.RequestID == requestID {
			requestFound = true
		}
		if commit.Event.Type == contract.EventActionResult || commit.Event.Type == "discovery.action.result" {
			if actionResultRequestID(commit.Event.Payload) == requestID {
				return requestFound, true, nil
			}
		}
	}
	return requestFound, false, nil
}

func actionResultRequestID(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload["request_id"].(string); ok {
		return value
	}
	if request, ok := payload["request"].(map[string]any); ok {
		if value, ok := request["request_id"].(string); ok {
			return value
		}
	}
	return ""
}

// ActionResultRequestID is a redacted helper for the central harness and
// contract tests; it never exposes an action payload or device identifier.
func ActionResultRequestID(payload map[string]any) string { return actionResultRequestID(payload) }

// ValidateActionResultJSON keeps the V3 service boundary explicit for action
// results that arrived through Discovery.
func ValidateActionResultJSON(payload []byte) (string, error) {
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", err
	}
	requestID := actionResultRequestID(value)
	if requestID == "" {
		return "", errors.New("action result request_id is required")
	}
	return requestID, nil
}
