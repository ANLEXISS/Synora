package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"synora/internal/ota"
	"synora/internal/runtimeconfig"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	runtime, runtimeErr := runtimeconfig.Load(os.Getenv)
	if runtimeErr != nil {
		fmt.Fprintln(os.Stderr, "synora-ota: invalid runtime configuration:", runtimeErr)
		os.Exit(1)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	rauc := fs.String("rauc", envOr("SYNORA_RAUC_BIN", "rauc"), "RAUC executable")
	healthcheck := fs.String("healthcheck", envOr("SYNORA_BOOT_HEALTHCHECK", "/opt/synora/bin/synora-boot-healthcheck"), "post-boot healthcheck executable")
	manifest := fs.String("manifest", envOr("SYNORA_OTA_MANIFEST", ""), "detached signed OTA manifest")
	publicKeyPath := fs.String("public-key", envOr("SYNORA_OTA_PUBLIC_KEY", ""), "Ed25519 public key file")
	currentVersion := fs.String("current-version", envOr("SYNORA_CORE_VERSION", "0.0.0"), "currently running Core version")
	currentMigration := fs.Int("current-migration", envOrInt("SYNORA_MIGRATION_SCHEMA", 0), "currently applied configuration migration schema")
	currentGeneration := fs.Uint64("current-security-generation", envOrUint64("SYNORA_OTA_SECURITY_GENERATION", 0), "persisted anti-downgrade generation")
	hardware := fs.String("hardware", envOr("SYNORA_HARDWARE", "rock-5b"), "hardware compatibility identifier")
	target := fs.String("target", envOr("SYNORA_OTA_TARGET", ota.TargetCentral), "release target: central or camera")
	productID := fs.String("product-id", envOr("SYNORA_OTA_PRODUCT_ID", "synora-central"), "release product identifier")
	rootsPath := fs.String("release-roots", envOr("SYNORA_OTA_RELEASE_ROOTS", ""), "PEM release root CA bundle")
	intermediatesPath := fs.String("release-intermediates", envOr("SYNORA_OTA_RELEASE_INTERMEDIATES", ""), "PEM release intermediate CA bundle")
	crlPath := fs.String("release-crl", envOr("SYNORA_OTA_RELEASE_CRL", ""), "PEM release CRL bundle")
	migrationPath := fs.String("migration-path", envOr("SYNORA_MIGRATION_PATH", ""), "persistent configuration path to migrate before readiness")
	journal := fs.String("journal", runtime.Paths.OTAJournal, "OTA transaction journal")
	stabilityWindow := fs.Duration("stability-window", 120*time.Second, "required healthy post-boot window")
	timeout := fs.Duration("timeout", 5*time.Minute, "operation timeout")
	_ = fs.Parse(os.Args[2:])
	controller := ota.NewController(commandRunner, *rauc, *healthcheck)
	controller.SetJournalPath(*journal)
	controller.SetMigration(*migrationPath, *currentMigration)
	controller.SetStabilityWindow(*stabilityWindow)
	if *manifest != "" || *publicKeyPath != "" || *rootsPath != "" {
		verification := ota.Verification{ManifestPath: *manifest, CurrentVersion: *currentVersion, CurrentSecurityGeneration: *currentGeneration, Hardware: *hardware, CurrentMigration: *currentMigration}
		if *rootsPath != "" {
			roots, rootsErr := os.ReadFile(*rootsPath)
			intermediates, intermediatesErr := readOptional(*intermediatesPath)
			crl, crlErr := readOptional(*crlPath)
			if rootsErr != nil || intermediatesErr != nil || crlErr != nil {
				fmt.Fprintln(os.Stderr, "synora-ota: invalid release trust files")
				os.Exit(1)
			}
			pki, pkiErr := ota.NewReleasePKI(*target, *productID, roots, intermediates, crl)
			if pkiErr != nil {
				fmt.Fprintln(os.Stderr, "synora-ota: invalid release trust profile:", pkiErr)
				os.Exit(1)
			}
			verification.ReleasePKI = pki
		} else {
			key, keyErr := os.ReadFile(*publicKeyPath)
			if keyErr != nil || len(key) != ed25519.PublicKeySize {
				fmt.Fprintln(os.Stderr, "synora-ota: invalid public key")
				os.Exit(1)
			}
			verification.PublicKey = ed25519.PublicKey(key)
		}
		controller.SetVerification(verification)
	}
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
	case "apply":
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: synora-ota apply [flags] BUNDLE")
			os.Exit(2)
		}
		if *manifest == "" || (*publicKeyPath == "" && *rootsPath == "") {
			err = fmt.Errorf("apply requires --manifest and a release trust profile")
		} else {
			err = controller.Apply(ctx, fs.Arg(0))
		}
	case "recover":
		err = controller.Recover(ctx)
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
	fmt.Fprintln(os.Stderr, "usage: synora-ota {status|install|apply|recover|mark-good|mark-bad} [flags]")
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

func envOrInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envOrUint64(name string, fallback uint64) uint64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func readOptional(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}
