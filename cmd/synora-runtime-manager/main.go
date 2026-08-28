package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"synora/internal/bus"
	"synora/internal/manager"
	"synora/internal/runtimeconfig"
	"synora/pkg/contract"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	busClient, err := connectBusContext(ctx,
		runtime.Paths.BusSocket,
		runtime.Timeouts.BusConnect,
	)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("runtime-manager bus connection stopped: %v", err)
		}
		return
	}
	defer busClient.Close()

	runtimeManager := manager.New(
		manager.Config{},
	)

	log.Println(
		"synora-runtime-manager started",
	)

	messages := busClient.SubscribeChannel(
		manager.ServiceRuntimeManager,
	)
	var workers sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return
		case msg, ok := <-messages:
			if !ok {
				workers.Wait()
				return
			}
			if msg.Kind != contract.KindRPC && msg.Kind != contract.KindCommand {
				continue
			}

			workers.Add(1)
			go func(message contract.Message) {
				defer workers.Done()
				handleMessage(ctx, busClient, runtimeManager, message, runtime.Timeouts.BusRPC)
			}(msg)
		}
	}
}

func connectBus(
	socketPath string,
	retryDelay time.Duration,
) *bus.Client {
	client, _ := connectBusContext(context.Background(), socketPath, retryDelay)
	return client
}

func connectBusContext(
	ctx context.Context,
	socketPath string,
	retryDelay time.Duration,
) (*bus.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		client, err := bus.NewClient(
			socketPath,
			manager.ServiceRuntimeManager,
		)

		if err == nil {
			log.Println(
				"connected to synora bus",
			)

			return client, nil
		}

		log.Println(
			"bus not ready, retrying in 2s...",
			err,
		)

		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func handleMessage(
	parent context.Context,
	busClient *bus.Client,
	runtimeManager *manager.Manager,
	msg contract.Message,
	timeout time.Duration,
) {
	ctx, cancel := context.WithTimeout(
		parent,
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
