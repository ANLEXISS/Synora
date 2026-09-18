package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
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
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: cognitivecore.UnavailableMLP{Reason: "no promoted full-snapshot V1 bundle"}, Gate: cognitivecore.SafetyGate{DryRun: true}}
	service := &cognitivecore.Service{Bus: client, Core: core, Name: "core"}
	log.Printf("synora core V1 ready mode=active_dry_run model=unavailable encoder_dimension=%d", cognitivecore.CognitiveVectorSize)
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
