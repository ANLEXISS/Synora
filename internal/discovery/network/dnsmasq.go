package network

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func renderDnsmasqConfig(cfg SynoraNetConfig) string {
	return renderDnsmasqConfigState(cfg, false, nil)
}

func renderDnsmasqConfigState(cfg SynoraNetConfig, pairing bool, stations []KnownStation) string {
	lines := []string{
		"interface=" + BridgeName, "bind-interfaces", "no-resolv", "no-hosts",
		"dhcp-range=" + cfg.DHCPStart + "," + cfg.DHCPEnd + "," + cfg.DHCLeaseTime,
		"dhcp-option=3," + cfg.GatewayIP, "dhcp-option=6," + cfg.GatewayIP,
		"listen-address=" + cfg.GatewayIP, "domain-needed", "bogus-priv",
	}
	for _, station := range stations {
		if station.AllowWiFi && station.Trust == "paired" && station.MAC != "" && station.StaticIP != "" {
			lines = append(lines, "dhcp-host="+station.MAC+","+station.StaticIP+","+station.DeviceID+","+cfg.DHCLeaseTime+",set:known")
		}
	}
	if cfg.AccessControl.Enabled && cfg.AccessControl.BindDHCPToKnown && !pairing {
		lines = append(lines, "dhcp-ignore=tag:!known")
	}
	if cfg.DNS.Enabled {
		for name, ip := range cfg.DNS.Names {
			lines = append(lines, "address=/"+name+"/"+ip)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func ensureDnsmasq(cfg SynoraNetConfig) error {
	return ensureDnsmasqState(cfg, false, nil)
}

func ensureDnsmasqState(cfg SynoraNetConfig, pairing bool, stations []KnownStation) error {
	if err := os.MkdirAll(DefaultRunDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(DnsmasqConfigPath, []byte(renderDnsmasqConfigState(cfg, pairing, stations)), 0644); err != nil {
		return err
	}
	// dnsmasq daemonizes by default, allowing Discovery to continue. A system
	// dnsmasq already owning the interface reports an ordinary degraded error.
	if err := exec.Command("dnsmasq", "--conf-file="+DnsmasqConfigPath, "--pid-file="+DefaultRunDir+"/dnsmasq-synoranet.pid").Run(); err != nil {
		return fmt.Errorf("dnsmasq start: %w", err)
	}
	return nil
}

func reloadDnsmasqState(cfg SynoraNetConfig, pairing bool, stations []KnownStation) error {
	if err := os.MkdirAll(DefaultRunDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(DnsmasqConfigPath, []byte(renderDnsmasqConfigState(cfg, pairing, stations)), 0644); err != nil {
		return err
	}
	pidPath := DefaultRunDir + "/dnsmasq-synoranet.pid"
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); parseErr == nil {
			if process, findErr := os.FindProcess(pid); findErr == nil && process.Signal(syscall.SIGHUP) == nil {
				return nil
			}
		}
	}
	return exec.Command("dnsmasq", "--conf-file="+DnsmasqConfigPath, "--pid-file="+pidPath).Run()
}

func EnsureDnsmasq() error {
	cfg, err := LoadConfig(os.Getenv("SYNORA_NETWORK_CONFIG"))
	if err != nil {
		return err
	}
	if !cfg.SynoraNet.Enabled {
		return nil
	}
	return ensureDnsmasq(cfg.SynoraNet)
}
