package manager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/discovery/network"
	"synora/pkg/contract"
)

type fakeExecutor struct {
	mu sync.Mutex

	outputs map[string][]byte
	errors  map[string]error
	strict  bool

	calls      []string
	unexpected []string
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

	if err, ok := e.errors[key]; ok {
		return e.outputs[key], err
	}

	if output, ok := e.outputs[key]; ok {
		return output, nil
	}

	if e.strict {
		e.unexpected = append(e.unexpected, key)
		return nil, fmt.Errorf("unexpected command: %s", key)
	}

	return nil, nil
}

func (e *fakeExecutor) assertNoUnexpected(t *testing.T) {
	t.Helper()

	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.unexpected) != 0 {
		t.Fatalf("unexpected commands: %v", e.unexpected)
	}
}

type healthDependencyFake struct {
	t *testing.T

	networkStatus    network.Status
	networkStatusErr error
	networkConfig    network.NetworkConfig
	networkConfigErr error
	configAllowed    bool
	configPath       string

	networkStatusCalls int
	networkConfigCalls int
	httpsHealthCalls   int
	diskHealthCalls    int
}

func (f *healthDependencyFake) dependencies() healthDependencies {
	f.t.Helper()

	return healthDependencies{
		loadNetworkStatus: func() (network.Status, error) {
			f.networkStatusCalls++
			return f.networkStatus, f.networkStatusErr
		},
		loadNetworkConfig: func(path string) (network.NetworkConfig, error) {
			f.networkConfigCalls++
			if !f.configAllowed {
				f.t.Fatalf("unexpected network config probe for %q", path)
			}
			if f.configPath != "" && path != f.configPath {
				f.t.Fatalf("network config path=%q, want %q", path, f.configPath)
			}
			return f.networkConfig, f.networkConfigErr
		},
		httpsHealth: func(now time.Time) contract.RuntimeServiceHealth {
			f.httpsHealthCalls++
			return contract.RuntimeServiceHealth{Name: "https_api", Status: "ok", Active: true, Checked: now, Message: "test HTTPS probe"}
		},
		diskHealth: func(path string) contract.RuntimeDiskHealth {
			f.diskHealthCalls++
			return contract.RuntimeDiskHealth{Path: path, TotalBytes: 100, FreeBytes: 60, UsedBytes: 40, UsedPercent: 40, Status: "ok"}
		},
	}
}

func (f *healthDependencyFake) assertCalls(t *testing.T, status, config, https, disk int) {
	t.Helper()
	if f.networkStatusCalls != status || f.networkConfigCalls != config || f.httpsHealthCalls != https || f.diskHealthCalls != disk {
		t.Fatalf("health dependency calls status/config/https/disk=%d/%d/%d/%d, want %d/%d/%d/%d", f.networkStatusCalls, f.networkConfigCalls, f.httpsHealthCalls, f.diskHealthCalls, status, config, https, disk)
	}
}

func activeNetworkStatus() network.Status {
	active := network.RuntimePart{Status: "ok", Active: true}
	return network.Status{
		Enabled:    true,
		Status:     "ok",
		ActiveBand: "5GHz",
		SynoraNet:  network.RuntimePart{Status: "ok", Active: true, Message: "test 5 GHz AP active"},
		AP5GHz:     active,
		AP2GHz:     active,
		DHCP:       active,
		DNS:        active,
		HTTPSAPI:   active,
		MediaMTX:   active,
	}
}

func newHealthTestManager(t *testing.T, cfg Config, deps *healthDependencyFake) *Manager {
	t.Helper()
	return newWithHealthDependencies(cfg, deps.dependencies())
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
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
		strict:  true,
	}
	deps := &healthDependencyFake{t: t, networkStatus: activeNetworkStatus()}

	for _, service := range append(RuntimeServices, "hostapd", "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}

	manager := newHealthTestManager(
		t,
		Config{
			Executor: executor,
			DiskPath: "test-disk",
			ProbeActions: func(context.Context) error {
				return nil
			},
			ProbeMediaMTX: func(context.Context) error {
				return nil
			},
			Now: fixedNow,
		},
		deps,
	)

	health := manager.Health(
		context.Background(),
	)

	for _, service := range RuntimeServices {
		current, ok := health.Services[service]
		if !ok {
			t.Fatalf("service %s missing from health", service)
		}

		wantStatus := "active"
		if service == "synora-actions" || service == "mediamtx" {
			wantStatus = "ok"
		}
		if !current.Active || current.Status != wantStatus {
			t.Fatalf("service %s health=%#v", service, current)
		}
	}

	if len(health.Services) != len(RuntimeServices) {
		t.Fatalf("service count=%d, want %d", len(health.Services), len(RuntimeServices))
	}
	if health.Status != "ok" || len(health.Components) < len(RuntimeServices) {
		t.Fatalf("health status/components=%s/%d", health.Status, len(health.Components))
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 0, 2, 1)
}

