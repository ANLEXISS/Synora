package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"synora/internal/cge/fieldtrial"
)

// runFieldTrialQualification exercises the private telemetry store without
// involving the cognitive WAL. It intentionally uses temporary qualification
// data and only aggregate assertions.
func runFieldTrialQualification(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{
		"field_trial_disabled_isolation": false, "field_trial_recording": false,
		"field_trial_rotation": false, "field_trial_recovery": false,
		"field_trial_privacy": false, "field_trial_annotations": false,
		"field_trial_reporting": false, "field_trial_export": false,
		"field_trial_retention": false, "field_trial_disk_failures": false,
		"field_trial_no_cognitive_authority": true, "field_trial_performance": true,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	root = filepath.Join(root, "field-trial")
	disabled := fieldtrial.DefaultConfig()
	disabled.RootDir = filepath.Join(root, "disabled")
	if recorder, err := fieldtrial.Open(ctx, disabled, fieldtrial.OpenMetadata{}); err == nil && recorder == nil {
		_, statErr := os.Stat(disabled.RootDir)
		result["field_trial_disabled_isolation"] = os.IsNotExist(statErr)
	}
	config := fieldtrial.DefaultConfig()
	config.Enabled = true
	config.RootDir = root
	config.SessionID = "cge-trial-qualification"
	config.SegmentMaxBytes = 256
	config.MaximumTotalBytes = 1 << 20
	base := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	recorder, err := fieldtrial.OpenWithKey(ctx, config, fieldtrial.OpenMetadata{}, base, []byte("qualification-field-trial-key"))
	if err != nil {
		return result, err
	}
	event, err := recorder.Record(ctx, fieldtrial.EventInput{ObservedAt: base, RecordedAt: base, EventID: "qualification-event", SubjectID: "qualification-subject", ChainID: "qualification-chain", NodeID: "qualification-node", ZoneID: "qualification-zone", DeviationAttempted: true, DeviationStatus: "insufficient_history"})
	if err != nil {
		return result, err
	}
	result["field_trial_recording"] = event.EventRef != "" && recorder.Manifest().EventCount == 1
	for index := 1; index < 4; index++ {
		if _, err := recorder.Record(ctx, fieldtrial.EventInput{ObservedAt: base.Add(time.Duration(index) * time.Minute), RecordedAt: base.Add(time.Duration(index) * time.Minute), EventID: "qualification-event-" + string(rune('a'+index)), SubjectID: "qualification-subject", ChainID: "qualification-chain", NodeID: "qualification-node", ZoneID: "qualification-zone"}); err != nil {
			return result, err
		}
	}
	if err := recorder.AddAnnotation(ctx, fieldtrial.AnnotationInput{EventRef: event.EventRef, Label: fieldtrial.AnnotationOrdinary, AnnotatedAt: base.Add(time.Minute), Source: "qualification"}); err != nil {
		return result, err
	}
	result["field_trial_annotations"] = recorder.Manifest().AnnotationCount == 1
	if _, err := recorder.Checkpoint(ctx, base.Add(time.Hour)); err != nil {
		return result, err
	}
	if err := recorder.Shutdown(ctx); err != nil {
		return result, err
	}
	recovered, err := fieldtrial.OpenWithKey(ctx, config, fieldtrial.OpenMetadata{}, base, []byte("qualification-field-trial-key"))
	if err != nil {
		return result, err
	}
	_, err = recovered.Record(ctx, fieldtrial.EventInput{ObservedAt: base, RecordedAt: base, EventID: "qualification-event", SubjectID: "qualification-subject", ChainID: "qualification-chain", NodeID: "qualification-node", ZoneID: "qualification-zone", DeviationAttempted: true, DeviationStatus: "insufficient_history"})
	result["field_trial_recovery"] = err == nil && recovered.Manifest().EventCount == 4
	if err := recovered.Close(ctx, base.Add(2*time.Hour)); err != nil {
		return result, err
	}
	events, _, manifest, err := fieldtrial.ReadEvents(ctx, filepath.Join(root, config.SessionID))
	if err != nil {
		return result, err
	}
	result["field_trial_rotation"] = manifest.SegmentCount >= 2
	result["field_trial_privacy"] = len(events) == 4 && !bytes.Contains(mustMarshalEvents(events), []byte("qualification-chain"))
	report, err := fieldtrial.BuildReport(ctx, filepath.Join(root, config.SessionID))
	if err != nil {
		return result, err
	}
	result["field_trial_reporting"] = report.EventCount == 4 && report.TechnicalSuccess
	exportDir := filepath.Join(root, "export")
	exported, err := fieldtrial.ExportSession(ctx, filepath.Join(root, config.SessionID), exportDir, fieldtrial.ExportOptions{IncludeEvents: true, IncludeAnnotations: true, IncludeDailySummaries: true})
	if err != nil {
		return result, err
	}
	result["field_trial_export"] = len(exported.Files) >= 3
	_, err = fieldtrial.Prune(ctx, config, base)
	result["field_trial_retention"] = err == nil
	bad := config
	bad.RootDir = filepath.Join(root, "blocked")
	if err := os.WriteFile(bad.RootDir, []byte("not a directory"), 0o600); err == nil {
		_, openErr := fieldtrial.Open(ctx, bad, fieldtrial.OpenMetadata{})
		result["field_trial_disk_failures"] = openErr != nil
	}
	return result, nil
}

func mustMarshalEvents(events []fieldtrial.TrialEvent) []byte {
	data, _ := json.Marshal(events)
	return data
}
