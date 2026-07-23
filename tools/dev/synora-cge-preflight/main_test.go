package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/internal/cge"
)

func repositoryEnvironmentFile() string {
	return filepath.Join("..", "..", "..", defaultEnvFile)
}

func TestPhysicalSmokeEnvironmentProfileUsesProductionParser(t *testing.T) {
	values, err := validateEnvironmentFile(repositoryEnvironmentFile())
	if err != nil {
		t.Fatal(err)
	}
	if values[cgeShadowEnabled] != "true" || values[cgeWorkflowEnabled] != "true" || values[cgeLedgerEnabled] != "true" || values[cgeAnalyticsEnabled] != "true" || values[cgeStoreMode] != "file" || values[cgeStoreDirectory] != "/var/lib/synora/cge/workflow" {
		t.Fatalf("profile activation values=%v", values)
	}
	config, err := cge.LoadShadowConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if string(config.Workflow.StoreMode) != "file" || !config.Workflow.SyncOnCommit {
		t.Fatalf("profile workflow durability=%+v", config.Workflow)
	}
}

func TestPhysicalSmokeEnvironmentRejectsInvalidWorkflowStoreValues(t *testing.T) {
	data, err := os.ReadFile(repositoryEnvironmentFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		from string
		to   string
	}{
		{name: "unknown mode", from: "SYNORA_CGE_SHADOW_WORKFLOW_STORE_MODE=file", to: "SYNORA_CGE_SHADOW_WORKFLOW_STORE_MODE=disk"},
		{name: "relative directory", from: "SYNORA_CGE_SHADOW_WORKFLOW_STORE_DIRECTORY=/var/lib/synora/cge/workflow", to: "SYNORA_CGE_SHADOW_WORKFLOW_STORE_DIRECTORY=workflow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.env")
			body := strings.Replace(string(data), tc.from, tc.to, 1)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := validateEnvironmentFile(path); err == nil {
				t.Fatal("invalid workflow store profile accepted")
			}
		})
	}
}

func TestEnvironmentParserRejectsUnknownDuplicateAndSecretEntries(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "unknown", body: "SYNORA_CGE_NOT_REAL=true\n"},
		{name: "duplicate", body: "SYNORA_CGE_SHADOW_ENABLED=true\nSYNORA_CGE_SHADOW_ENABLED=false\n"},
		{name: "secret", body: "SYNORA_CGE_JOURNAL_ID=secret-token\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.env")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := validateEnvironmentFile(path); err == nil {
				t.Fatalf("invalid profile accepted: %s", tc.body)
			}
		})
	}
}

func TestFilesystemPreflightUsesOnlySimulatedRoot(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"/opt/synora/bin", "/var/lib/synora", "/var/lib/synora/cge", "/var/lib/synora/cge/workflow", "/etc/synora", "/run/synora"} {
		if err := os.MkdirAll(filepath.Join(root, path[1:]), 0750); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/opt/synora/bin/synora-core", "/opt/synora/bin/synora-bus", "/opt/synora/bin/synora-actions", "/opt/synora/version.json"} {
		mode := os.FileMode(0755)
		if filepath.Ext(path) == ".json" {
			mode = 0644
		}
		if err := os.WriteFile(filepath.Join(root, path[1:]), []byte("placeholder"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateFilesystem(root); err != nil {
		t.Fatal(err)
	}
}

func TestFilesystemPreflightRequiresWorkflowDirectoryWithoutCreatingIt(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"/opt/synora/bin", "/var/lib/synora", "/var/lib/synora/cge", "/etc/synora", "/run/synora"} {
		if err := os.MkdirAll(filepath.Join(root, path[1:]), 0750); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"/opt/synora/bin/synora-core", "/opt/synora/bin/synora-bus", "/opt/synora/bin/synora-actions", "/opt/synora/version.json"} {
		mode := os.FileMode(0755)
		if filepath.Ext(path) == ".json" {
			mode = 0644
		}
		if err := os.WriteFile(filepath.Join(root, path[1:]), []byte("placeholder"), mode); err != nil {
			t.Fatal(err)
		}
	}
	workflow := filepath.Join(root, "var/lib/synora/cge/workflow")
	if err := validateFilesystem(root); err == nil {
		t.Fatal("filesystem without workflow directory accepted")
	}
	if _, err := os.Stat(workflow); !os.IsNotExist(err) {
		t.Fatalf("filesystem preflight created workflow directory: %v", err)
	}
}

func TestFilesystemPreflightRejectsMissingWrongTypeAndWorldWritable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "opt/synora/bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt/synora/bin/synora-core"), []byte("not executable"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt/synora/bin/synora-bus"), []byte("placeholder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opt/synora/bin/synora-actions"), []byte("placeholder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "var/lib/synora/cge"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/synora"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "run/synora"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateFilesystem(root); err == nil {
		t.Fatal("invalid simulated filesystem accepted")
	}
}

func TestSystemdUnitContainsPhysicalSmokeEnvironmentBoundary(t *testing.T) {
	if err := validateSystemdUnit(filepath.Join("..", "..", "..", defaultCoreUnit)); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPreflightRejectsNonARM64AndNonELF(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"synora-core", "synora-bus", "synora-actions"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not an ELF executable"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateBuild(preflightOptions{BinaryDir: dir, ExpectedArch: "arm64"}); err == nil {
		t.Fatal("non-ELF simulated build accepted")
	}
	if err := validateBuild(preflightOptions{BinaryDir: dir, ExpectedArch: "amd64"}); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
}

const (
	cgeShadowEnabled    = "SYNORA_CGE_SHADOW_ENABLED"
	cgeWorkflowEnabled  = "SYNORA_CGE_SHADOW_WORKFLOW_ENABLED"
	cgeLedgerEnabled    = "SYNORA_CGE_CALIBRATION_LEDGER_ENABLED"
	cgeAnalyticsEnabled = "SYNORA_CGE_CALIBRATION_ANALYTICS_ENABLED"
	cgeStoreMode        = "SYNORA_CGE_SHADOW_WORKFLOW_STORE_MODE"
	cgeStoreDirectory   = "SYNORA_CGE_SHADOW_WORKFLOW_STORE_DIRECTORY"
)
