package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"synora/internal/security"
)

const (
	defaultEtc       = "/etc/synora"
	defaultTemplates = "configs"
)

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type options struct {
	etc       string
	templates string
	apply     bool
	template  bool
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	command := os.Args[1]
	fs := flag.NewFlagSet(command, flag.ExitOnError)
	etc := fs.String("etc", defaultEtc, "runtime configuration directory")
	templates := fs.String("templates", templateDir(), "versioned configuration templates")
	apply := fs.Bool("apply", false, "write changes; without this flag the command is a dry-run")
	dryRun := fs.Bool("dry-run", false, "explicitly keep the command read-only")
	template := fs.Bool("template", false, "validate versioned templates instead of runtime files")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	opts := options{etc: filepath.Clean(*etc), templates: filepath.Clean(*templates), apply: *apply && !*dryRun, template: *template}

	var err error
	switch command {
	case "plan":
		err = plan(opts)
	case "validate":
		err = validate(opts)
	case "init":
		err = initConfig(opts)
	case "rotate-api-token":
		err = rotateAPIToken(opts)
	case "rotate-synoranet-psk":
		err = rotateSynoraNetPSK(opts)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "synora-bootstrap-config:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("usage: synora-bootstrap-config {plan|validate|init|rotate-api-token|rotate-synoranet-psk} [--etc DIR] [--templates DIR] [--apply|--dry-run]")
}

func templateDir() string {
	if _, err := os.Stat(defaultTemplates); err == nil {
		return defaultTemplates
	}
	return "/opt/synora/config-templates"
}

func plan(opts options) error {
	fmt.Printf("Synora bootstrap plan (read-only)\netc=%s templates=%s\n", opts.etc, opts.templates)
	for _, item := range []struct {
		name string
		mode os.FileMode
		note string
	}{
		{"security.yaml", 0640, "generated API hash and feature policy"},
		{"auth.yaml", 0640, "generated initial admin hash"},
		{"network.yaml", 0640, "safe network template; PSK is external"},
		{"devices.yaml", 0640, "safe device registry template; pairing secrets are local"},
		{"secrets/api_token", 0600, "generated locally; value never printed"},
		{"secrets/session_secret", 0640, "generated locally; readable by synora-api"},
		{"secrets/synoranet_psk", 0600, "generated locally; value never printed"},
		{"secrets/admin_initial_password", 0600, "generated locally; value never printed"},
	} {
		path := filepath.Join(opts.etc, item.name)
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("preserve %-42s mode=%04o note=%s\n", path, item.mode.Perm(), item.note)
		} else if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("create   %-42s mode=%04o note=%s\n", path, item.mode.Perm(), item.note)
		} else {
			return err
		}
	}
	fmt.Println("No secrets are read or displayed. Use init --apply only during controlled first boot.")
	return nil
}

func initConfig(opts options) error {
	if !opts.apply {
		fmt.Println("init dry-run: no files will be modified")
		return plan(opts)
	}
	if err := ensureRuntimeDirs(opts.etc); err != nil {
		return err
	}
	if err := ensureSecretsDir(filepath.Join(opts.etc, "secrets")); err != nil {
		return err
	}

	apiToken, err := ensureGeneratedSecret(filepath.Join(opts.etc, "secrets/api_token"), 32, 0600)
	if err != nil {
		return err
	}
	_, err = ensureGeneratedSecret(filepath.Join(opts.etc, "secrets/session_secret"), 32, 0640)
	if err != nil {
		return err
	}
	_, err = ensureGeneratedSecret(filepath.Join(opts.etc, "secrets/synoranet_psk"), 32, 0600)
	if err != nil {
		return err
	}
	adminPassword, err := ensureGeneratedSecret(filepath.Join(opts.etc, "secrets/admin_initial_password"), 32, 0600)
	if err != nil {
		return err
	}
	adminHash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	securityData, err := loadYAMLTemplate(filepath.Join(opts.templates, "security.yaml.template"), filepath.Join(opts.templates, "security.yaml"))
	if err != nil {
		return err
	}
	securityData["api_token_hash"] = security.HashSecret(apiToken)
	securityData["session_secret_file"] = filepath.Join(opts.etc, "secrets/session_secret")
	if err := writeYAMLIfMissing(filepath.Join(opts.etc, "security.yaml"), securityData, 0640); err != nil {
		return err
	}
	if err := secureConfigMetadata(filepath.Join(opts.etc, "security.yaml")); err != nil {
		return err
	}

	authData, err := loadYAMLTemplate(filepath.Join(opts.templates, "auth.yaml.template"), filepath.Join(opts.templates, "auth.yaml"))
	if err != nil {
		return err
	}
	authData["users"] = []map[string]any{{
		"id": "user_admin", "login": "admin", "role": "admin", "enabled": true,
		"password_hash": string(adminHash),
	}}
	if err := writeYAMLIfMissing(filepath.Join(opts.etc, "auth.yaml"), authData, 0640); err != nil {
		return err
	}
	if err := secureConfigMetadata(filepath.Join(opts.etc, "auth.yaml")); err != nil {
		return err
	}

	networkData, err := loadYAMLTemplate(filepath.Join(opts.templates, "network.yaml.template"), filepath.Join(opts.templates, "network.yaml"))
	if err != nil {
		return err
	}
	setNestedString(networkData, filepath.Join(opts.etc, "secrets/synoranet_psk"), "synoranet", "ap", "passphrase_file")
	if err := writeYAMLIfMissing(filepath.Join(opts.etc, "network.yaml"), networkData, 0640); err != nil {
		return err
	}
	if err := secureConfigMetadata(filepath.Join(opts.etc, "network.yaml")); err != nil {
		return err
	}
	if err := copyIfMissing(filepath.Join(opts.templates, "devices.yaml"), filepath.Join(opts.etc, "devices.yaml"), 0640); err != nil {
		return err
	}
	if err := secureConfigMetadata(filepath.Join(opts.etc, "devices.yaml")); err != nil {
		return err
	}
	fmt.Printf("bootstrap init applied to %s; generated secrets were written with restricted permissions\n", opts.etc)
	return nil
}

