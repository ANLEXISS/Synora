package main

import (
	"context"
	"log"
	"os"

	"synora/internal/bus"
	"synora/internal/cognitive"
)

func main() {
	busPath := getenv("SYNORA_BUS", "/run/synora/bus.sock")
	client, err := bus.NewClient(busPath, "cognitive")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	registry := cognitive.DefaultRegistry()
	modelDir := os.Getenv("SYNORA_COGNITIVE_MODEL_DIR")
	if modelDir != "" {
		backend, loadErr := cognitive.NewCPUMLPBackend(modelDir)
		if loadErr != nil {
			log.Printf("cognitive CPU backend unavailable; retaining mock backend: %v", loadErr)
		} else {
			registry = cognitive.RegistryWithBackend(backend)
			log.Printf("cognitive CPU backend loaded from %s", modelDir)
		}
	}
	service := &cognitive.Service{Bus: client, Scheduler: cognitive.NewScheduler(registry), Name: "cognitive"}
	if err := service.PublishStatus(); err != nil {
		log.Printf("cognitive startup status warning: %v", err)
	}
	log.Printf("synora cognitive service ready (dry-run=%s, advisory-only)", dryRunValue())
	if err := service.Run(context.Background()); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}

func dryRunValue() string {
	if value := os.Getenv(cognitive.CognitiveDryRunEnv); value != "" {
		return value
	}
	return "1 (fail-closed default)"
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
