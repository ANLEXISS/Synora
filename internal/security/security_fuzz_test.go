package security

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func FuzzVerifyPairingProof(f *testing.F) {
	f.Add("camera-1", "setup", "1770000000", "aa:bb:cc:dd:ee:ff", "fingerprint", "not-a-signature")
	f.Fuzz(func(t *testing.T, deviceID, setupToken, timestamp, mac, fingerprint, signature string) {
		_ = VerifyPairingProof(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)), deviceID, setupToken, timestamp, mac, fingerprint, signature, time.Now().UTC(), time.Second)
	})
}

func FuzzIdentityRegistryLoad(f *testing.F) {
	f.Add([]byte(`{"version":1,"identities":{}}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "identities.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		_ = NewIdentityRegistry(path).Load()
	})
}