func validate(opts options) error {
	if opts.template {
		return validateTemplates(opts.templates)
	}
	var problems []string
	securityPath := filepath.Join(opts.etc, "security.yaml")
	securityData, err := readYAML(securityPath)
	if err != nil {
		problems = append(problems, "security.yaml is missing or invalid")
	} else {
		if err := validateMode(securityPath, 0640, 0600); err != nil {
			problems = append(problems, err.Error())
		}
		hash, _ := securityData["api_token_hash"].(string)
		if !hashPattern.MatchString(strings.TrimSpace(hash)) {
			problems = append(problems, "security.yaml api_token_hash is missing or still a placeholder")
		}
		if _, ok := securityData["api_token"]; ok {
			problems = append(problems, "security.yaml must not contain plaintext api_token")
		}
		features, _ := securityData["features"].(map[string]any)
		if boolValue(features, "debug_endpoints_enabled", true) || boolValue(features, "dev_simulation_enabled", true) {
			problems = append(problems, "debug endpoints and developer simulation must be disabled")
		}
		secretPath := stringValue(securityData, "session_secret_file", security.DefaultSessionSecretPath)
		if err := validateSecretFile(secretPath); err != nil {
			problems = append(problems, err.Error())
		}
		if err := validateSecretFile(filepath.Join(opts.etc, "secrets/api_token")); err != nil {
			problems = append(problems, err.Error())
		}
	}

	authPath := filepath.Join(opts.etc, "auth.yaml")
	if authData, readErr := readYAML(authPath); readErr != nil {
		problems = append(problems, "auth.yaml is missing or invalid")
	} else {
		if err := validateMode(authPath, 0640, 0600); err != nil {
			problems = append(problems, err.Error())
		}
		users, _ := authData["users"].([]any)
		if len(users) == 0 {
			problems = append(problems, "auth.yaml has no generated admin user")
		}
		adminFound := false
		adminPassword, passwordErr := os.ReadFile(filepath.Join(opts.etc, "secrets/admin_initial_password"))
		if err := validateSecretFile(filepath.Join(opts.etc, "secrets/admin_initial_password")); err != nil {
			problems = append(problems, err.Error())
		}
		for _, raw := range users {
			user, _ := raw.(map[string]any)
			isAdmin := strings.EqualFold(fmt.Sprint(user["role"]), "admin") && boolValue(user, "enabled", false)
			if isAdmin {
				adminFound = true
				if passwordErr == nil && bcrypt.CompareHashAndPassword([]byte(fmt.Sprint(user["password_hash"])), bytes.TrimSpace(adminPassword)) != nil {
					problems = append(problems, "auth.yaml admin hash does not match the local initial password secret")
				}
			}
			if strings.Contains(strings.ToLower(fmt.Sprint(user["password_hash"])), "__") || !strings.HasPrefix(fmt.Sprint(user["password_hash"]), "$2") {
				problems = append(problems, "auth.yaml contains a missing or placeholder password hash")
				break
			}
		}
		if !adminFound {
			problems = append(problems, "auth.yaml has no enabled admin account")
		}
	}

	networkPath := filepath.Join(opts.etc, "network.yaml")
	if networkData, readErr := readYAML(networkPath); readErr != nil {
		problems = append(problems, "network.yaml is missing or invalid")
	} else {
		if err := validateMode(networkPath, 0640, 0600); err != nil {
			problems = append(problems, err.Error())
		}
		if mode := nestedString(networkData, "synoranet", "security", "mode"); mode != "wpa3" {
			problems = append(problems, "network.yaml must use WPA3")
		}
		if pmf := nestedString(networkData, "synoranet", "security", "pmf"); pmf != "required" {
			problems = append(problems, "network.yaml must require PMF")
		}
		passphrasePath := nestedString(networkData, "synoranet", "ap", "passphrase_file")
		if passphrasePath == "" {
			passphrasePath = filepath.Join(opts.etc, "secrets/synoranet_psk")
		}
		if err := validateSecretFile(passphrasePath); err != nil {
			problems = append(problems, err.Error())
		}
	}

	for _, name := range []string{"security.yaml", "auth.yaml", "network.yaml", "devices.yaml"} {
		path := filepath.Join(opts.etc, name)
		if data, readErr := os.ReadFile(path); readErr == nil {
			text := strings.ToLower(string(data))
			for _, marker := range []string{"__generated_at_install__", "__set_during_first_boot__", "replace_with", "synora-dev-token-change-me", "changeme", "synoratest", "synora-test"} {
				if strings.Contains(text, marker) {
					problems = append(problems, fmt.Sprintf("%s still contains placeholder %q", name, marker))
				}
			}
		}
	}

	if len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "invalid:", problem)
		}
		return fmt.Errorf("configuration validation failed (%d issue(s))", len(problems))
	}
	fmt.Printf("configuration valid: %s\n", opts.etc)
	return nil
}

