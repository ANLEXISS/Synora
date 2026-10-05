package cognitivecore

import (
	"context"
	"errors"
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
	snapshot = snapshot.Normalized()
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
