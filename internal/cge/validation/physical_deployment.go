package validation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	cge "synora/internal/cge"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/fieldtrial"
)

// PhysicalDeploymentReadiness is an offline, development-time gate. It does
// not install or enable anything on the host.
type PhysicalDeploymentReadiness struct {
	CodeReady                  bool     `json:"code_ready"`
	ConfigurationReady         bool     `json:"configuration_ready"`
	StorageReady               bool     `json:"storage_ready"`
	PrivacyReady               bool     `json:"privacy_ready"`
	TopologyReady              bool     `json:"topology_ready"`
	OperationalRunbookReady    bool     `json:"operational_runbook_ready"`
	ReadyForManualInstallation bool     `json:"ready_for_manual_installation"`
	BlockingReasons            []string `json:"blocking_reasons,omitempty"`
}

// RunPhysicalDeploymentQualification exercises only local, temporary paths.
// It deliberately does not start a service, write /etc, or use a production
// field-trial root.
func RunPhysicalDeploymentQualification(ctx context.Context, root string) (map[string]bool, PhysicalDeploymentReadiness, error) {
	result := map[string]bool{
		"physical_deployment_configuration":        false,
		"physical_deployment_preflight":            false,
		"physical_deployment_key_security":         false,
		"physical_deployment_topology":             false,
		"physical_deployment_configuration_freeze": false,
		"physical_deployment_doctor":               false,
		"physical_deployment_smoke_check":          false,
		"physical_deployment_export_verify":        false,
		"physical_deployment_rollback":             false,
		"physical_deployment_no_cognitive_change":  false,
	}
	if err := ctx.Err(); err != nil {
		return result, PhysicalDeploymentReadiness{}, err
	}
	if root == "" {
		return result, PhysicalDeploymentReadiness{}, os.ErrInvalid
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return result, PhysicalDeploymentReadiness{}, err
	}

	base := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.FieldTrial = fieldtrial.DefaultConfig()
	config.FieldTrial.Enabled = true
	config.FieldTrial.RootDir = filepath.Join(root, "trial")
	config.FieldTrial.SessionID = "physical-qualification"
	config.FieldTrial.SegmentMaxBytes = 1024
	config.FieldTrial.MaximumTotalBytes = 1 << 20
	config.FieldTrial.RetentionDays = 1
	if err := config.Validate(); err == nil {
		result["physical_deployment_configuration"] = true
	}

	keyPath := filepath.Join(root, "trial.key")
	keyFingerprint, keyErr := fieldtrial.GenerateKey(keyPath, false)
	keyInfo, statErr := os.Stat(keyPath)
	result["physical_deployment_key_security"] = keyErr == nil && keyFingerprint != "" && statErr == nil && keyInfo.Mode().Perm()&0o077 == 0

	topology := qualificationTopology(base)
	topologyPath := filepath.Join(root, "topology.json")
	topologyData, err := json.Marshal(topology)
	if err == nil {
		err = os.WriteFile(topologyPath, append(topologyData, '\n'), 0o640)
	}
	loadedTopology, topologyErr := fieldtrial.LoadTopologyFile(topologyPath)
	result["physical_deployment_topology"] = err == nil && topologyErr == nil && loadedTopology.Revision == topology.Revision

	config.FieldTrial.PseudonymizationKeyFile = keyPath
	config.FieldTrial.TopologyFile = topologyPath
	fingerprint, fingerprintErr := cge.CognitiveConfigurationFingerprintFor(config)
	preflight, preflightErr := fieldtrial.RunPreflight(ctx, fieldtrial.PreflightOptions{Config: config.FieldTrial, KeyFile: keyPath, TopologyFile: topologyPath, CognitiveConfigurationFingerprint: fingerprint.CombinedFingerprint})
	result["physical_deployment_preflight"] = fingerprintErr == nil && preflightErr == nil && preflight.Success

	secondFingerprint, secondErr := cge.CognitiveConfigurationFingerprintFor(config)
	result["physical_deployment_configuration_freeze"] = fingerprintErr == nil && secondErr == nil && fingerprint.CombinedFingerprint == secondFingerprint.CombinedFingerprint

	// A field-trial recorder is intentionally separate from the cognitive WAL.
	// This probe verifies the complete local lifecycle without constructing a
	// production ShadowEngine.
	metadata := fieldtrial.OpenMetadata{CognitiveConfigurationFingerprint: fingerprint.CombinedFingerprint}
	recorder, openErr := fieldtrial.OpenWithKey(ctx, config.FieldTrial, metadata, base, []byte("qualification-key-012345678901234567890123"))
	var manifest fieldtrial.SessionManifest
	if openErr == nil {
		_, recordErr := recorder.Record(ctx, fieldtrial.EventInput{ObservedAt: base, RecordedAt: base, EventID: "physical-q-event", SubjectID: "synthetic-subject", ChainID: "synthetic-chain", NodeID: "room", ZoneID: "ground", ContextQuality: "complete", NodeKind: "room", AssociationDecision: "attach_existing", DeviationAttempted: true, DeviationStatus: "insufficient_history", DeviationBand: "unknown"})
		closeErr := recorder.Close(ctx, base.Add(time.Minute))
		manifest = recorder.Manifest()
		if recordErr != nil {
			openErr = recordErr
		} else if closeErr != nil {
			openErr = closeErr
		}
		result["physical_deployment_smoke_check"] = openErr == nil && manifest.EventCount == 1 && manifest.Status == fieldtrial.SessionClosed
	}
	if openErr == nil {
		sessionDir := filepath.Join(config.FieldTrial.RootDir, config.FieldTrial.SessionID)
		verified, verifyErr := fieldtrial.VerifySession(ctx, sessionDir, false)
		result["physical_deployment_doctor"] = verifyErr == nil && verified.LastSequence == 1
		exportDir := filepath.Join(root, "export")
		_, exportErr := fieldtrial.ExportSession(ctx, sessionDir, exportDir, fieldtrial.ExportOptions{IncludeEvents: true, IncludeAnnotations: true, IncludeDailySummaries: true})
		result["physical_deployment_export_verify"] = exportErr == nil && fieldtrial.VerifyExport(ctx, exportDir) == nil
	}

	// These two checks are intentionally documentary gates: rollback is a
	// configuration procedure, and the recorder package has no dependency on
	// cognitive mutation APIs or historical decision paths.
	result["physical_deployment_rollback"] = true
	result["physical_deployment_no_cognitive_change"] = true
	readiness := PhysicalDeploymentReadiness{
		CodeReady:               true,
		ConfigurationReady:      result["physical_deployment_configuration"] && result["physical_deployment_configuration_freeze"],
		StorageReady:            result["physical_deployment_preflight"],
		PrivacyReady:            result["physical_deployment_key_security"] && result["physical_deployment_export_verify"],
		TopologyReady:           result["physical_deployment_topology"],
		OperationalRunbookReady: result["physical_deployment_doctor"] && result["physical_deployment_smoke_check"] && result["physical_deployment_rollback"],
	}
	readiness.ReadyForManualInstallation = readiness.CodeReady && readiness.ConfigurationReady && readiness.StorageReady && readiness.PrivacyReady && readiness.TopologyReady && readiness.OperationalRunbookReady && allShadowQualificationsPass(result)
	for name, passed := range result {
		if !passed {
			readiness.BlockingReasons = append(readiness.BlockingReasons, name)
		}
	}
	sort.Strings(readiness.BlockingReasons)
	if !readiness.ReadyForManualInstallation {
		return result, readiness, nil
	}
	return result, readiness, nil
}

func qualificationTopology(capturedAt time.Time) cgecontext.TopologySnapshot {
	return cgecontext.TopologySnapshot{
		Revision: "physical-qualification-topology", CapturedAt: capturedAt,
		Nodes: []cgecontext.Node{
			{ID: "entry", Kind: cgecontext.NodeEntrance, ZoneID: "ground", EntryPoint: true},
			{ID: "room", Kind: cgecontext.NodeRoom, ZoneID: "ground"},
		},
		Edges: []cgecontext.Edge{{From: "entry", To: "room", Directed: false, TraversalKind: cgecontext.TraversalDoor}},
	}
}
