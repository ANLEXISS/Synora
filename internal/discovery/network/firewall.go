package network

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// RenderFirewall renders only Synora-owned nftables state. The chains have an
// accept policy so unrelated INPUT/OUTPUT/FORWARD traffic is untouched; every
// restrictive rule is scoped to synorabr0.
func RenderFirewall(cfg SynoraNetConfig) string { return RenderFirewallState(cfg, false) }

func RenderFirewallState(cfg SynoraNetConfig, pairing bool) string {
	input := inputPorts(cfg, pairing)
	central := intPorts(cfg.Firewall.CentralToCameraAllowedPorts)
	var b strings.Builder
	b.WriteString("add table inet synora_net\n")
	b.WriteString("add chain inet synora_net synora_input { type filter hook input priority -10; policy accept; }\n")
	b.WriteString("add chain inet synora_net synora_forward { type filter hook forward priority -10; policy accept; }\n")
	b.WriteString("add chain inet synora_net synora_output { type filter hook output priority -10; policy accept; }\n")
	for _, rule := range input {
		b.WriteString("add rule inet synora_net synora_input iifname \"" + BridgeName + "\" " + rule + " accept\n")
	}
	if cfg.ConnectionPolicy.AllowEstablishedRelated {
		b.WriteString("add rule inet synora_net synora_input iifname \"" + BridgeName + "\" ct state established,related accept\n")
	}
	b.WriteString("add rule inet synora_net synora_input iifname \"" + BridgeName + "\" drop\n")
	if cfg.ConnectionPolicy.AllowEstablishedRelated {
		b.WriteString("add rule inet synora_net synora_output oifname \"" + BridgeName + "\" ct state established,related accept\n")
	}
	if len(central) > 0 {
		b.WriteString("add rule inet synora_net synora_output oifname \"" + BridgeName + "\" tcp dport { " + strings.Join(central, ", ") + " } accept\n")
	}
	b.WriteString("add rule inet synora_net synora_output oifname \"" + BridgeName + "\" udp sport { 53, 67, 68 } accept\n")
	b.WriteString("add rule inet synora_net synora_output oifname \"" + BridgeName + "\" drop\n")
	if cfg.Firewall.BlockForwardToLAN || cfg.Firewall.BlockForwardToTailscale || cfg.Firewall.BlockForwardToInternet || cfg.Firewall.BlockClientToClient {
		b.WriteString("add rule inet synora_net synora_forward iifname \"" + BridgeName + "\" drop\n")
	}
	if cfg.Firewall.BlockClientToClient {
		b.WriteString("add rule inet synora_net synora_forward iifname \"" + BridgeName + "\" oifname \"" + BridgeName + "\" drop\n")
	}
	return b.String()
}

func inputPorts(cfg SynoraNetConfig, pairing bool) []string {
	ports := []string{}
	add := func(protocol string, values ...int) {
		if len(values) == 0 {
			return
		}
		formatted := intPorts(values)
		ports = append(ports, protocol+" dport { "+strings.Join(formatted, ", ")+" }")
	}
	if cfg.Firewall.AllowDHCP {
		add("udp", 67, 68)
	}
	if cfg.Firewall.AllowDNS {
		add("udp", 53)
		add("tcp", 53)
	}
	if cfg.Firewall.AllowNTPLocal {
		add("udp", 123)
	}
	if pairing {
		for _, port := range cfg.Firewall.PairingAllowedPorts {
			if port != 53 && port != 67 && port != 68 {
				add("tcp", port)
			}
		}
	} else if cfg.ConnectionPolicy.Mode == "camera_push_legacy" && cfg.ConnectionPolicy.AllowCameraPushRuntime {
		if cfg.Firewall.AllowAPIHTTPFromClients || cfg.Firewall.AllowAPIHTTP {
			add("tcp", 8080)
		}
		if cfg.Firewall.AllowAPIHTTPSFromClients || cfg.Firewall.AllowAPIHTTPS {
			add("tcp", 8443)
		}
		if cfg.Firewall.AllowVisionIngressFromClients || cfg.Firewall.AllowVisionIngress {
			add("tcp", 7070)
		}
		if cfg.Firewall.AllowMediaRTSPFromClients || cfg.Firewall.AllowMediaRTSP {
			add("tcp", 8554)
		}
		if cfg.Firewall.AllowMediaWebRTCFromClients || cfg.Firewall.AllowMediaWebRTC {
			add("tcp", 8889)
		}
		if cfg.Firewall.AllowMediaHLSFromClients || cfg.Firewall.AllowMediaHLS {
			add("tcp", 8888)
		}
		if cfg.Firewall.AllowMediaMTXAPI {
			add("tcp", 9997)
		}
	}
	return ports
}

