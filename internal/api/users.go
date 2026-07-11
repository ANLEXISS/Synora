package api

import (
	"errors"
	"os"
	"sort"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

const (
	RoleAdmin    = "admin"
	RoleResident = "resident"
	RoleGuest    = "guest"

	DefaultAuthConfigPath = "/etc/synora/auth.yaml"
)

const (
	PermissionStateRead        = "state:read"
	PermissionDevicesRead      = "devices:read"
	PermissionDevicesWrite     = "devices:write"
	PermissionResidentsRead    = "residents:read"
	PermissionResidentsWrite   = "residents:write"
	PermissionTopologyRead     = "topology:read"
	PermissionTopologyWrite    = "topology:write"
	PermissionAutomationsRead  = "automations:read"
	PermissionAutomationsWrite = "automations:write"
	PermissionCGERead          = "cge:read"
	PermissionCGEWrite         = "cge:write"
	PermissionSimulationRun    = "simulation:run"
	PermissionLabUse           = "lab:use"
	PermissionVideoRead        = "video:read"
	PermissionSettingsRead     = "settings:read"
	PermissionSettingsWrite    = "settings:write"
	PermissionSecurityAdmin    = "security:admin"
)

type AuthUser struct {
	ID          string   `json:"id"`
	Login       string   `json:"login"`
	Role        string   `json:"role"`
	ResidentID  string   `json:"resident_id,omitempty"`
	Source      string   `json:"source"`
	Permissions []string `json:"permissions"`
}

func (u AuthUser) HasPermission(permission string) bool {
	permissions := u.Permissions
	if len(permissions) == 0 {
		// Compatibility for sessions created before RBAC claims existed.
		permissions = PermissionsForRole(u.Role)
	}
	for _, candidate := range permissions {
		if candidate == permission {
			return true
		}
	}
	return false
}

func AdminAuthUser() AuthUser {
	return AuthUser{
		ID:          "api-admin",
		Login:       "api-token",
		Role:        RoleAdmin,
		Source:      "local",
		Permissions: PermissionsForRole(RoleAdmin),
	}
}

func PermissionsForRole(role string) []string {
	var permissions []string
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleAdmin:
		permissions = []string{
			PermissionStateRead,
			PermissionDevicesRead, PermissionDevicesWrite,
			PermissionResidentsRead, PermissionResidentsWrite,
			PermissionTopologyRead, PermissionTopologyWrite,
			PermissionAutomationsRead, PermissionAutomationsWrite,
			PermissionCGERead, PermissionCGEWrite,
			PermissionSimulationRun, PermissionLabUse,
			PermissionVideoRead,
			PermissionSettingsRead, PermissionSettingsWrite,
			PermissionSecurityAdmin,
		}
	case RoleResident:
		permissions = []string{
			PermissionStateRead,
			PermissionDevicesRead,
			PermissionResidentsRead,
			PermissionTopologyRead,
			PermissionAutomationsRead,
			PermissionVideoRead,
		}
	case RoleGuest:
		permissions = []string{
			PermissionStateRead,
			PermissionTopologyRead,
		}
	}
	sort.Strings(permissions)
	return permissions
}

type authFile struct {
	Users []authFileUser `yaml:"users"`
}

type authFileUser struct {
	ID           string `yaml:"id"`
	Login        string `yaml:"login"`
	ResidentID   string `yaml:"resident_id,omitempty"`
	Role         string `yaml:"role"`
	Enabled      bool   `yaml:"enabled"`
	PasswordHash string `yaml:"password_hash"`
}

type UserDirectory struct {
	byID    map[string]authFileUser
	byLogin map[string]authFileUser
}

func NewUserDirectory() *UserDirectory {
	return &UserDirectory{
		byID:    make(map[string]authFileUser),
		byLogin: make(map[string]authFileUser),
	}
}

func LoadUserDirectory(path string) (*UserDirectory, error) {
	directory := NewUserDirectory()
	if strings.TrimSpace(path) == "" {
		path = DefaultAuthConfigPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return directory, nil
	}
	if err != nil {
		return nil, err
	}
	var file authFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	for _, raw := range file.Users {
		raw.ID = strings.TrimSpace(raw.ID)
		raw.Login = strings.ToLower(strings.TrimSpace(raw.Login))
		raw.ResidentID = strings.TrimSpace(raw.ResidentID)
		raw.Role = strings.ToLower(strings.TrimSpace(raw.Role))
		raw.PasswordHash = strings.TrimSpace(raw.PasswordHash)
		if raw.ID == "" || raw.Login == "" || raw.PasswordHash == "" {
			continue
		}
		if raw.Role != RoleAdmin && raw.Role != RoleResident && raw.Role != RoleGuest {
			continue
		}
		directory.byID[raw.ID] = raw
		directory.byLogin[raw.Login] = raw
	}
	return directory, nil
}

func (d *UserDirectory) Count() int {
	if d == nil {
		return 0
	}
	return len(d.byID)
}

func (d *UserDirectory) Authenticate(login, password string) (AuthUser, bool) {
	if d == nil {
		return AuthUser{}, false
	}
	record, ok := d.byLogin[strings.ToLower(strings.TrimSpace(login))]
	if !ok || !record.Enabled || bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)) != nil {
		return AuthUser{}, false
	}
	return publicAuthUser(record), true
}

func (d *UserDirectory) UserByID(id string) (AuthUser, bool) {
	if d == nil {
		return AuthUser{}, false
	}
	record, ok := d.byID[strings.TrimSpace(id)]
	if !ok || !record.Enabled {
		return AuthUser{}, false
	}
	return publicAuthUser(record), true
}

func publicAuthUser(record authFileUser) AuthUser {
	return AuthUser{
		ID:          record.ID,
		Login:       record.Login,
		Role:        record.Role,
		ResidentID:  record.ResidentID,
		Source:      "auth.yaml",
		Permissions: PermissionsForRole(record.Role),
	}
}

func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
