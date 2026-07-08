package discovery

import (
	"log"
	"os"
	"time"

	"synora/internal/bus"
)

const DefaultBusSocket = "/run/synora/bus.sock"

func Run() error {
	if err := os.MkdirAll(
		VisionClipDir,
		0755,
	); err != nil {
		return err
	}

	manager := NewManager(
		connectBus(
			DefaultBusSocket,
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
