package main

import "testing"

func TestDiscoveryHealthURLNormalizesListenAddress(t *testing.T) {
	for input, want := range map[string]string{
		":8091":                  "http://127.0.0.1:8091/healthz",
		"127.0.0.1:8091":         "http://127.0.0.1:8091/healthz",
		"http://127.0.0.1:8091/": "http://127.0.0.1:8091/healthz",
	} {
		if got := discoveryHealthURL(input); got != want {
			t.Fatalf("discoveryHealthURL(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestFirstNetworkValueNeverReturnsNonStringSecretMaterial(t *testing.T) {
	values := map[string]any{"rtsp_url": "", "endpoint": "rtsp://10.0.0.8/live", "password": "do-not-use"}
	if got := firstNetworkValue(values, "rtsp_url", "endpoint"); got != "rtsp://10.0.0.8/live" {
		t.Fatalf("endpoint=%q", got)
	}
}
