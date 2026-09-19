package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"synora/internal/discovery/network"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "status":
		status(os.Args[2:])
	case "validate":
		validate(os.Args[2:])
	case "set-hidden":
		setHidden(os.Args[2:])
	case "set-policy":
		setPolicy(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synora-network-config {status|validate|set-hidden|set-policy} [flags]")
	os.Exit(2)
}

func status(args []string) {
	flags := flag.NewFlagSet("status", flag.ExitOnError)
	path := flags.String("path", network.DefaultConfigPath, "SynoraNet config path")
	_ = flags.Parse(args)
	cfg, err := network.LoadConfig(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "network config invalid: %v\n", err)
		os.Exit(1)
	}
	view := map[string]any{
		"enabled":                cfg.SynoraNet.Enabled,
		"interface":              cfg.SynoraNet.Interface,
		"ssid":                   cfg.SynoraNet.SSID,
		"security_mode":          cfg.SynoraNet.Security.Mode,
		"pmf":                    cfg.SynoraNet.Security.PMF,
		"hidden_by_default":      cfg.SynoraNet.Visibility.HiddenByDefault,
		"pairing_window_seconds": cfg.SynoraNet.Pairing.WindowSeconds,
		"connection_policy":      cfg.SynoraNet.ConnectionPolicy.Mode,
		"firewall_enabled":       cfg.SynoraNet.Firewall.Enabled,
	}
	data, _ := json.MarshalIndent(view, "", "  ")
	fmt.Println(string(data))
}

func validate(args []string) {
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	path := flags.String("path", network.DefaultConfigPath, "SynoraNet config path")
	_ = flags.Parse(args)
	cfg, err := network.LoadConfig(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("valid: %s\n", *path)
	_ = cfg
}

func setHidden(args []string) {
	flags := flag.NewFlagSet("set-hidden", flag.ExitOnError)
	path := flags.String("path", network.DefaultConfigPath, "SynoraNet config path")
	value := flags.Bool("value", true, "hide SSID by default")
	_ = flags.Parse(args)
	cfg, err := network.LoadConfig(*path)
	if err != nil {
		fail(err)
	}
	cfg.SynoraNet.Visibility.HiddenByDefault = *value
	backup, err := network.WriteConfigWithBackup(*path, cfg, time.Now().UTC())
	if err != nil {
		fail(err)
	}
	fmt.Printf("updated hidden_by_default=%t; backup=%s\n", *value, backup)
}

func setPolicy(args []string) {
	flags := flag.NewFlagSet("set-policy", flag.ExitOnError)
	path := flags.String("path", network.DefaultConfigPath, "SynoraNet config path")
	value := flags.String("value", "central_initiated", "central_initiated")
	_ = flags.Parse(args)
	if strings.TrimSpace(*value) != "central_initiated" {
		fail(fmt.Errorf("unsupported connection policy %q", *value))
	}
	cfg, err := network.LoadConfig(*path)
	if err != nil {
		fail(err)
	}
	cfg.SynoraNet.ConnectionPolicy.Mode = strings.TrimSpace(*value)
	if err := network.ValidateConfig(cfg.SynoraNet); err != nil {
		fail(err)
	}
	backup, err := network.WriteConfigWithBackup(*path, cfg, time.Now().UTC())
	if err != nil {
		fail(err)
	}
	fmt.Printf("updated connection_policy=%s; backup=%s\n", *value, backup)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}
