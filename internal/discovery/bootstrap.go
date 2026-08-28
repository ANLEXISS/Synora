package discovery

import (
	"context"
	"log"
	"os"
	"time"

	"synora/internal/bus"
	"synora/internal/runtimeconfig"
)

const DefaultBusSocket = runtimeconfig.DefaultBusSocket

func Run() error {
	return RunContext(context.Background())
}

func RunContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(
		runtime.Paths.ClipRoot,
		0755,
	); err != nil {
		return err
	}

	busClient, err := connectBusContext(ctx,
		runtime.Paths.BusSocket,
	)
	if err != nil {
		return err
	}
	manager := NewManager(
		busClient,
	)

	manager.StartContext(ctx)
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), runtime.Timeouts.Shutdown)
	defer cancel()
	return manager.Close(shutdownCtx)
}

func connectBusContext(ctx context.Context, socketPath string) (*bus.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		client, err := bus.NewClient(socketPath, "discovery")
		if err == nil {
			log.Println("connected to synora bus")
			return client, nil
		}
		log.Println("bus not ready, retrying in 2s...", err)
		timer := time.NewTimer(2 * time.Second)
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

func connectBus(
	socketPath string,
) *bus.Client {
	for {
		client, err := bus.NewClient(
			socketPath,
			"discovery",
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

		time.Sleep(2 * time.Second)
	}
}
