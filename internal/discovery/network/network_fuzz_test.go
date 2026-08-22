package network

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzLoadNetworkConfig(f *testing.F) {
	f.Add([]byte("synoranet:\n  enabled: false\n"))
	f.Add([]byte("synoranet:\n  pairing:\n    window_seconds: 1\n"))
	f.Add([]byte("not: [yaml"))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "network.yaml")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		_, _ = LoadConfig(path)
	})
}
