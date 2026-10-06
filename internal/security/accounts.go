package security

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Account is the server-side identity used to revalidate a web session. The
// password hash is intentionally not part of this read model: bootstrap and
// password management own that secret, while the API only needs the account's
// enabled state and current role.
type Account struct {
	ID      string `yaml:"id"`
	Login   string `yaml:"login"`
	Role    string `yaml:"role"`
	Enabled bool   `yaml:"enabled"`
}

type accountFile struct {
	Users []Account `yaml:"users"`
}

// LoadAccounts reads the account authority and rejects malformed or
// ambiguous identities. An absent/invalid file must never silently turn into
// an authorization success.
func LoadAccounts(path string) (map[string]Account, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("account file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var disk accountFile
	if err := yaml.Unmarshal(data, &disk); err != nil {
		return nil, fmt.Errorf("account file: %w", err)
	}
	accounts := make(map[string]Account, len(disk.Users))
	aliases := make(map[string]string, len(disk.Users)*2)
	for index, account := range disk.Users {
		account.ID = strings.TrimSpace(account.ID)
		account.Login = strings.TrimSpace(account.Login)
		account.Role = strings.ToLower(strings.TrimSpace(account.Role))
		if account.ID == "" || account.Login == "" {
			return nil, fmt.Errorf("account %d requires id and login", index)
		}
		if !validSessionRole(account.Role) {
			return nil, fmt.Errorf("account %q has unsupported role %q", account.ID, account.Role)
		}
		if _, exists := accounts[account.ID]; exists {
			return nil, fmt.Errorf("duplicate account id %q", account.ID)
		}
		for _, alias := range []string{account.ID, account.Login} {
			if previous, exists := aliases[alias]; exists && previous != account.ID {
				return nil, fmt.Errorf("account alias %q is ambiguous", alias)
			}
			aliases[alias] = account.ID
		}
		accounts[account.ID] = account
	}
	return accounts, nil
}

// AccountAllows reports whether the subject still names an enabled account
// with the role carried by a session. Both stable id and login are accepted
// as session subjects for compatibility with the bootstrap format.
func AccountAllows(accounts map[string]Account, subject, role string) bool {
	subject = strings.TrimSpace(subject)
	role = strings.ToLower(strings.TrimSpace(role))
	if subject == "" || !validSessionRole(role) {
		return false
	}
	for _, account := range accounts {
		if !account.Enabled || account.Role != role {
			continue
		}
		if account.ID == subject || account.Login == subject {
			return true
		}
	}
	return false
}
