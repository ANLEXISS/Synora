package discovery

import (
	"log"
	"os"
	"time"

	"synora/internal/bus"
	"synora/internal/runtimeconfig"
)

const DefaultBusSocket = runtimeconfig.DefaultBusSocket

func Run() error {
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

	manager := NewManager(
		connectBus(
			runtime.Paths.BusSocket,
		),
	)

	manager.Start()

	select {}
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
