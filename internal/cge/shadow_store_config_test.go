package cge

import (
	"errors"
	"path/filepath"
	"testing"

	"synora/internal/cge/shadowworkflow"
)

func TestLoadShadowConfigLoadsDurableWorkflowStoreFromEnvironment(t *testing.T) {
	root := t.TempDir()
	values := map[string]string{
		ShadowEnabledEnv:                               "true",
		ShadowWorkflowEnabledEnv:                       "true",
		shadowworkflow.ShadowWorkflowStoreModeEnv:      "file",
		shadowworkflow.ShadowWorkflowStoreDirectoryEnv: filepath.Join(root, "workflow"),
	}
	config, err := LoadShadowConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if config.Workflow.StoreMode != shadowworkflow.StoreFile || config.Workflow.StoreDirectory != values[shadowworkflow.ShadowWorkflowStoreDirectoryEnv] {
		t.Fatalf("workflow store environment not loaded: %+v", config.Workflow)
	}
	if !config.Workflow.SyncOnCommit {
		t.Fatal("workflow store lost SyncOnCommit=true")
	}
}

func TestLoadShadowConfigRejectsInvalidWorkflowStoreWithoutStartingIt(t *testing.T) {
	values := map[string]string{
		ShadowEnabledEnv:                               "true",
		ShadowWorkflowEnabledEnv:                       "true",
		shadowworkflow.ShadowWorkflowStoreModeEnv:      "file",
		shadowworkflow.ShadowWorkflowStoreDirectoryEnv: "workflow",
	}
	_, err := LoadShadowConfig(func(key string) string { return values[key] })
	if err == nil || !errors.Is(err, ErrInvalidShadowConfig) {
		t.Fatalf("invalid workflow store configuration accepted: %v", err)
	}
}
