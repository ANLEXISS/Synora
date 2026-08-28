package main

import (
	"log"
	"os"

	"synora/internal/bus"
	"synora/internal/runtimeconfig"
)

func main() {

	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	server := bus.NewServer(runtime.Paths.BusSocket)

	log.Println("starting synora bus")

	err = server.Start()
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