func TestHealthKeepsActiveActionsAndMediaMTXDegradedWithUsefulMessages(t *testing.T) {
	executor := &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}, strict: true}
	for _, service := range append(RuntimeServices, "hostapd", "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}
	deps := &healthDependencyFake{t: t, networkStatus: activeNetworkStatus()}
	mediaProbeCalls := 0
	manager := newHealthTestManager(t, Config{
		Executor: executor,
		DiskPath: "test-disk",
		ProbeMediaMTX: func(context.Context) error {
			mediaProbeCalls++
			return errors.New("test MediaMTX API unavailable")
		},
		Now: fixedNow,
	}, deps)
	health := manager.Health(context.Background())
	actions := health.Services["synora-actions"]
	if actions.Status != "degraded" || !actions.Active || actions.Message != "service active, no health probe" {
		t.Fatalf("actions=%#v", actions)
	}
	media := health.Services["mediamtx"]
	if media.Status != "degraded" || !media.Active || media.Message != "service active, api probe unavailable" {
		t.Fatalf("mediamtx=%#v", media)
	}
	if health.Status != "degraded" {
		t.Fatalf("health status=%q", health.Status)
	}
	if mediaProbeCalls != 1 {
		t.Fatalf("MediaMTX probe calls=%d, want 1", mediaProbeCalls)
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 0, 2, 1)
}

func TestHealthMarksInactiveOptionalServicesUnavailable(t *testing.T) {
	executor := &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}, strict: true}
	for _, service := range append(RuntimeServices, "hostapd", "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}
	executor.outputs[commandKey("systemctl", "is-active", "synora-actions")] = []byte("inactive\n")
	executor.outputs[commandKey("systemctl", "is-active", "mediamtx")] = []byte("inactive\n")
	deps := &healthDependencyFake{t: t, networkStatus: activeNetworkStatus()}
	manager := newHealthTestManager(t, Config{Executor: executor, DiskPath: "test-disk", Now: fixedNow}, deps)
	health := manager.Health(context.Background())
	for _, name := range []string{"synora-actions", "mediamtx"} {
		item := health.Services[name]
		wantMessage := "service inactive"
		if name == "mediamtx" {
			wantMessage = "optional component inactive"
		}
		if item.Status != "unavailable" || item.Active || item.Message != wantMessage {
			t.Fatalf("%s=%#v", name, item)
		}
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 0, 2, 1)
}

func TestHealthMarksMissingHostapdDegradedWithDetails(t *testing.T) {
	executor := &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}, strict: true}
	for _, service := range append(RuntimeServices, "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}
	executor.errors[commandKey("systemctl", "is-active", "hostapd")] = errors.New("hostapd inactive")
	deps := &healthDependencyFake{
		t:                t,
		networkStatusErr: errors.New("test network status unavailable"),
		networkConfig:    network.NetworkConfig{SynoraNet: network.SynoraNetConfig{Enabled: true}},
		configAllowed:    true,
	}
	manager := newHealthTestManager(t, Config{
		Executor: executor,
		DiskPath: "test-disk",
		ProbeActions: func(context.Context) error {
			return nil
		},
		ProbeMediaMTX: func(context.Context) error {
			return nil
		},
		Now: fixedNow,
	}, deps)
	health := manager.Health(context.Background())
	if health.Status != "degraded" || health.Network.HostAPD.Name != "hostapd" || health.Network.HostAPD.Checked.IsZero() {
		t.Fatalf("health=%#v", health)
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 1, 1, 1)
}

func activeRuntimeExecutor() *fakeExecutor {
	executor := &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}, strict: true}
	for _, service := range append(RuntimeServices, "hostapd", "dnsmasq") {
		executor.outputs[commandKey("systemctl", "is-active", service)] = []byte("active\n")
	}
	return executor
}

