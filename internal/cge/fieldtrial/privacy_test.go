package fieldtrial

import (
	"testing"
)

func TestPseudonymizationIsSessionScoped(t *testing.T) {
	key := []byte("fixed-test-key-with-sufficient-length")
	first, err := NewPseudonymizer("cge-trial-a", key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPseudonymizer("cge-trial-b", key)
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref("chain", "raw-chain") != first.Ref("chain", "raw-chain") {
		t.Fatal("unstable reference")
	}
	if first.Ref("chain", "raw-chain") == second.Ref("chain", "raw-chain") {
		t.Fatal("cross-session reference reused")
	}
	if len(first.Ref("chain", "raw-chain")) != len("trial-ref-")+36 {
		t.Fatal("unexpected reference format")
	}
}
