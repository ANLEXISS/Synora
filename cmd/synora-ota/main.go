package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"synora/internal/ota"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	rauc := fs.String("rauc", envOr("SYNORA_RAUC_BIN", "rauc"), "RAUC executable")
	healthcheck := fs.String("healthcheck", envOr("SYNORA_BOOT_HEALTHCHECK", "/opt/synora/bin/synora-boot-healthcheck"), "post-boot healthcheck executable")
	timeout := fs.Duration("timeout", 10*time.Minute, "operation timeout")
	_ = fs.Parse(os.Args[2:])
	controller := ota.NewController(commandRunner, *rauc, *healthcheck)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var err error
	switch os.Args[1] {
	case "status":
		var status ota.Status
		status, err = controller.Status(ctx)
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(status)
		}
	case "install":
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: synora-ota install [flags] BUNDLE")
			os.Exit(2)
		}
		err = controller.Install(ctx, fs.Arg(0))
	case "mark-good":
		err = controller.MarkGood(ctx)
	case "mark-bad":
		err = controller.MarkBad(ctx)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "synora-ota:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synora-ota {status|install|mark-good|mark-bad} [flags]")
}

func commandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
