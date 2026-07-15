package manager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"synora/internal/discovery/network"
	"synora/pkg/contract"
)

const (
	ServiceRuntimeManager = "runtime-manager"

	DefaultConfigDir   = "/etc/synora"
	DefaultSnapshotDir = "/var/lib/synora/snapshots"
	DefaultDiskPath    = "/var/lib/synora"
)

var RuntimeServices = []string{
	"synora-bus",
	"synora-core",
	"synora-actions",
	"synora-api",
	"synora-discovery",
	"mediamtx",
}

type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type OSExecutor struct{}

func (OSExecutor) Run(
	ctx context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	return runCommand(ctx, name, args...)
}

type Config struct {
	Executor Executor

	// ProbeActions and ProbeMediaMTX are optional transport-level probes. They
	// are deliberately injectable so health tests do not depend on local
	// services being installed.
	ProbeActions  func(context.Context) error
	ProbeMediaMTX func(context.Context) error

	ConfigDir         string
	SnapshotDir       string
	DiskPath          string
	NetworkConfigPath string

	Now func() time.Time
}

type Manager struct {
	executor Executor

	probeActions  func(context.Context) error
	probeMediaMTX func(context.Context) error

	configDir         string
	snapshotDir       string
	diskPath          string
	networkConfigPath string

	allowlist map[string]struct{}

	startedAt time.Time
	now       func() time.Time
}

func New(
	cfg Config,
) *Manager {
	if cfg.Executor == nil {
		cfg.Executor = OSExecutor{}
	}

	if cfg.ConfigDir == "" {
		cfg.ConfigDir = DefaultConfigDir
	}

	if cfg.SnapshotDir == "" {
		cfg.SnapshotDir = DefaultSnapshotDir
	}

	if cfg.DiskPath == "" {
		cfg.DiskPath = DefaultDiskPath
	}
	if cfg.NetworkConfigPath == "" {
		cfg.NetworkConfigPath = network.DefaultConfigPath
	}

	if cfg.Now == nil {
		cfg.Now = func() time.Time {
			return time.Now().UTC()
		}
	}

	allowlist := map[string]struct{}{}
	for _, service := range RuntimeServices {
		allowlist[service] = struct{}{}
	}

	return &Manager{
		executor: cfg.Executor,

		probeActions:  cfg.ProbeActions,
		probeMediaMTX: cfg.ProbeMediaMTX,

		configDir:         cfg.ConfigDir,
		snapshotDir:       cfg.SnapshotDir,
		diskPath:          cfg.DiskPath,
		networkConfigPath: cfg.NetworkConfigPath,

		allowlist: allowlist,

		startedAt: cfg.Now(),
		now:       cfg.Now,
	}
}

