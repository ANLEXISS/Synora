package validation

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	"synora/internal/cge/campaign"
)

// runCampaignQualification exercises the development campaign harness. It
// deliberately treats calibration findings as observations, not failures.
func runCampaignQualification(ctx context.Context, root string, full bool) (map[string]bool, error) {
	result := map[string]bool{
		"campaign_profiles":              false,
		"campaign_determinism":           false,
		"campaign_warmup":                false,
		"campaign_benign_variations":     false,
		"campaign_routine_shift":         false,
		"campaign_synthetic_episodes":    false,
		"campaign_partial_context":       false,
		"campaign_restarts":              false,
		"campaign_checkpoints":           false,
		"campaign_growth":                false,
		"campaign_latency":               false,
		"campaign_no_security_authority": false,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	profiles := campaign.DefaultProfiles()
	reports := make(map[string]campaign.Report, len(profiles))
	days := 7
	if full {
		days = 0
	}
	allSuccessful := true
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return result, err
		}
		runReport, err := campaign.Run(ctx, profile, campaign.RunOptions{RootDir: filepath.Join(root, profile.ID), Full: full, DaysOverride: days})
		if err != nil || !runReport.Success {
			allSuccessful = false
		}
		reports[profile.ID] = runReport
	}
	result["campaign_profiles"] = allSuccessful && len(reports) == len(profiles)
	stable, stableOK := reports["stable_single_resident_30d"]
	shift, shiftOK := reports["routine_shift_45d"]
	benign, benignOK := reports["benign_irregularity_30d"]
	synthetic, syntheticOK := reports["synthetic_intrusion_30d"]
	degraded, degradedOK := reports["degraded_sensors_30d"]
	restart, restartOK := reports["restart_stress_14d"]
	result["campaign_warmup"] = stableOK && stable.Warmup.FirstEvaluatedAt != nil && stable.Warmup.InsufficientHistoryCount > 0
	result["campaign_benign_variations"] = benignOK && len(benign.Labels) > 0
	result["campaign_routine_shift"] = shiftOK && shift.Adaptation != nil
	result["campaign_synthetic_episodes"] = syntheticOK && len(synthetic.Labels) > 0
	result["campaign_partial_context"] = degradedOK && len(degraded.Labels) > 0
	result["campaign_restarts"] = restartOK && restart.RestartCount > 0 && restart.IdempotenceChecks > 0 && restart.IdempotenceFailures == 0
	result["campaign_checkpoints"] = stableOK && stable.CheckpointCount > 0 && restartOK && restart.CheckpointCount > 0
	result["campaign_growth"] = stableOK && len(stable.Growth) > 0 && stable.Growth[len(stable.Growth)-1].JournalRecords > 0
	result["campaign_latency"] = stableOK && stable.Latency.Total.Count == stable.EventCount
	result["campaign_no_security_authority"] = allSuccessful && allReportsRemainDescriptive(reports)

	deterministicProfile, ok := campaign.ProfileByID("stable_single_resident_30d")
	if ok {
		deterministicProfile.DurationDays = 7
		first, firstErr := campaign.GenerateTimeline(deterministicProfile)
		second, secondErr := campaign.GenerateTimeline(deterministicProfile)
		result["campaign_determinism"] = firstErr == nil && secondErr == nil && reflect.DeepEqual(first, second)
	}
	if !allShadowQualificationsPass(result) {
		return result, fmt.Errorf("one or more campaign qualification probes failed")
	}
	return result, nil
}

func allReportsRemainDescriptive(reports map[string]campaign.Report) bool {
	for _, report := range reports {
		if len(report.InvariantFailures) != 0 || report.EventsFailed != 0 {
			return false
		}
		for _, event := range report.Events {
			if event.ErrorCode == "security_action" || event.ErrorCode == "historical_mutation" {
				return false
			}
		}
	}
	return true
}
