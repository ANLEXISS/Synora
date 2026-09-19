package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"synora/internal/bus"
	"synora/internal/cognitivecore"
	"synora/internal/runtimeconfig"
)

// Core V1 is deliberately small at the process boundary. All decision state
// and writes are owned by cognitivecore; Discovery is reached only through
// versioned bus messages after a Store commit.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	client, err := bus.ConnectContext(ctx, runtime.Paths.BusSocket, "core")
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Fatal(err)
		}
		return
	}
	defer client.Close()
	storeDir := os.Getenv("SYNORA_STORE_DIR")
	if storeDir == "" {
		storeDir = filepath.Join(filepath.Dir(runtime.Paths.State), "universal-store")
	}
	store, err := cognitivecore.OpenUniversalStore(storeDir)
	if err != nil {
		log.Fatal("universal store unavailable; refusing to start Core: ", err)
	}
	var mlp cognitivecore.MLPBackend = cognitivecore.UnavailableMLP{Reason: "no promoted full-snapshot V1 bundle"}
	if bundleDir := os.Getenv("SYNORA_COGNITIVE_BUNDLE"); bundleDir != "" {
		loaded, loadErr := cognitivecore.LoadCPUBundle(bundleDir)
		if loadErr != nil {
			log.Printf("V1 MLP bundle unavailable; continuing fail-closed: %v", loadErr)
		} else {
			mlp = loaded
			log.Printf("V1 MLP bundle loaded path=%s dimension=%d heads=%v", bundleDir, cognitivecore.CognitiveVectorSize, cognitivecore.HeadOrder)
		}
	}
	core := &cognitivecore.Core{Store: store, MLP: mlp, Gate: cognitivecore.SafetyGate{DryRun: true}}
	service := &cognitivecore.Service{Bus: client, Core: core, Name: "core"}
	log.Printf("synora core V1 ready mode=active_dry_run model=unavailable encoder_dimension=%d", cognitivecore.CognitiveVectorSize)
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
