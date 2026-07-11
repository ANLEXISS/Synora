package main

import (
	"flag"
	"fmt"
	"log"

	"synora/internal/security"
)

func main() {
	path := flag.String("path", security.DefaultPath, "path to security.yaml")
	flag.Parse()

	if _, err := security.RotateAPIToken(*path); err != nil {
		log.Fatal(err)
	}
	// Deliberately do not print the generated token. Read it from the protected
	// security.yaml file when a CLI/service client needs to be updated.
	fmt.Println("Synora API token rotated")
}
