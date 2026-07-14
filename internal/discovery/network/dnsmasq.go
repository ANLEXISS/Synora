package network

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func renderDnsmasqConfig(cfg SynoraNetConfig) string {
	lines := []string{
		"interface=" + BridgeName, "bind-interfaces", "no-resolv", "no-hosts",
		"dhcp-range=" + cfg.DHCPStart + "," + cfg.DHCPEnd + "," + cfg.DHCLeaseTime,
		"dhcp-option=3," + cfg.GatewayIP, "dhcp-option=6," + cfg.GatewayIP,
		"listen-address=" + cfg.GatewayIP, "domain-needed", "bogus-priv",
	}
	if cfg.DNS.Enabled {
		for name, ip := range cfg.DNS.Names {
			lines = append(lines, "address=/"+name+"/"+ip)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func ensureDnsmasq(cfg SynoraNetConfig) error {
	if err := os.MkdirAll(DefaultRunDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(DnsmasqConfigPath, []byte(renderDnsmasqConfig(cfg)), 0644); err != nil {
		return err
	}
	// dnsmasq daemonizes by default, allowing Discovery to continue. A system
	// dnsmasq already owning the interface reports an ordinary degraded error.
	if err := exec.Command("dnsmasq", "--conf-file="+DnsmasqConfigPath, "--pid-file="+DefaultRunDir+"/dnsmasq-synoranet.pid").Run(); err != nil {
		return fmt.Errorf("dnsmasq start: %w", err)
	}
	return nil
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
