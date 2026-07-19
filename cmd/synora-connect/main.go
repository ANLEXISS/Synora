package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"synora/internal/connectivity"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		os.Exit(run(os.Args[2:]))
	case "status":
		os.Exit(status(os.Args[2:]))
	case "explain":
		os.Exit(explain(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

type options struct{ config, data, bus string }

func parseOptions(name string, args []string) (options, error) {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	opts := options{}
	set.StringVar(&opts.config, "config", envOr("SYNORA_CONNECTIVITY_CONFIG", connectivity.DefaultConfigPath), "connectivity config path")
	set.StringVar(&opts.data, "data-dir", envOr("SYNORA_CONNECTIVITY_DIR", "/var/lib/synora/connectivity"), "persistent connectivity directory")
	set.StringVar(&opts.bus, "bus", envOr("SYNORA_BUS", "/run/synora/bus.sock"), "local Synora bus Unix socket")
	if err := set.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func run(args []string) int {
	opts, err := parseOptions("run", args)
	if err != nil {
		return 2
	}
	cfg, err := connectivity.Load(opts.config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid connectivity configuration")
		return 2
	}
	agent, err := connectivity.NewAgent(cfg, opts.data, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot initialize connectivity identity or state")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, opts.bus); err != nil {
		fmt.Fprintln(os.Stderr, "cannot connect to local Synora bus")
		return 1
	}
	return 0
}

func status(args []string) int {
	opts, err := parseOptions("status", args)
	if err != nil {
		return 2
	}
	cfg, err := connectivity.Load(opts.config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid connectivity configuration")
		return 2
	}
	current, err := connectivity.ReadStatus(opts.data, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read connectivity status")
		return 1
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return 1
	}
	fmt.Println(string(data))
	return 0
}

func explain(args []string) int {
	opts, err := parseOptions("explain", args)
	if err != nil {
		return 2
	}
	cfg, err := connectivity.Load(opts.config)
	if err != nil {
		fmt.Println("configuration: invalid")
		return 2
	}
	status, err := connectivity.ReadStatus(opts.data, cfg)
	if err != nil {
		fmt.Println("status: unavailable")
		return 1
	}
	edPresent, wgPresent := connectivity.MaterialPresence(opts.data)
	fmt.Printf("state: %s\nmode: %s\nenabled: %t\ndevice_id: %s\nidentity_fingerprint: %s\nprovisioned: %t\ninterface: %s\nlast_transition: %s\nlast_error_code: %s\ned25519_material: %s\nwireguard_material: %s\ntransport: no external network and no WireGuard interface in this pass\n", status.State, status.Mode, status.Enabled, status.DeviceID, shortFingerprint(status.IdentityFingerprint), status.Provisioned, status.InterfaceName, status.LastTransition.UTC().Format("2006-01-02T15:04:05Z"), status.LastErrorCode, presence(edPresent), presence(wgPresent))
	return 0
}

func shortFingerprint(value string) string {
	if len(value) > 23 {
		return value[:23]
	}
	return value
}

func presence(value bool) string {
	if value {
		return "present"
	}
	return "absent"
}
func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func usage() {
	fmt.Fprintln(os.Stderr, "usage: synora-connect run|status|explain [--config PATH] [--data-dir DIR] [--bus SOCKET]")
}
