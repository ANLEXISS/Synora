package main

import (
	"fmt"
	"os"

	webapi "synora/internal/api"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "hash-password" {
		fmt.Fprintln(os.Stderr, "usage: synora-auth-tool hash-password PASSWORD")
		os.Exit(2)
	}
	hash, err := webapi.HashPassword(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
