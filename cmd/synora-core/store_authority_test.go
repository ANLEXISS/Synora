package main

import (
	"os"
	"strings"
	"testing"
)

func TestV1RuntimeUsesUniversalStoreAsTheOnlyCoreStore(t *testing.T) {
	body, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	if !strings.Contains(source, "cognitivecore.OpenUniversalStore") {
		t.Fatal("V1 Core no longer opens the authoritative UniversalStore")
	}
	if strings.Contains(source, "synora/internal/state") {
		t.Fatal("V1 Core must not import the legacy internal/state Store")
	}
}
