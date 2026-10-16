// synora-oem-preflight validates a versioned OEM profile and prints a redacted
// capability report. It performs local inspection only; it never loads models
// or contacts external services.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"

	"synora/internal/oemprofile"
)

func main() {
	profilePath := flag.String("profile", "", "versioned OEM profile path")
	flag.Parse()
	if *profilePath == "" {
		fmt.Fprintln(os.Stderr, "--profile is required")
		os.Exit(2)
	}
	clean, err := oemprofile.ResolveProfilePath(*profilePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	p, body, err := oemprofile.Load(clean)
	if err != nil {
		fmt.Fprintln(os.Stderr, "OEM preflight rejected:", err)
		os.Exit(1)
	}
	obs := oemprofile.ObserveHost()
	obs.Architecture = runtime.GOARCH
	report := oemprofile.Preflight(p, body, obs)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if report.PreflightStatus == "blocked" {
		os.Exit(1)
	}
}
