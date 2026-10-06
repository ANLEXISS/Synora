package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAccountsAndRoleRevalidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte("users:\n  - id: user_guest\n    login: guest\n    role: guest\n    enabled: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	accounts, err := LoadAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	if !AccountAllows(accounts, "user_guest", "guest") || !AccountAllows(accounts, "guest", "guest") {
		t.Fatal("enabled account was not accepted by id and login")
	}
	if AccountAllows(accounts, "user_guest", "admin") {
		t.Fatal("role escalation was accepted")
	}
	accounts["user_guest"] = Account{ID: "user_guest", Login: "guest", Role: "admin", Enabled: false}
	if AccountAllows(accounts, "user_guest", "guest") {
		t.Fatal("disabled or role-changed account remained accepted")
	}
}

func TestLoadAccountsRejectsAmbiguousAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(path, []byte("users:\n  - id: user_a\n    login: shared\n    role: guest\n    enabled: true\n  - id: user_b\n    login: shared\n    role: resident\n    enabled: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccounts(path); err == nil {
		t.Fatal("ambiguous account alias was accepted")
	}
}
