package api

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

const (
	RoleAdmin    = "admin"
	RoleResident = "resident"
	RoleGuest    = "guest"

	DefaultAuthConfigPath = "/etc/synora/auth.yaml"
)

var (
	ErrAccountExists      = errors.New("account_exists")
	ErrAccountNotFound    = errors.New("account_not_found")
	ErrInvalidAccount     = errors.New("invalid_account")
	ErrInvalidRole        = errors.New("invalid_role")
	ErrInvalidPassword    = errors.New("invalid_password")
	ErrPasswordMismatch   = errors.New("password_mismatch")
	ErrLastAdmin          = errors.New("last_admin")
	ErrBootstrapCompleted = errors.New("bootstrap_completed")
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
			PermissionCGERead,
			PermissionDevicesRead,
			PermissionResidentsRead,
			PermissionTopologyRead,
			PermissionAutomationsRead,
			PermissionVideoRead,
		}
	case RoleGuest:
		permissions = []string{
			PermissionStateRead,
			PermissionCGERead,
			PermissionDevicesRead,
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
	path    string
	mu      sync.RWMutex
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
	directory.path = filepath.Clean(path)
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
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.byID)
}

func (d *UserDirectory) AdminCount() int {
	if d == nil {
		return 0
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for _, user := range d.byID {
		if user.Enabled && user.Role == RoleAdmin {
			count++
		}
	}
	return count
}

func (d *UserDirectory) PublicUsers() []AuthUser {
	if d == nil {
		return []AuthUser{}
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	users := make([]AuthUser, 0, len(d.byID))
	for _, user := range d.byID {
		users = append(users, publicAuthUser(user))
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users
}

func (d *UserDirectory) Authenticate(login, password string) (AuthUser, bool) {
	if d == nil {
		return AuthUser{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
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
	d.mu.RLock()
	defer d.mu.RUnlock()
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

// BootstrapAdmin creates the first local administrator exactly once. It is
// intentionally owned by the dedicated auth file and never touches resident
// configuration.
func (d *UserDirectory) BootstrapAdmin(login, password string) (AuthUser, error) {
	if d == nil {
		return AuthUser{}, ErrInvalidAccount
	}
	login = normalizeLogin(login)
	if err := validateAccountInput(login, password); err != nil {
		return AuthUser{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return AuthUser{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, user := range d.byID {
		if user.Enabled && user.Role == RoleAdmin {
			return AuthUser{}, ErrBootstrapCompleted
		}
	}
	if _, exists := d.byLogin[login]; exists {
		return AuthUser{}, ErrAccountExists
	}
	user := authFileUser{ID: newAccountID(login), Login: login, Role: RoleAdmin, Enabled: true, PasswordHash: hash}
	d.byID[user.ID] = user
	d.byLogin[user.Login] = user
	if err := d.saveLocked(); err != nil {
		delete(d.byID, user.ID)
		delete(d.byLogin, user.Login)
		return AuthUser{}, err
	}
	return publicAuthUser(user), nil
}

func (d *UserDirectory) ChangePassword(id, currentPassword, newPassword string) error {
	if d == nil {
		return ErrAccountNotFound
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	user, ok := d.byID[strings.TrimSpace(id)]
	if !ok || !user.Enabled {
		return ErrAccountNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return ErrPasswordMismatch
	}
	oldUser := user
	user.PasswordHash = hash
	d.byID[user.ID] = user
	d.byLogin[user.Login] = user
	if err := d.saveLocked(); err != nil {
		// Keep the in-memory directory aligned with the durable file when the
		// atomic replacement cannot complete.
		d.byID[user.ID] = oldUser
		d.byLogin[user.Login] = oldUser
		return err
	}
	return nil
}

func (d *UserDirectory) CreateUser(login, role, password string, enabled bool) (AuthUser, error) {
	if d == nil {
		return AuthUser{}, ErrInvalidAccount
	}
	login = normalizeLogin(login)
	role = normalizeRole(role)
	if err := validateAccountInput(login, password); err != nil {
		return AuthUser{}, err
	}
	if !validRole(role) {
		return AuthUser{}, ErrInvalidRole
	}
	hash, err := HashPassword(password)
	if err != nil {
		return AuthUser{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.byLogin[login]; exists {
		return AuthUser{}, ErrAccountExists
	}
	user := authFileUser{ID: newAccountID(login), Login: login, Role: role, Enabled: enabled, PasswordHash: hash}
	d.byID[user.ID] = user
	d.byLogin[user.Login] = user
	if err := d.saveLocked(); err != nil {
		delete(d.byID, user.ID)
		delete(d.byLogin, user.Login)
		return AuthUser{}, err
	}
	return publicAuthUser(user), nil
}

func (d *UserDirectory) UpdateUser(id string, role *string, enabled *bool, password *string) (AuthUser, error) {
	if d == nil {
		return AuthUser{}, ErrAccountNotFound
	}
	id = strings.TrimSpace(id)
	d.mu.Lock()
	defer d.mu.Unlock()
	user, ok := d.byID[id]
	if !ok {
		return AuthUser{}, ErrAccountNotFound
	}
	updated := user
	if role != nil {
		updated.Role = normalizeRole(*role)
		if !validRole(updated.Role) {
			return AuthUser{}, ErrInvalidRole
		}
	}
	if enabled != nil {
		updated.Enabled = *enabled
	}
	if password != nil {
		if err := validatePassword(*password); err != nil {
			return AuthUser{}, err
		}
		hash, err := HashPassword(*password)
		if err != nil {
			return AuthUser{}, err
		}
		updated.PasswordHash = hash
	}
	if user.Enabled && user.Role == RoleAdmin && (!updated.Enabled || updated.Role != RoleAdmin) && d.enabledAdminCountLocked() <= 1 {
		return AuthUser{}, ErrLastAdmin
	}
	d.byID[id] = updated
	d.byLogin[updated.Login] = updated
	if err := d.saveLocked(); err != nil {
		d.byID[id] = user
		d.byLogin[user.Login] = user
		return AuthUser{}, err
	}
	return publicAuthUser(updated), nil
}

func (d *UserDirectory) DeleteUser(id string) error {
	if d == nil {
		return ErrAccountNotFound
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	id = strings.TrimSpace(id)
	user, ok := d.byID[id]
	if !ok {
		return ErrAccountNotFound
	}
	if user.Enabled && user.Role == RoleAdmin && d.enabledAdminCountLocked() <= 1 {
		return ErrLastAdmin
	}
	delete(d.byID, id)
	delete(d.byLogin, user.Login)
	if err := d.saveLocked(); err != nil {
		d.byID[id] = user
		d.byLogin[user.Login] = user
		return err
	}
	return nil
}

func (d *UserDirectory) enabledAdminCountLocked() int {
	count := 0
	for _, user := range d.byID {
		if user.Enabled && user.Role == RoleAdmin {
			count++
		}
	}
	return count
}

func (d *UserDirectory) saveLocked() error {
	if strings.TrimSpace(d.path) == "" {
		return ErrInvalidAccount
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(d.path), 0700); err != nil {
		return err
	}
	users := make([]authFileUser, 0, len(d.byID))
	for _, user := range d.byID {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	payload, err := yaml.Marshal(authFile{Users: users})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".auth-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, d.path); err != nil {
		return err
	}
	return os.Chmod(d.path, 0640)
}

func normalizeLogin(login string) string { return strings.ToLower(strings.TrimSpace(login)) }

func normalizeRole(role string) string { return strings.ToLower(strings.TrimSpace(role)) }

func validRole(role string) bool {
	return role == RoleAdmin || role == RoleResident || role == RoleGuest
}

func validateAccountInput(login, password string) error {
	if login == "" || strings.ContainsAny(login, " \t\r\n:/") {
		return ErrInvalidAccount
	}
	return validatePassword(password)
}

func validatePassword(password string) error {
	if len([]byte(password)) < 8 {
		return ErrInvalidPassword
	}
	return nil
}

func newAccountID(login string) string {
	return "user_" + strings.ReplaceAll(login, "-", "_")
}
