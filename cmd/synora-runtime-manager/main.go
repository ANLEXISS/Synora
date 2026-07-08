package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"synora/internal/bus"
	"synora/internal/manager"
	"synora/pkg/contract"
)

func main() {
	busClient := connectBus(
		getenv(
			"SYNORA_BUS",
			"/run/synora/bus.sock",
		),
	)

	runtimeManager := manager.New(
		manager.Config{},
	)

	log.Println(
		"synora-runtime-manager started",
	)

	for msg := range busClient.SubscribeChannel(
		manager.ServiceRuntimeManager,
	) {
		if msg.Kind != contract.KindRPC && msg.Kind != contract.KindCommand {
			continue
		}

		go handleMessage(
			busClient,
			runtimeManager,
			msg,
		)
	}
}

func connectBus(
	socketPath string,
) *bus.Client {
	for {
		client, err := bus.NewClient(
			socketPath,
			manager.ServiceRuntimeManager,
		)

		if err == nil {
			log.Println(
				"connected to synora bus",
			)

			return client
		}

		log.Println(
			"bus not ready, retrying in 2s...",
			err,
		)

		time.Sleep(
			2 * time.Second,
		)
	}
}

func handleMessage(
	busClient *bus.Client,
	runtimeManager *manager.Manager,
	msg contract.Message,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	result, err := runtimeManager.Handle(
		ctx,
		msg,
	)

	response := contract.Message{
		ID:        msg.ID,
		Type:      msg.Type,
		Kind:      contract.KindRPC,
		Source:    manager.ServiceRuntimeManager,
		Target:    msg.Source,
		Timestamp: time.Now().UTC(),
	}

	if err != nil {
		response.Payload, _ = json.Marshal(
			map[string]any{
				"error": err.Error(),
			},
		)
	} else if result != nil {
		response.Payload, err = json.Marshal(
			result,
		)
		if err != nil {
			response.Payload, _ = json.Marshal(
				map[string]any{
					"error": err.Error(),
				},
			)
		}
	} else {
		response.Payload = []byte("{}")
	}

	if err := busClient.Send(
		response,
	); err != nil {
		log.Println(
			"runtime-manager response failed:",
			err,
		)
	}
}

func getenv(
	key string,
	fallback string,
) string {
	value := os.Getenv(
		key,
	)
	if value == "" {
		return fallback
	}

	return value
}
