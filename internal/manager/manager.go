package manager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

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

	ConfigDir   string
	SnapshotDir string
	DiskPath    string

	Now func() time.Time
}

type Manager struct {
	executor Executor

	configDir   string
	snapshotDir string
	diskPath    string

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

		configDir:   cfg.ConfigDir,
		snapshotDir: cfg.SnapshotDir,
		diskPath:    cfg.DiskPath,

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
	status := "ok"
	for _, service := range services {
		if !service.Active {
			status = "degraded"
			break
		}
	}
	if hostapd.Status != "active" || dnsmasq.Status != "active" {
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
		Network: contract.RuntimeNetworkHealth{
			Status: combinedStatus(
				hostapd,
				dnsmasq,
			),
			HostAPD: hostapd,
			DNSMasq: dnsmasq,
		},
		Components: components,
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