func (m *Manager) Health(
	ctx context.Context,
) contract.RuntimeHealth {
	now := m.now()
	if ctx == nil {
		ctx = context.Background()
	}

	services := map[string]contract.RuntimeServiceHealth{}
	components := map[string]contract.RuntimeServiceHealth{}
	allServices := append(append([]string{}, RuntimeServices...), "hostapd", "dnsmasq")
	type result struct {
		name   string
		health contract.RuntimeServiceHealth
	}
	results := make(chan result, len(allServices))
	for _, service := range allServices {
		go func(name string) {
			results <- result{name: name, health: m.serviceHealth(ctx, name, now)}
		}(service)
	}
	var hostapd, dnsmasq contract.RuntimeServiceHealth
	for range allServices {
		item := <-results
		switch item.name {
		case "hostapd":
			hostapd = item.health
		case "dnsmasq":
			dnsmasq = item.health
		default:
			services[item.name] = item.health
			components[item.name] = item.health
		}
	}

	mediaMTX := services["mediamtx"]
	services["synora-actions"] = m.actionServiceHealth(ctx, services["synora-actions"], now)
	components["synora-actions"] = services["synora-actions"]
	mediaMTX = m.mediaMTXServiceHealth(ctx, mediaMTX, now)
	services["mediamtx"] = mediaMTX
	components["mediamtx"] = mediaMTX
	networkStatus := combinedStatus(hostapd, dnsmasq)
	networkHealth := contract.RuntimeNetworkHealth{Status: networkStatus, HostAPD: hostapd, DNSMasq: dnsmasq, Details: map[string]contract.RuntimeServiceHealth{}}
	networkHealth.Details["mediamtx_rtsp"] = contract.RuntimeServiceHealth{Name: "mediamtx_rtsp", Status: mediaMTX.Status, Active: mediaMTX.Active, Checked: now, Message: "RTSP endpoint 8554"}
	networkHealth.Details["mediamtx_webrtc_hls"] = contract.RuntimeServiceHealth{Name: "mediamtx_webrtc_hls", Status: mediaMTX.Status, Active: mediaMTX.Active, Checked: now, Message: "browser live endpoints 8888/8889"}
	networkHealth.Details["https_api"] = runtimeHTTPSHealth(now)
	components["mediamtx_rtsp"] = networkHealth.Details["mediamtx_rtsp"]
	components["mediamtx_webrtc_hls"] = networkHealth.Details["mediamtx_webrtc_hls"]
	components["https_api"] = networkHealth.Details["https_api"]
	snapshot, statusErr := network.LoadStatus(os.Getenv("SYNORA_NETWORK_STATUS_FILE"))
	if statusErr != nil {
		if cfg, configErr := network.LoadConfig(m.networkConfigPath); configErr == nil && !cfg.SynoraNet.Enabled {
			disabled := network.RuntimePart{Status: "disabled", Message: "SynoraNet disabled"}
			snapshot = network.Status{Status: "disabled", SynoraNet: disabled, AP5GHz: disabled, AP2GHz: disabled, DHCP: disabled, DNS: disabled, WifiSecurity: network.WifiSecurityStatus{RuntimePart: disabled}, Visibility: network.VisibilityStatus{RuntimePart: disabled}, AccessControl: network.AccessControlStatus{RuntimePart: disabled}, ConnectionPolicy: network.ConnectionPolicyStatus{RuntimePart: disabled}, PairingSecurity: network.PairingSecurityStatus{RuntimePart: disabled}, NetworkIsolation: disabled, Firewall: disabled}
			statusErr = nil
		}
	}
	if statusErr == nil {
		networkHealth = mergeSynoraNetHealth(networkHealth, snapshot, now)
		networkHealth.Details["https_api"] = runtimeHTTPSHealth(now)
		networkHealth.Details["mediamtx_rtsp"] = contract.RuntimeServiceHealth{Name: "mediamtx_rtsp", Status: mediaMTX.Status, Active: mediaMTX.Active, Checked: now, Message: "RTSP endpoint 8554"}
		networkHealth.Details["mediamtx_webrtc_hls"] = contract.RuntimeServiceHealth{Name: "mediamtx_webrtc_hls", Status: mediaMTX.Status, Active: mediaMTX.Active, Checked: now, Message: "browser live endpoints 8888/8889"}
		for name, item := range networkHealth.Details {
			components[name] = item
		}
		components["synoranet"] = networkHealth.SynoraNet
	}
	status := "ok"
	for _, service := range services {
		if !service.Active || (service.Status != "active" && service.Status != "ok") {
			status = "degraded"
			break
		}
	}
	if networkHealth.Status != "ok" && networkHealth.Status != "disabled" {
		status = "degraded"
	}

	uptime := int64(now.Sub(m.startedAt).Seconds())
	if uptime < 1 {
		uptime = 1
	}
	health := contract.RuntimeHealth{
		Status:      status,
		GeneratedAt: now,
		Services:    services,
		Network:     networkHealth,
		Components:  components,
		MediaMTX: contract.RuntimeMediaMTXHealth{
			Status:  mediaMTX.Status,
			Service: mediaMTX,
		},
		Disk:      m.diskHealth(),
		Uptime:    uptime,
		Timestamp: now,
	}
	return contract.NormalizeRuntimeHealth(health, now)
}

func runtimeHTTPSHealth(now time.Time) contract.RuntimeServiceHealth {
	cert := os.Getenv("SYNORA_TLS_CERT_FILE")
	key := os.Getenv("SYNORA_TLS_KEY_FILE")
	if cert == "" {
		cert = "/etc/synora/tls/synora.crt"
	}
	if key == "" {
		key = "/etc/synora/tls/synora.key"
	}
	item := contract.RuntimeServiceHealth{Name: "https_api", Checked: now}
	if _, certErr := os.Stat(cert); certErr != nil {
		item.Status, item.Message = "degraded", "HTTPS configured but local certificate is missing"
		return item
	}
	if _, keyErr := os.Stat(key); keyErr != nil {
		item.Status, item.Message = "degraded", "HTTPS configured but local key is missing"
		return item
	}
	item.Status, item.Active, item.Message = "ok", true, "HTTPS API available on 8443"
	return item
}

