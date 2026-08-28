package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"synora/internal/bus"
	"synora/internal/manager"
	"synora/internal/runtimeconfig"
	"synora/pkg/contract"
)

func main() {
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	busClient := connectBus(
		runtime.Paths.BusSocket,
		runtime.Timeouts.BusConnect,
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
			runtime.Timeouts.BusRPC,
		)
	}
}

func connectBus(
	socketPath string,
	retryDelay time.Duration,
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

		time.Sleep(retryDelay)
	}
}

func handleMessage(
	busClient *bus.Client,
	runtimeManager *manager.Manager,
	msg contract.Message,
	timeout time.Duration,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		timeout,
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