func rotateAPIToken(opts options) error {
	path := filepath.Join(opts.etc, "security.yaml")
	data, err := readYAML(path)
	if err != nil {
		return err
	}
	if !opts.apply {
		fmt.Printf("dry-run: would rotate API token and update %s plus its local secret file\n", path)
		return nil
	}
	token, err := randomHex(32)
	if err != nil {
		return err
	}
	data["api_token_hash"] = security.HashSecret(token)
	delete(data, "api_token")
	if err := writeYAMLWithBackup(path, data, 0640); err != nil {
		return err
	}
	return writeSecretWithBackup(filepath.Join(opts.etc, "secrets/api_token"), []byte(token+"\n"))
}

func rotateSynoraNetPSK(opts options) error {
	path := filepath.Join(opts.etc, "network.yaml")
	data, err := readYAML(path)
	if err != nil {
		return err
	}
	pathToSecret := nestedString(data, "synoranet", "ap", "passphrase_file")
	if pathToSecret == "" {
		pathToSecret = filepath.Join(opts.etc, "secrets/synoranet_psk")
	}
	if !opts.apply {
		fmt.Printf("dry-run: would rotate SynoraNet PSK at %s\n", pathToSecret)
		return nil
	}
	psk, err := randomHex(32)
	if err != nil {
		return err
	}
	return writeSecretWithBackup(pathToSecret, []byte(psk+"\n"))
}

func loadYAMLTemplate(preferred, fallback string) (map[string]any, error) {
	data, err := readYAML(preferred)
	if errors.Is(err, os.ErrNotExist) {
		return readYAML(fallback)
	}
	return data, err
}

func readYAML(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}

func writeYAMLIfMissing(path string, value map[string]any, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return writeFile(path, data, mode, false)
}

func copyIfMissing(source, destination string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if _, err := os.Stat(destination); err == nil {
		return nil
	}
	return writeFile(destination, data, mode, false)
}

func writeSecretIfMissing(path string, data []byte) error {
	return writeSecretIfMissingMode(path, data, 0600)
}

func writeSecretIfMissingMode(path string, data []byte, mode os.FileMode) error {
	return writeFile(path, data, mode, false)
}

func writeSecretWithBackup(path string, data []byte) error {
	return writeFile(path, data, 0600, true)
}

func writeYAMLWithBackup(path string, value map[string]any, mode os.FileMode) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return writeFile(path, data, mode, true)
}