func mergeSynoraNetHealth(base contract.RuntimeNetworkHealth, snapshot network.Status, now time.Time) contract.RuntimeNetworkHealth {
	part := func(name string, value network.RuntimePart) contract.RuntimeServiceHealth {
		status := value.Status
		if status == "" {
			status = "unavailable"
		}
		return contract.RuntimeServiceHealth{Name: name, Status: status, Active: value.Active, Checked: now, Message: value.Message}
	}
	base.SynoraNet = part("synoranet", snapshot.SynoraNet)
	base.Enabled = snapshot.Enabled
	base.AP5GHz = part("ap_5ghz", snapshot.AP5GHz)
	base.AP2GHz = part("ap_2ghz", snapshot.AP2GHz)
	base.DHCP = part("dhcp", snapshot.DHCP)
	base.DNS = part("dns", snapshot.DNS)
	security := part("wifi_security", snapshot.WifiSecurity.RuntimePart)
	security.Mode = snapshot.WifiSecurity.Mode
	security.PMF = snapshot.WifiSecurity.PMF
	security.APIsolate = snapshot.WifiSecurity.APIsolate
	base.WifiSecurity = security
	base.NetworkIsolation = part("network_isolation", snapshot.NetworkIsolation)
	base.Firewall = part("firewall", snapshot.Firewall)
	visibility := part("synoranet_visibility", snapshot.Visibility.RuntimePart)
	visibility.Hidden = snapshot.Visibility.Hidden
	visibility.PairingVisible = snapshot.Visibility.PairingVisible
	base.Visibility = visibility
	access := part("synoranet_access_control", snapshot.AccessControl.RuntimePart)
	access.EnabledComponent = snapshot.AccessControl.Enabled
	access.StationAllowlist = snapshot.AccessControl.StationAllowlist
	access.KnownDevices = snapshot.AccessControl.KnownDevices
	access.PendingDevices = snapshot.AccessControl.PendingDevices
	access.UnknownPolicy = snapshot.AccessControl.UnknownPolicy
	base.AccessControl = access
	policy := part("synoranet_connection_policy", snapshot.ConnectionPolicy.RuntimePart)
	policy.Mode = snapshot.ConnectionPolicy.Mode
	policy.PairingWindowActive = snapshot.ConnectionPolicy.PairingWindowActive
	policy.CameraPushRuntimeAllowed = snapshot.ConnectionPolicy.CameraPushRuntimeAllowed
	base.ConnectionPolicy = policy
	pairing := part("pairing_security", snapshot.PairingSecurity.RuntimePart)
	pairing.PairingWindowActive = snapshot.PairingSecurity.Active
	pairing.ExpiresAt = timePtr(snapshot.PairingSecurity.ExpiresAt)
	pairing.ClaimEndpointActive = snapshot.PairingSecurity.ClaimEndpointActive
	pairing.MaxPendingDevices = snapshot.PairingSecurity.MaxPendingDevices
	base.PairingSecurity = pairing
	base.ActiveBand = snapshot.ActiveBand
	base.GatewayIP = "10.77.0.1"
	base.Details["synoranet"] = base.SynoraNet
	base.Details["ap_5ghz"] = base.AP5GHz
	base.Details["ap_2ghz"] = base.AP2GHz
	base.Details["dhcp"] = base.DHCP
	base.Details["dns"] = base.DNS
	base.Details["wifi_security"] = base.WifiSecurity
	base.Details["network_isolation"] = base.NetworkIsolation
	base.Details["firewall"] = base.Firewall
	base.Details["synoranet_visibility"] = base.Visibility
	base.Details["synoranet_access_control"] = base.AccessControl
	base.Details["synoranet_connection_policy"] = base.ConnectionPolicy
	base.Details["pairing_security"] = base.PairingSecurity
	base.Details["https_api"] = part("https_api", snapshot.HTTPSAPI)
	base.Details["mediamtx_rtsp"] = part("mediamtx_rtsp", snapshot.MediaMTX)
	base.Status = snapshot.Status
	if base.Status == "" {
		base.Status = "degraded"
	}
	if !snapshot.Enabled {
		base.Status = "disabled"
	}
	return base
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func (m *Manager) RestartService(
	ctx context.Context,
	service string,
) (contract.RuntimeRestartServiceResult, error) {
	service = strings.TrimSpace(
		service,
	)

	if _, ok := m.allowlist[service]; !ok {
		return contract.RuntimeRestartServiceResult{}, fmt.Errorf(
			"service %q is not allowed",
			service,
		)
	}

	if _, err := m.executor.Run(
		ctx,
		"systemctl",
		"restart",
		service,
	); err != nil {
		return contract.RuntimeRestartServiceResult{}, err
	}

	return contract.RuntimeRestartServiceResult{
		Service:   service,
		Status:    "restarted",
		Timestamp: m.now(),
	}, nil
}

func (m *Manager) Snapshot(
	_ context.Context,
	req contract.RuntimeSnapshotRequest,
) (contract.RuntimeSnapshotResult, error) {
	now := m.now()

	if err := os.MkdirAll(
		m.snapshotDir,
		0750,
	); err != nil {
		return contract.RuntimeSnapshotResult{}, err
	}

	name := strings.TrimSpace(
		req.Name,
	)
	if name == "" {
		name = now.Format("20060102-150405")
	}

	name = sanitizeSnapshotName(
		name,
	)

	path := filepath.Join(
		m.snapshotDir,
		fmt.Sprintf(
			"synora-config-%s.tar.gz",
			name,
		),
	)

	if err := archiveDirectory(
		m.configDir,
		path,
	); err != nil {
		return contract.RuntimeSnapshotResult{}, err
	}

	info, err := os.Stat(
		path,
	)
	if err != nil {
		return contract.RuntimeSnapshotResult{}, err
	}

	return contract.RuntimeSnapshotResult{
		Path:      path,
		Source:    m.configDir,
		SizeBytes: info.Size(),
		Timestamp: now,
	}, nil
}

func (m *Manager) Rollback(
	_ context.Context,
	_ contract.RuntimeRollbackRequest,
) (contract.RuntimeRollbackResult, error) {
	return contract.RuntimeRollbackResult{
		Status:    "disabled",
		Reason:    "runtime.rollback contract is prepared; restore action is intentionally disabled",
		Timestamp: m.now(),
	}, nil
}

func (m *Manager) Handle(
	ctx context.Context,
	msg contract.Message,
) (any, error) {
	switch msg.Type {
	case contract.RPCRuntimeHealth:
		return m.Health(ctx), nil

	case contract.RPCRuntimeRestartService:
		var req contract.RuntimeRestartServiceRequest
		if err := decodePayload(
			msg.Payload,
			&req,
		); err != nil {
			return nil, err
		}

		return m.RestartService(
			ctx,
			req.Service,
		)

	case contract.RPCRuntimeSnapshot:
		var req contract.RuntimeSnapshotRequest
		if len(msg.Payload) > 0 {
			if err := decodePayload(
				msg.Payload,
				&req,
			); err != nil {
				return nil, err
			}
		}

		return m.Snapshot(
			ctx,
			req,
		)

	case contract.RPCRuntimeRollback:
		var req contract.RuntimeRollbackRequest
		if len(msg.Payload) > 0 {
			if err := decodePayload(
				msg.Payload,
				&req,
			); err != nil {
				return nil, err
			}
		}

		return m.Rollback(
			ctx,
			req,
		)

	default:
		return nil, fmt.Errorf(
			"unsupported runtime rpc %q",
			msg.Type,
		)
	}
}

func (m *Manager) AllowedServices() []string {
	services := make(
		[]string,
		0,
		len(m.allowlist),
	)

	for service := range m.allowlist {
		services = append(
			services,
			service,
		)
	}

	sort.Strings(
		services,
	)

	return services
}

func (m *Manager) serviceHealth(
	ctx context.Context,
	service string,
	now time.Time,
) contract.RuntimeServiceHealth {
	checkCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	output, err := m.executor.Run(
		checkCtx,
		"systemctl",
		"is-active",
		service,
	)

	status := strings.TrimSpace(
		string(output),
	)

	if status == "" && err == nil {
		status = "active"
	}

	if status == "" {
		status = "unknown"
	}

	health := contract.RuntimeServiceHealth{
		Name:    service,
		Status:  status,
		Active:  status == "active",
		Checked: now,
	}

	if err != nil {
		health.Error = err.Error()
		health.Message = err.Error()
	}

	return health
}

func (m *Manager) actionServiceHealth(
	ctx context.Context,
	health contract.RuntimeServiceHealth,
	now time.Time,
) contract.RuntimeServiceHealth {
	health.Name = "synora-actions"
	if !health.Active {
		health.Status = "unavailable"
		health.Message = "service inactive"
		return health
	}
	if m.probeActions == nil {
		health.Status = "degraded"
		health.Message = "service active, no health probe"
		health.Error = ""
		health.Checked = now
		return health
	}
	probeCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	if err := m.probeActions(probeCtx); err != nil {
		health.Status = "degraded"
		health.Message = "service active, health probe failed"
		health.Error = err.Error()
	} else {
		health.Status = "ok"
		health.Message = "action service reachable"
		health.Error = ""
	}
	health.Checked = now
	return health
}

func (m *Manager) mediaMTXServiceHealth(
	ctx context.Context,
	health contract.RuntimeServiceHealth,
	now time.Time,
) contract.RuntimeServiceHealth {
	health.Name = "mediamtx"
	if !health.Active {
		health.Status = "unavailable"
		health.Message = "optional component inactive"
		return health
	}
	probe := m.probeMediaMTX
	if probe == nil {
		probe = defaultMediaMTXProbe
	}
	probeCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	if err := probe(probeCtx); err != nil {
		health.Status = "degraded"
		health.Message = "service active, api probe unavailable"
		health.Error = err.Error()
	} else {
		health.Status = "ok"
		health.Message = "api reachable"
		health.Error = ""
	}
	health.Checked = now
	return health
}

func defaultMediaMTXProbe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:9997/v3/paths/list", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	return nil
}

