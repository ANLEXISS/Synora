package main

import (
	"os"
	"path/filepath"
	"testing"
)

func repositoryEnvironmentFile() string {
	return filepath.Join("..", "..", "..", defaultEnvFile)
}

func TestPhysicalSmokeEnvironmentProfileUsesProductionParser(t *testing.T) {
	values, err := validateEnvironmentFile(repositoryEnvironmentFile())
	if err != nil {
		t.Fatal(err)
	}
	if values[cgeShadowEnabled] != "true" || values[cgeWorkflowEnabled] != "true" || values[cgeLedgerEnabled] != "true" || values[cgeAnalyticsEnabled] != "true" {
		t.Fatalf("profile activation values=%v", values)
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
	if err := validateFilesystem(root); err != nil {
		t.Fatal(err)
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
)
