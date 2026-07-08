package main

import (
	"log"

	"synora/internal/discovery"
)

func main() {
	if err := discovery.Run(); err != nil {
		log.Fatal(err)
	}
}