func intPorts(values []int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value >= 1 && value <= 65535 {
			out = append(out, strconv.Itoa(value))
		}
	}
	return out
}

type firewallCommand func(name string, args []string, stdin string) error

func runFirewallCommand(name string, args []string, stdin string) error {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd.Run()
}

func ensureNFTables(cfg SynoraNetConfig, pairing bool, run firewallCommand) error {
	if err := run("nft", []string{"list", "table", "inet", "synora_net"}, ""); err == nil {
		if err := run("nft", []string{"delete", "table", "inet", "synora_net"}, ""); err != nil {
			return err
		}
	}
	return run("nft", []string{"-f", "-"}, RenderFirewallState(cfg, pairing))
}

func ensureIPTables(cfg SynoraNetConfig, pairing bool, run firewallCommand) error {
	for _, args := range [][]string{{"-N", "synora_input"}, {"-F", "synora_input"}, {"-N", "synora_forward"}, {"-F", "synora_forward"}, {"-N", "synora_output"}, {"-F", "synora_output"}} {
		_ = run("iptables", args, "")
	}
	for _, hook := range []struct{ parent, child string }{{"INPUT", "synora_input"}, {"FORWARD", "synora_forward"}, {"OUTPUT", "synora_output"}} {
		check := []string{"-C", hook.parent, "-i", BridgeName, "-j", hook.child}
		if hook.parent == "OUTPUT" {
			check = []string{"-C", hook.parent, "-o", BridgeName, "-j", hook.child}
		}
		if err := run("iptables", check, ""); err != nil {
			insert := []string{"-I", hook.parent, "1", "-i", BridgeName, "-j", hook.child}
			if hook.parent == "OUTPUT" {
				insert = []string{"-I", hook.parent, "1", "-o", BridgeName, "-j", hook.child}
			}
			if err := run("iptables", insert, ""); err != nil {
				return err
			}
		}
	}
	for _, rule := range iptablesInputRules(cfg, pairing) {
		if err := run("iptables", append([]string{"-A", "synora_input", "-i", BridgeName}, rule...), ""); err != nil {
			return err
		}
	}
	if cfg.ConnectionPolicy.AllowEstablishedRelated {
		if err := run("iptables", []string{"-A", "synora_input", "-i", BridgeName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}, ""); err != nil {
			return err
		}
	}
	if err := run("iptables", []string{"-A", "synora_input", "-i", BridgeName, "-j", "DROP"}, ""); err != nil {
		return err
	}
	if cfg.ConnectionPolicy.AllowEstablishedRelated {
		_ = run("iptables", []string{"-A", "synora_output", "-o", BridgeName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}, "")
	}
	for _, port := range cfg.Firewall.CentralToCameraAllowedPorts {
		if err := run("iptables", []string{"-A", "synora_output", "-o", BridgeName, "-p", "tcp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"}, ""); err != nil {
			return err
		}
	}
	if err := run("iptables", []string{"-A", "synora_output", "-o", BridgeName, "-p", "udp", "--sport", "53:68", "-j", "ACCEPT"}, ""); err != nil {
		return err
	}
	if err := run("iptables", []string{"-A", "synora_output", "-o", BridgeName, "-j", "DROP"}, ""); err != nil {
		return err
	}
	if cfg.Firewall.BlockForwardToLAN || cfg.Firewall.BlockForwardToTailscale || cfg.Firewall.BlockForwardToInternet || cfg.Firewall.BlockClientToClient {
		if err := run("iptables", []string{"-A", "synora_forward", "-i", BridgeName, "-j", "DROP"}, ""); err != nil {
			return err
		}
	}
	return ensureIP6Tables(run)
}

