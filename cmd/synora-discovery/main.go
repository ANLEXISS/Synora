package main

import (
	"log"
	"os"
	"time"

	"synora/internal/bus"
	"synora/internal/discovery"
)

func main() {

	if err := os.MkdirAll(
		discovery.VisionClipDir,
		0755,
	); err != nil {

		log.Fatal(err)
	}

	busClient := connectBus()

	manager := discovery.NewManager(
		busClient,
	)

	manager.Start()

	select {}
}

func connectBus() *bus.Client {

	for {

		client, err := bus.NewClient(
			"/run/synora/bus.sock",
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