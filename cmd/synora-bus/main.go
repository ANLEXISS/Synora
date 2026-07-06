package main

import (
	"log"

	"synora/internal/bus"
)

func main() {

	server := bus.NewServer("/run/synora/bus.sock")

	log.Println("starting synora bus")

	err := server.Start()
	if err != nil {
		log.Fatal(err)
	}
}
