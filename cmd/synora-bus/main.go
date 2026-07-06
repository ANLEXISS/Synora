package main

import (
	"log"
	"os"

	"synora/internal/bus"
)

func main() {

	server := bus.NewServer(getenv("SYNORA_BUS", "/run/synora/bus.sock"))

	log.Println("starting synora bus")

	err := server.Start()
	if err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