func (m *Manager) diskHealth() contract.RuntimeDiskHealth {
	var stat syscall.Statfs_t

	if err := syscall.Statfs(
		m.diskPath,
		&stat,
	); err != nil {
		return contract.RuntimeDiskHealth{
			Path:   m.diskPath,
			Status: "unknown",
			Error:  err.Error(),
		}
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free

	usedPercent := 0
	if total > 0 {
		usedPercent = int(
			used * 100 / total,
		)
	}

	status := "ok"
	if usedPercent >= 90 {
		status = "critical"
	} else if usedPercent >= 80 {
		status = "warning"
	}

	return contract.RuntimeDiskHealth{
		Path:        m.diskPath,
		TotalBytes:  total,
		FreeBytes:   free,
		UsedBytes:   used,
		UsedPercent: usedPercent,
		Status:      status,
	}
}

func combinedStatus(
	services ...contract.RuntimeServiceHealth,
) string {
	for _, service := range services {
		if !service.Active {
			return "degraded"
		}
	}

	return "ok"
}

func decodePayload(
	payload []byte,
	out any,
) error {
	if len(payload) == 0 {
		return errors.New("payload required")
	}

	return json.Unmarshal(
		payload,
		out,
	)
}

func sanitizeSnapshotName(
	name string,
) string {
	var b strings.Builder

	for _, current := range name {
		switch {
		case current >= 'a' && current <= 'z':
			b.WriteRune(current)
		case current >= 'A' && current <= 'Z':
			b.WriteRune(current)
		case current >= '0' && current <= '9':
			b.WriteRune(current)
		case current == '-', current == '_':
			b.WriteRune(current)
		}
	}

	value := b.String()
	if value == "" {
		return "snapshot"
	}

	return value
}

func archiveDirectory(
	source string,
	destination string,
) error {
	sourceInfo, err := os.Stat(
		source,
	)
	if err != nil {
		return err
	}

	if !sourceInfo.IsDir() {
		return fmt.Errorf(
			"%s is not a directory",
			source,
		)
	}

	file, err := os.Create(
		destination,
	)
	if err != nil {
		return err
	}

	defer file.Close()

	gzipWriter := gzip.NewWriter(
		file,
	)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(
		gzipWriter,
	)
	defer tarWriter.Close()

	return filepath.WalkDir(
		source,
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if path == source {
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return err
			}

			if !info.Mode().IsRegular() && !info.IsDir() {
				return nil
			}

			relative, err := filepath.Rel(
				source,
				path,
			)
			if err != nil {
				return err
			}

			header, err := tar.FileInfoHeader(
				info,
				"",
			)
			if err != nil {
				return err
			}

			header.Name = filepath.ToSlash(
				relative,
			)

			if err := tarWriter.WriteHeader(
				header,
			); err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			input, err := os.Open(
				path,
			)
			if err != nil {
				return err
			}

			_, err = io.Copy(
				tarWriter,
				input,
			)

			closeErr := input.Close()
			if err != nil {
				return err
			}

			return closeErr
		},
	)
}
