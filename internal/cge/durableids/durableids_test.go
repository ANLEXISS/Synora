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

func TestProtectDoesNotKeepAProtectedTokenFromAnotherDomain(t *testing.T) {
	device := Protect(KindDevice, "shared-value")
	entity := Protect(KindEntity, device)
	if strings.HasPrefix(entity, "cgeid-v1:device:") {
		t.Fatalf("entity protection retained the device domain: %q", entity)
	}
}

func TestProtectDoesNotTrustAProtectedLookingRawValue(t *testing.T) {
	raw := "cgeid-v1:entity:" + strings.Repeat("a", 64)
	protected := ProtectRaw(KindEntity, raw)
	if protected == raw {
		t.Fatalf("protected-looking raw value was accepted unchanged: %q", protected)
	}
}

func TestProtectRawProperties(t *testing.T) {
	if ProtectRaw(KindEntity, "") != "" {
		t.Fatal("empty raw identifier was not preserved")
	}
	const raw = "raw-entity"
	value := ProtectRaw(KindEntity, raw)
	if value == raw || !IsProtectedFor(KindEntity, value) || ProtectRaw(KindEntity, raw) != value {
		t.Fatalf("raw protection is invalid or unstable: %q", value)
	}
	if strings.Contains(value, raw) || strings.ContainsAny(value, "\r\n") || len(value) > 128 {
		t.Fatalf("raw protection violates bounded ASCII format: %q", value)
	}
	if other := ProtectRaw(KindEntity, "raw-other"); other == value {
		t.Fatal("distinct raw values collided in the test")
	}
	if domain := ProtectRaw(KindDevice, raw); domain == value {
		t.Fatal("raw protection did not separate domains")
	}
}

func TestProtectCorrectsDomainAndRetainsOnlyTheRequestedDomain(t *testing.T) {
	device := Protect(KindDevice, "abc")
	entity := Protect(KindEntity, device)
	if !IsProtectedFor(KindEntity, entity) || IsProtectedFor(KindDevice, entity) {
		t.Fatalf("wrong-domain token was not corrected: %q", entity)
	}
	if Protect(KindEntity, entity) != entity {
		t.Fatal("same-domain protection was not idempotent")
	}
	if !IsProtected(device) || !IsProtectedFor(KindDevice, device) || IsProtectedFor(KindEntity, device) {
		t.Fatalf("domain-specific detection mismatch: %q", device)
	}
}

func TestProtectedFormatValidation(t *testing.T) {
	valid := ProtectRaw(KindEntity, "abc")
	values := map[string]bool{
		valid: true,
		"cgeid-v1:device:" + strings.Repeat("a", 64):  true,
		"cgeid-v1:entity:" + strings.Repeat("g", 64):  false,
		"cgeid-v2:entity:" + strings.Repeat("a", 64):  false,
		"cgeid-v1:invalid:" + strings.Repeat("a", 64): false,
		"cgeid-v1:entity:deadbeef":                    false,
		"cgeid-v1:entity:" + strings.Repeat("a", 63):  false,
		"cgeid-v1:entity:" + strings.Repeat("a", 65):  false,
	}
	for value, expected := range values {
		if got := IsProtected(value); got != expected {
			t.Errorf("IsProtected(%q) = %v, want %v", value, got, expected)
		}
	}
	if !IsProtectedFor(KindEntity, valid) || IsProtectedFor(KindDevice, valid) {
		t.Fatalf("IsProtectedFor did not enforce the exact domain: %q", valid)
	}
}