func TestHealthMarksConfiguredSynoraNetDisabledWithoutDegradingRuntime(t *testing.T) {
	configPath := "test-network-config"
	executor := activeRuntimeExecutor()
	deps := &healthDependencyFake{
		t:                t,
		networkStatusErr: errors.New("test network status unavailable"),
		networkConfig:    network.NetworkConfig{SynoraNet: network.SynoraNetConfig{Enabled: false}},
		configAllowed:    true,
		configPath:       configPath,
	}
	manager := newHealthTestManager(t, Config{
		Executor:          executor,
		DiskPath:          "test-disk",
		NetworkConfigPath: configPath,
		ProbeActions: func(context.Context) error {
			return nil
		},
		ProbeMediaMTX: func(context.Context) error {
			return nil
		},
		Now: fixedNow,
	}, deps)
	health := manager.Health(context.Background())
	if health.Network.Status != "disabled" || health.Network.SynoraNet.Status != "disabled" || health.Status != "ok" {
		t.Fatalf("health=%#v", health)
	}
	for _, name := range []string{"ap_5ghz", "ap_2ghz", "dhcp", "dns"} {
		if item := health.Network.Details[name]; item.Status != "disabled" || item.Message != "SynoraNet disabled" {
			t.Fatalf("%s=%#v", name, item)
		}
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 1, 2, 1)
}

func TestHealthReports24GHzFallbackAsUsableDegraded(t *testing.T) {
	executor := activeRuntimeExecutor()
	deps := &healthDependencyFake{t: t, networkStatus: network.Status{Enabled: true, Status: "degraded", ActiveBand: "2.4GHz", SynoraNet: network.RuntimePart{Status: "degraded", Active: true, Message: "5 GHz failed, running 2.4 GHz fallback"}, AP5GHz: network.RuntimePart{Status: "degraded"}, AP2GHz: network.RuntimePart{Status: "degraded", Active: true}, DHCP: network.RuntimePart{Status: "ok", Active: true}, DNS: network.RuntimePart{Status: "ok", Active: true}}}
	manager := newHealthTestManager(t, Config{
		Executor: executor,
		DiskPath: "test-disk",
		ProbeActions: func(context.Context) error {
			return nil
		},
		ProbeMediaMTX: func(context.Context) error {
			return nil
		},
		Now: fixedNow,
	}, deps)
	health := manager.Health(context.Background())
	if health.Status != "degraded" || health.Network.ActiveBand != "2.4GHz" || !health.Network.SynoraNet.Active || health.Network.SynoraNet.Message == "" {
		t.Fatalf("health=%#v", health)
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 0, 2, 1)
}

func TestNewWiresProductionHealthDependencies(t *testing.T) {
	manager := New(Config{Executor: &fakeExecutor{outputs: map[string][]byte{}, errors: map[string]error{}}})

	for _, test := range []struct {
		name string
		got  any
		want any
	}{
		{name: "MediaMTX", got: manager.probeMediaMTX, want: defaultMediaMTXProbe},
		{name: "network status", got: manager.loadNetworkStatus, want: defaultNetworkStatus},
		{name: "network config", got: manager.loadNetworkConfig, want: network.LoadConfig},
		{name: "HTTPS", got: manager.httpsHealth, want: runtimeHTTPSHealth},
		{name: "disk", got: manager.diskHealthProbe, want: systemDiskHealth},
	} {
		if reflect.ValueOf(test.got).Pointer() != reflect.ValueOf(test.want).Pointer() {
			t.Fatalf("%s dependency is not the production probe", test.name)
		}
	}
}

func TestHealthUsesOnlyProvidedDependencies(t *testing.T) {
	executor := activeRuntimeExecutor()
	deps := &healthDependencyFake{t: t, networkStatus: activeNetworkStatus()}
	actionProbeCalls := 0
	mediaProbeCalls := 0
	manager := newHealthTestManager(t, Config{
		Executor: executor,
		DiskPath: "test-disk",
		ProbeActions: func(context.Context) error {
			actionProbeCalls++
			return nil
		},
		ProbeMediaMTX: func(context.Context) error {
			mediaProbeCalls++
			return nil
		},
		Now: fixedNow,
	}, deps)

	health := manager.Health(context.Background())
	if health.Services["synora-actions"].Status != "ok" || health.Services["mediamtx"].Status != "ok" || health.Network.SynoraNet.Message != "test 5 GHz AP active" {
		t.Fatalf("health=%#v", health)
	}
	if actionProbeCalls != 1 || mediaProbeCalls != 1 {
		t.Fatalf("action/MediaMTX probe calls=%d/%d, want 1/1", actionProbeCalls, mediaProbeCalls)
	}
	executor.assertNoUnexpected(t)
	deps.assertCalls(t, 1, 0, 2, 1)
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
