// Package migrations is the versioned, rollback-aware configuration
// migration framework. It is deliberately not wired into a runtime service
// yet; the OTA controller will call it before mark-good.
package migrations

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"synora/internal/configfile"
)

type Definition struct {
	ID          string
	FromSchema  int
	ToSchema    int
	Description string
	Apply       func(*yaml.Node) error
}

type Result struct {
	Planned, Applied []string
	DryRun           bool
	Backup           string
}

var registry = []Definition{
	{ID: "0001_network_security", FromSchema: 0, ToSchema: 1, Description: "normalize network security defaults", Apply: noop},
	{ID: "0002_features_flags", FromSchema: 1, ToSchema: 2, Description: "normalize feature flags", Apply: noop},
	{ID: "0003_cge_decay", FromSchema: 2, ToSchema: 3, Description: "normalize CGE decay settings", Apply: noop},
}

func noop(*yaml.Node) error { return nil }

func List() []Definition {
	result := append([]Definition(nil), registry...)
	sort.Slice(result, func(i, j int) bool { return result[i].FromSchema < result[j].FromSchema })
	return result
}

func Plan(currentSchema, targetSchema int) ([]Definition, error) {
	if currentSchema < 0 || targetSchema < currentSchema {
		return nil, fmt.Errorf("invalid schema range %d -> %d", currentSchema, targetSchema)
	}
	result := make([]Definition, 0)
	current := currentSchema
	for current < targetSchema {
		var found *Definition
		for _, migration := range registry {
			if migration.FromSchema == current {
				copy := migration
				found = &copy
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("no migration from schema %d", current)
		}
		if found.ToSchema > targetSchema {
			return nil, fmt.Errorf("migration %s exceeds target schema", found.ID)
		}
		result = append(result, *found)
		current = found.ToSchema
	}
	return result, nil
}

// Apply runs registered transforms in memory. Skeleton transforms are no-ops,
// so applying them is idempotent and does not rewrite a runtime file until a
// concrete transform changes the YAML document.
func Apply(path string, currentSchema, targetSchema int, dryRun bool) (Result, error) {
	plan, err := Plan(currentSchema, targetSchema)
	if err != nil {
		return Result{}, err
	}
	result := Result{DryRun: dryRun}
	for _, migration := range plan {
		result.Planned = append(result.Planned, migration.ID)
	}
	if dryRun || len(plan) == 0 {
		return result, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read migration target: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return result, fmt.Errorf("parse migration target: %w", err)
	}
	var originalDocument yaml.Node
	if err := yaml.Unmarshal(data, &originalDocument); err != nil {
		return result, fmt.Errorf("parse original migration target: %w", err)
	}
	for _, migration := range plan {
		if migration.Apply == nil {
			return result, fmt.Errorf("migration %s has no handler", migration.ID)
		}
		if err := migration.Apply(&document); err != nil {
			return result, fmt.Errorf("apply %s: %w", migration.ID, err)
		}
	}
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return result, fmt.Errorf("encode migrated config: %w", err)
	}
	originalNormalized, err := yaml.Marshal(&originalDocument)
	if err != nil {
		return result, fmt.Errorf("encode original migration target: %w", err)
	}
	if bytes.Equal(bytes.TrimSpace(updated), bytes.TrimSpace(originalNormalized)) {
		return result, nil
	}
	mode := os.FileMode(0640)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := configfile.WriteAtomicWithBackup(path, updated, mode); err != nil {
		return result, err
	}
	result.Applied = result.Planned
	result.Backup = filepath.Join(filepath.Dir(path), "backups")
	return result, nil
}

func SchemaVersion(data []byte) int {
	var raw struct {
		SchemaVersion int `yaml:"schema_version"`
	}
	if yaml.Unmarshal(data, &raw) != nil {
		return 0
	}
	return raw.SchemaVersion
}

func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, key := range []string{"token", "password", "secret", "psk", "private_key"} {
		if strings.Contains(strings.ToLower(message), key) {
			return "migration failed (sensitive details redacted)"
		}
	}
	return message
}
