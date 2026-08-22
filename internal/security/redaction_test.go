package security

import "testing"

func TestRedactSupportTextRemovesSecretsBiometricsAndPaths(t *testing.T) {
	input := `request token=abc123 path=/etc/synora/security.yaml face_profile="biometric-value" authorization: Bearer-secret`
	output := RedactSupportText(input)
	for _, forbidden := range []string{"abc123", "/etc/synora/security.yaml", "biometric-value", "Bearer-secret"} {
		if contains(output, forbidden) {
			t.Fatalf("support text retained %q: %s", forbidden, output)
		}
	}
	if !contains(output, "<redacted>") || !contains(output, "<path-redacted>") {
		t.Fatalf("redaction markers missing: %s", output)
	}
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
