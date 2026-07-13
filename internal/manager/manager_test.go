package manager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/pkg/contract"
)

type fakeExecutor struct {
	mu sync.Mutex

	outputs map[string][]byte
	errors  map[string]error

	calls []string
}

func (e *fakeExecutor) Run(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	key := commandKey(
		name,
		args...,
	)

	e.mu.Lock()
	defer e.mu.Unlock()

	e.calls = append(
		e.calls,
		key,
	)

	if err := e.errors[key]; err != nil {
		return e.outputs[key], err
	}

	return e.outputs[key], nil
}

func (e *fakeExecutor) called(
	prefix string,
) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, call := range e.calls {
		if strings.HasPrefix(
			call,
			prefix,
		) {
			return true
		}
	}

	return false
}

func TestHealthUsesRuntimeServiceNames(t *testing.T) {
	executor := &fakeExecutor{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}

	for _, service := range append(RuntimeServices, "hostapd", "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}

	manager := New(
		Config{
			Executor: executor,
			DiskPath: t.TempDir(),
			Now: func() time.Time {
				return time.Date(
					2026,
					7,
					8,
					12,
					0,
					0,
					0,
					time.UTC,
				)
			},
		},
	)

	health := manager.Health(
		context.Background(),
	)

	for _, service := range RuntimeServices {
		current, ok := health.Services[service]
		if !ok {
			t.Fatalf("service %s missing from health", service)
		}

		if !current.Active || current.Status != "active" {
			t.Fatalf("service %s health=%#v", service, current)
		}
	}

	if len(health.Services) != len(RuntimeServices) {
		t.Fatalf("service count=%d, want %d", len(health.Services), len(RuntimeServices))
	}
	if health.Status != "ok" || len(health.Components) < len(RuntimeServices) {
		t.Fatalf("health status/components=%s/%d", health.Status, len(health.Components))
	}
}

func TestHealthMarksMissingHostapdDegradedWithDetails(t *testing.T) {
	executor := &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}}
	for _, service := range append(RuntimeServices, "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}
	executor.errors[commandKey("systemctl", "is-active", "hostapd")] = errors.New("hostapd inactive")
	manager := New(Config{Executor: executor, DiskPath: t.TempDir()})
	health := manager.Health(context.Background())
	if health.Status != "degraded" || health.Network.HostAPD.Name != "hostapd" || health.Network.HostAPD.Checked.IsZero() {
		t.Fatalf("health=%#v", health)
	}
}

func TestRestartServiceRejectsServiceOutsideAllowlist(t *testing.T) {
	executor := &fakeExecutor{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}

	manager := New(
		Config{
			Executor: executor,
			DiskPath: t.TempDir(),
		},
	)

	_, err := manager.RestartService(
		context.Background(),
		"not-allowed-service",
	)
	if err == nil {
		t.Fatal("RestartService() should reject services outside allowlist")
	}

	if executor.called("systemctl restart") {
		t.Fatal("systemctl restart should not be called for rejected service")
	}
}

func TestRestartServiceAllowsRuntimeService(t *testing.T) {
	executor := &fakeExecutor{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}

	manager := New(
		Config{
			Executor: executor,
			DiskPath: t.TempDir(),
		},
	)

	result, err := manager.RestartService(
		context.Background(),
		"synora-core",
	)
	if err != nil {
		t.Fatalf("RestartService() error = %v", err)
	}

	if result.Service != "synora-core" || result.Status != "restarted" {
		t.Fatalf("result=%#v", result)
	}

	if !executor.called("systemctl restart synora-core") {
		t.Fatal("systemctl restart synora-core was not called")
	}
}

func TestSnapshotArchivesOnlyConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	snapshotDir := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(configDir, "devices.yaml"),
		[]byte("devices: []\n"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(
		filepath.Join(configDir, "certs"),
		0700,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(configDir, "certs", "server.crt"),
		[]byte("cert"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	manager := New(
		Config{
			Executor:    &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}},
			ConfigDir:   configDir,
			SnapshotDir: snapshotDir,
			DiskPath:    t.TempDir(),
			Now: func() time.Time {
				return time.Date(
					2026,
					7,
					8,
					12,
					0,
					0,
					0,
					time.UTC,
				)
			},
		},
	)

	result, err := manager.Snapshot(
		context.Background(),
		contract.RuntimeSnapshotRequest{
			Name: "../bad name",
		},
	)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if !strings.HasPrefix(result.Path, snapshotDir) {
		t.Fatalf("snapshot path %s outside %s", result.Path, snapshotDir)
	}

	names := archiveNames(
		t,
		result.Path,
	)

	assertArchiveContains(
		t,
		names,
		"devices.yaml",
	)

	assertArchiveContains(
		t,
		names,
		"certs/server.crt",
	)

	for _, name := range names {
		if strings.HasPrefix(name, "..") || strings.HasPrefix(name, "/") {
			t.Fatalf("unsafe archive path: %s", name)
		}
	}
}

func TestRollbackIsContractOnly(t *testing.T) {
	manager := New(
		Config{
			Executor: &fakeExecutor{
				outputs: map[string][]byte{},
				errors:  map[string]error{},
			},
			DiskPath: t.TempDir(),
		},
	)

	result, err := manager.Rollback(
		context.Background(),
		contract.RuntimeRollbackRequest{},
	)
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	if result.Status != "disabled" {
		t.Fatalf("rollback status=%s", result.Status)
	}
}

func TestHandleRestartDecodesPayload(t *testing.T) {
	executor := &fakeExecutor{
		outputs: map[string][]byte{},
		errors:  map[string]error{},
	}

	manager := New(
		Config{
			Executor: executor,
			DiskPath: t.TempDir(),
		},
	)

	result, err := manager.Handle(
		context.Background(),
		contract.Message{
			Type:    contract.RPCRuntimeRestartService,
			Payload: []byte(`{"service":"synora-api"}`),
		},
	)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	restart, ok := result.(contract.RuntimeRestartServiceResult)
	if !ok {
		t.Fatalf("result type=%T", result)
	}

	if restart.Service != "synora-api" {
		t.Fatalf("restart=%#v", restart)
	}
}

func TestServiceHealthCapturesSystemctlErrors(t *testing.T) {
	executor := &fakeExecutor{
		outputs: map[string][]byte{
			commandKey("systemctl", "is-active", "synora-core"): []byte("inactive\n"),
		},
		errors: map[string]error{
			commandKey("systemctl", "is-active", "synora-core"): errors.New("exit status 3"),
		},
	}

	manager := New(
		Config{
			Executor: executor,
			DiskPath: t.TempDir(),
		},
	)

	health := manager.serviceHealth(
		context.Background(),
		"synora-core",
		time.Now().UTC(),
	)

	if health.Active {
		t.Fatal("inactive service should not be active")
	}

	if health.Error == "" {
		t.Fatal("service health should include executor error")
	}
}

func archiveNames(
	t *testing.T,
	path string,
) []string {
	t.Helper()

	file, err := os.Open(
		path,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(
		file,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(
		gzipReader,
	)

	var names []string

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		names = append(
			names,
			header.Name,
		)
	}

	return names
}

func assertArchiveContains(
	t *testing.T,
	names []string,
	expected string,
) {
	t.Helper()

	for _, name := range names {
		if name == expected {
			return
		}
	}

	t.Fatalf(
		"archive missing %s, names=%v",
		expected,
		names,
	)
}

func commandKey(
	name string,
	args ...string,
) string {
	return strings.Join(
		append(
			[]string{name},
			args...,
		),
		" ",
	)
}
