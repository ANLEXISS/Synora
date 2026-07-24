package shadowworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func storeEnvironment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadStoreConfigBoundary(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "workflow")
	tests := []struct {
		name      string
		values    map[string]string
		wantMode  StoreMode
		wantDir   string
		wantError bool
	}{
		{name: "absent defaults to memory", values: map[string]string{}, wantMode: StoreMemory},
		{name: "explicit memory", values: map[string]string{ShadowWorkflowStoreModeEnv: "memory"}, wantMode: StoreMemory},
		{name: "memory with directory is ambiguous", values: map[string]string{ShadowWorkflowStoreModeEnv: "memory", ShadowWorkflowStoreDirectoryEnv: directory}, wantError: true},
		{name: "file with absolute directory", values: map[string]string{ShadowWorkflowStoreModeEnv: "file", ShadowWorkflowStoreDirectoryEnv: directory}, wantMode: StoreFile, wantDir: directory},
		{name: "file without directory", values: map[string]string{ShadowWorkflowStoreModeEnv: "file"}, wantError: true},
		{name: "file with relative directory", values: map[string]string{ShadowWorkflowStoreModeEnv: "file", ShadowWorkflowStoreDirectoryEnv: "workflow"}, wantError: true},
		{name: "unknown mode", values: map[string]string{ShadowWorkflowStoreModeEnv: "disk"}, wantError: true},
		{name: "mode case is rejected", values: map[string]string{ShadowWorkflowStoreModeEnv: "FILE", ShadowWorkflowStoreDirectoryEnv: directory}, wantError: true},
		{name: "mode whitespace is rejected", values: map[string]string{ShadowWorkflowStoreModeEnv: " file", ShadowWorkflowStoreDirectoryEnv: directory}, wantError: true},
		{name: "root directory is rejected", values: map[string]string{ShadowWorkflowStoreModeEnv: "file", ShadowWorkflowStoreDirectoryEnv: "/"}, wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mode, gotDir, err := LoadStoreConfig(storeEnvironment(tc.values))
			if tc.wantError {
				if err == nil || !errors.Is(err, ErrInvalidConfig) {
					t.Fatalf("expected invalid config, mode=%q dir=%q err=%v", mode, gotDir, err)
				}
				return
			}
			if err != nil || mode != tc.wantMode || gotDir != tc.wantDir {
				t.Fatalf("mode=%q dir=%q err=%v", mode, gotDir, err)
			}
		})
	}
}

func TestLoadStoreConfigDoesNotCreateDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "not-created", "workflow")
	mode, gotDir, err := LoadStoreConfig(storeEnvironment(map[string]string{
		ShadowWorkflowStoreModeEnv:      "file",
		ShadowWorkflowStoreDirectoryEnv: directory,
	}))
	if err != nil || mode != StoreFile || gotDir != directory {
		t.Fatalf("mode=%q dir=%q err=%v", mode, gotDir, err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("parser created workflow directory: stat err=%v", err)
	}
}

func TestDefaultStoreConfigRemainsMemoryAndSyncDurable(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.StoreMode != StoreMemory || cfg.StoreDirectory != "" || !cfg.SyncOnCommit || !cfg.AllowTruncatedFinalRecord {
		t.Fatalf("historical defaults changed: %+v", cfg)
	}
	mode, directory, err := LoadStoreConfig(storeEnvironment(nil))
	if err != nil || mode != StoreMemory || directory != "" {
		t.Fatalf("environment defaults changed: mode=%q directory=%q err=%v", mode, directory, err)
	}
}

func TestMemoryRuntimeDoesNotCreateWorkflowDirectory(t *testing.T) {
	parent := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxProcessingDuration = time.Second
	runtime, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status := runtime.Status(); status.StoreMode != StoreMemory || status.StorePersistent {
		t.Fatalf("memory runtime status=%+v", status)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("memory runtime created filesystem entries: %v", entries)
	}
}

func TestConfigValidationRejectsInvalidStoreBoundaries(t *testing.T) {
	base := DefaultConfig()
	base.Enabled = true
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{name: "memory directory", edit: func(cfg *Config) { cfg.StoreDirectory = filepath.Join(t.TempDir(), "workflow") }},
		{name: "file missing directory", edit: func(cfg *Config) { cfg.StoreMode = StoreFile }},
		{name: "file relative directory", edit: func(cfg *Config) { cfg.StoreMode = StoreFile; cfg.StoreDirectory = "workflow" }},
		{name: "file root directory", edit: func(cfg *Config) { cfg.StoreMode = StoreFile; cfg.StoreDirectory = "/" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.edit(&cfg)
			if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("invalid store config accepted: %v", err)
			}
		})
	}
}