func writeFile(path string, data []byte, mode os.FileMode, replace bool) error {
	if !replace {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if replace {
		if existing, err := os.ReadFile(path); err == nil {
			backup := fmt.Sprintf("%s.bak.%s", path, time.Now().UTC().Format("20060102-150405"))
			if err := os.WriteFile(backup, existing, mode); err != nil {
				return err
			}
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".synora-bootstrap-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func validateMode(path string, allowed ...os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s is missing", filepath.Base(path))
	}
	mode := info.Mode().Perm()
	if mode&007 != 0 {
		return fmt.Errorf("%s is world-readable or world-writable", filepath.Base(path))
	}
	for _, candidate := range allowed {
		if mode == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s has unexpected permissions %04o", filepath.Base(path), mode)
}

func validateSecretFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("secret file %s is missing", filepath.Base(path))
	}
	if info.Mode().Perm()&007 != 0 {
		return fmt.Errorf("secret file %s is world-readable or world-writable", filepath.Base(path))
	}
	if info.Size() < 24 {
		return fmt.Errorf("secret file %s is too short", filepath.Base(path))
	}
	return nil
}

func ensureSecretsDir(path string) error {
	if err := os.MkdirAll(path, 0750); err != nil {
		return err
	}
	if err := os.Chmod(path, 0750); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if group, err := user.LookupGroup("synora"); err == nil {
			if gid, err := strconv.Atoi(group.Gid); err == nil {
				_ = os.Chown(path, 0, gid)
			}
		}
	}
	return nil
}

func ensureRuntimeDirs(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	return nil
}

func ensureGeneratedSecret(path string, size int, mode os.FileMode) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	value, err := randomHex(size)
	if err != nil {
		return "", err
	}
	if err := writeSecretIfMissingMode(path, []byte(value+"\n"), mode); err != nil {
		return "", err
	}
	return value, nil
}

func secureConfigMetadata(path string) error {
	if err := os.Chmod(path, 0640); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return nil
	}
	group, err := user.LookupGroup("synora")
	if err != nil {
		return nil
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	return os.Chown(path, 0, gid)
}

func validateTemplates(dir string) error {
	var problems []string
	for _, name := range []string{"security.yaml.template", "auth.yaml.template", "network.yaml.template", "devices.yaml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("template %s is missing or unreadable", name))
			continue
		}
		text := strings.ToLower(string(data))
		for _, marker := range []string{"synora-dev-token-change-me", "cf58hj14", "synoratest", "changeme", "password:", "api_token:", "psk:"} {
			if strings.Contains(text, marker) {
				problems = append(problems, fmt.Sprintf("template %s contains forbidden bootstrap value %q", name, marker))
			}
		}
	}
	securityData, err := readYAML(filepath.Join(dir, "security.yaml.template"))
	if err == nil {
		features, _ := securityData["features"].(map[string]any)
		if boolValue(features, "debug_endpoints_enabled", true) || boolValue(features, "dev_simulation_enabled", true) {
			problems = append(problems, "security template enables debug or developer simulation")
		}
	}
	networkData, err := readYAML(filepath.Join(dir, "network.yaml.template"))
	if err == nil {
		if nestedString(networkData, "synoranet", "security", "mode") != "wpa3" || nestedString(networkData, "synoranet", "security", "pmf") != "required" {
			problems = append(problems, "network template must require WPA3 and PMF")
		}
	}
	if len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "invalid:", problem)
		}
		return fmt.Errorf("template validation failed (%d issue(s))", len(problems))
	}
	fmt.Printf("templates valid: %s\n", dir)
	return nil
}

func boolValue(value map[string]any, key string, fallback bool) bool {
	if value == nil {
		return fallback
	}
	if result, ok := value[key].(bool); ok {
		return result
	}
	return fallback
}

func stringValue(value map[string]any, key, fallback string) string {
	if result, ok := value[key].(string); ok && strings.TrimSpace(result) != "" {
		return result
	}
	return fallback
}

func nestedString(value map[string]any, keys ...string) string {
	current := value
	for index, key := range keys {
		item, ok := current[key].(map[string]any)
		if index == len(keys)-1 {
			if result, ok := current[key].(string); ok {
				return strings.TrimSpace(result)
			}
			return ""
		}
		if !ok {
			return ""
		}
		current = item
	}
	return ""
}

func setNestedString(value map[string]any, item string, keys ...string) {
	if len(keys) == 0 {
		return
	}
	current := value
	for _, key := range keys[:len(keys)-1] {
		nested, ok := current[key].(map[string]any)
		if !ok {
			nested = map[string]any{}
			current[key] = nested
		}
		current = nested
	}
	current[keys[len(keys)-1]] = item
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func randomPassword() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
