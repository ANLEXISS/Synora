package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"synora/internal/version"
)

func main() {
	output := flag.String("output", "build/version.json", "output path")
	flag.Parse()
	if strings.TrimSpace(*output) == "" {
		fmt.Fprintln(os.Stderr, "output path is required")
		os.Exit(2)
	}
	manifest := generate()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode version manifest:", err)
		os.Exit(1)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(parent(*output), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "create version directory:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, data, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "write version manifest:", err)
		os.Exit(1)
	}
}

func generate() version.Manifest {
	commit := strings.TrimSpace(os.Getenv("SYNORA_GIT_COMMIT"))
	if commit == "" {
		commit = gitCommit()
	}
	if commit == "" {
		commit = "unknown"
	}
	image := envOr("SYNORA_IMAGE_VERSION", "0.0.0-dev")
	synora := envOr("SYNORA_VERSION", image)
	bundle := envOr("SYNORA_BUNDLE_ID", "local-"+commit)
	return version.Manifest{
		ImageVersion: image, SynoraVersion: synora, GitCommit: commit,
		BuildTime:           envOr("SYNORA_BUILD_TIME", time.Now().UTC().Format(time.RFC3339)),
		TargetBoard:         envOr("SYNORA_TARGET_BOARD", "rock-5-itx"),
		OSBase:              envOr("SYNORA_OS_BASE", "radxa-debian-bookworm"),
		KernelExpected:      envOr("SYNORA_KERNEL_EXPECTED", "6.1.43-26-rk2312"),
		RKNNRuntimeExpected: envOr("SYNORA_RKNN_RUNTIME_EXPECTED", "unknown"),
		ConfigSchemaVersion: 1,
		BundleID:            bundle,
	}
}

func gitCommit() string {
	command := exec.Command("git", "rev-parse", "--short", "HEAD")
	data, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parent(path string) string {
	index := strings.LastIndexAny(path, "/\\")
	if index < 0 {
		return "."
	}
	return path[:index]
}
