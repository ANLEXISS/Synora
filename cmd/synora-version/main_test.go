package main

import "testing"

func TestBuildTimeUsesReproducibleInputs(t *testing.T) {
	t.Setenv("SYNORA_BUILD_TIME", "")
	t.Setenv("SOURCE_DATE_EPOCH", "0")
	if got := buildTime(); got != "1970-01-01T00:00:00Z" {
		t.Fatalf("epoch build time=%q", got)
	}
	t.Setenv("SYNORA_BUILD_TIME", "2026-08-29T00:00:00Z")
	if got := buildTime(); got != "2026-08-29T00:00:00Z" {
		t.Fatalf("explicit build time=%q", got)
	}
	t.Setenv("SYNORA_BUILD_TIME", "")
	t.Setenv("SOURCE_DATE_EPOCH", "not-a-number")
	if got := buildTime(); got != "unknown" {
		t.Fatalf("uncontrolled build time=%q", got)
	}
}
