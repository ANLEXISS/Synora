package connectivity

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzLoadConnectivityConfig(f *testing.F) {
	f.Add([]byte("version: 1\nenabled: false\n"))
	f.Add([]byte("version: 1\ninterface:\n  name: synora0\n"))
	f.Add([]byte("not: [yaml"))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "connectivity.yaml")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		_, _ = Load(path)
	})
}