func ensureIP6Tables(run firewallCommand) error {
	for _, args := range [][]string{{"-N", "synora_v6_input"}, {"-F", "synora_v6_input"}, {"-N", "synora_v6_forward"}, {"-F", "synora_v6_forward"}, {"-N", "synora_v6_output"}, {"-F", "synora_v6_output"}} {
		_ = run("ip6tables", args, "")
	}
	for _, hook := range []struct{ parent, child string }{{"INPUT", "synora_v6_input"}, {"FORWARD", "synora_v6_forward"}, {"OUTPUT", "synora_v6_output"}} {
		check := []string{"-C", hook.parent, "-i", BridgeName, "-j", hook.child}
		if hook.parent == "OUTPUT" {
			check = []string{"-C", hook.parent, "-o", BridgeName, "-j", hook.child}
		}
		if err := run("ip6tables", check, ""); err != nil {
			insert := []string{"-I", hook.parent, "1", "-i", BridgeName, "-j", hook.child}
			if hook.parent == "OUTPUT" {
				insert = []string{"-I", hook.parent, "1", "-o", BridgeName, "-j", hook.child}
			}
			if err := run("ip6tables", insert, ""); err != nil {
				return err
			}
		}
	}
	if err := run("ip6tables", []string{"-A", "synora_v6_input", "-i", BridgeName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}, ""); err != nil {
		return err
	}
	if err := run("ip6tables", []string{"-A", "synora_v6_input", "-i", BridgeName, "-j", "DROP"}, ""); err != nil {
		return err
	}
	if err := run("ip6tables", []string{"-A", "synora_v6_forward", "-i", BridgeName, "-j", "DROP"}, ""); err != nil {
		return err
	}
	if err := run("ip6tables", []string{"-A", "synora_v6_output", "-o", BridgeName, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"}, ""); err != nil {
		return err
	}
	return run("ip6tables", []string{"-A", "synora_v6_output", "-o", BridgeName, "-j", "DROP"}, "")
}

func iptablesInputRules(cfg SynoraNetConfig, pairing bool) [][]string {
	// Keep this fallback function intentionally narrow; tests and deployments
	// share the same port policy as nftables.
	rules := [][]string{}
	add := func(protocol string, port int) {
		rules = append(rules, []string{"-p", protocol, "--dport", strconv.Itoa(port), "-j", "ACCEPT"})
	}
	if cfg.Firewall.AllowDHCP {
		add("udp", 67)
		add("udp", 68)
	}
	if cfg.Firewall.AllowDNS {
		add("udp", 53)
		add("tcp", 53)
	}
	if cfg.Firewall.AllowNTPLocal {
		add("udp", 123)
	}
	if pairing {
		for _, port := range cfg.Firewall.PairingAllowedPorts {
			if port != 53 && port != 67 && port != 68 {
				add("tcp", port)
			}
		}
	}
	return rules
}

func EnsureFirewallState(cfg SynoraNetConfig, pairing bool) error {
	if !cfg.Firewall.Enabled {
		return nil
	}
	if err := ensureNFTables(cfg, pairing, runFirewallCommand); err == nil {
		return nil
	}
	if err := ensureIPTables(cfg, pairing, runFirewallCommand); err == nil {
		return nil
	}
	return fmt.Errorf("unable to apply SynoraNet firewall with nftables or iptables")
}

func EnsureFirewallFor(cfg SynoraNetConfig) error { return EnsureFirewallState(cfg, false) }

func EnsureFirewall() error {
	cfg, err := LoadConfigWithEnv()
	if err != nil {
		return err
	}
	return EnsureFirewallFor(cfg.SynoraNet)
}

func LoadConfigWithEnv() (NetworkConfig, error) {
	return LoadConfig(strings.TrimSpace(getenv("SYNORA_NETWORK_CONFIG")))
}

var getenv = os.Getenv
