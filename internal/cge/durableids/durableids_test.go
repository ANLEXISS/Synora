package durableids

import (
	"strings"
	"testing"
)

func TestProtectProperties(t *testing.T) {
	const raw = "PASS64-SENSITIVE-IDENTITY"
	if Protect(KindEntity, "") != "" {
		t.Fatal("empty identifier was not preserved")
	}
	first := Protect(KindEntity, raw)
	if first == raw || !IsProtected(first) || Protect(KindEntity, raw) != first {
		t.Fatalf("unstable or invalid protected identifier: %q", first)
	}
	if strings.Contains(first, raw) || strings.ContainsAny(first, "\r\n") || len(first) > 128 {
		t.Fatalf("protected identifier violates bounded ASCII format: %q", first)
	}
	if other := Protect(KindEntity, "PASS64-SENSITIVE-OTHER"); other == first {
		t.Fatal("distinct values collided in the test")
	}
	if domain := Protect(KindDevice, raw); domain == first {
		t.Fatal("domains are not separated")
	}
	if Protect(KindEntity, first) != first {
		t.Fatal("protected identifier was protected twice")
	}
}

func TestProtectHasStableVersionedFormat(t *testing.T) {
	value := Protect(KindObservation, "event-1")
	if !strings.HasPrefix(value, "cgeid-v1:observation:") {
		t.Fatalf("unexpected format: %q", value)
	}
	if !IsProtected(value) || IsProtected("cgeid-v2:observation:deadbeef") {
		t.Fatalf("format detection mismatch")
	}
}
