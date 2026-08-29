package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synora/internal/backup"
	"synora/internal/runtimeconfig"
	"synora/internal/state"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: synora-backup create|restore|expire")
	}
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	root := runtime.Paths.BackupRoot
	statePath := runtime.Paths.State
	manager := backup.New(root, 512<<20)
	manager.Secret = strings.TrimSpace(os.Getenv("SYNORA_BACKUP_SECRET"))
	switch os.Args[1] {
	case "create":
		if manager.Secret == "" {
			log.Fatal("SYNORA_BACKUP_SECRET is required for backup creation")
		}
		store := state.NewStore(state.WithPersistencePath(statePath))
		if _, err := store.LoadPersisted(); err != nil {
			log.Fatal(err)
		}
		sources, _, err := backupPaths(runtime.Paths)
		if err != nil {
			log.Fatal(err)
		}
		manifest, err := manager.Create(context.Background(), store, sources)
		if err != nil {
			log.Fatal(err)
		}
		printJSON(manifest)
	case "restore":
		if manager.Secret == "" {
			log.Fatal("SYNORA_BACKUP_SECRET is required for backup restoration")
		}
		if len(os.Args) != 3 {
			log.Fatal("usage: synora-backup restore SNAPSHOT_ID")
		}
		store := state.NewStore(state.WithPersistencePath(statePath))
		_, destinations, err := backupPaths(runtime.Paths)
		if err != nil {
			log.Fatal(err)
		}
		if err := manager.Restore(context.Background(), os.Args[2], store, destinations); err != nil {
			log.Fatal(err)
		}
		fmt.Println("restored", os.Args[2])
	case "expire":
		set := flag.NewFlagSet("expire", flag.ExitOnError)
		age := set.Duration("age", 30*24*time.Hour, "retain snapshots newer than this duration")
		_ = set.Parse(os.Args[2:])
		removed, err := manager.Expire(time.Now().UTC().Add(-*age))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("expired", removed)
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func backupPaths(paths runtimeconfig.Paths) (map[string]string, map[string]string, error) {
	sources := make(map[string]string)
	destinations := make(map[string]string)
	add := func(name, path string) error {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("backup source is not a regular file: %s", path)
		}
		sources[name], destinations[name] = path, path
		return nil
	}
	for name, path := range map[string]string{
		"config/auth.yaml":          paths.Auth,
		"config/security.yaml":      paths.Security,
		"config/topology.yaml":      paths.Topology,
		"config/residents.yaml":     paths.Residents,
		"config/devices.yaml":       paths.Devices,
		"config/automations.yaml":   paths.Automations,
		"config/action_policy.yaml": paths.ActionPolicy,
		"config/cge_chains.yaml":    paths.CGEChains,
		"config/cge_profile.yaml":   paths.CGEProfile,
		"config/network.yaml":       paths.NetworkConfig,
		"config/mediamtx.yml":       paths.MediaMTXConfig,
		"security/identities.json":  paths.IdentityRegistry,
	} {
		if err := add(name, path); err != nil {
			return nil, nil, err
		}
	}
	faceRoot := strings.TrimSpace(paths.FaceDataRoot)
	if faceRoot != "" {
		if info, err := os.Lstat(faceRoot); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, nil, fmt.Errorf("face data root is unsafe: %s", faceRoot)
			}
			err = filepath.WalkDir(faceRoot, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					if path != faceRoot && (entry.Name() == "uploads" || entry.Name() == "staging") {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("face data contains symlink: %s", path)
				}
				if !entry.Type().IsRegular() {
					return nil
				}
				rel, err := filepath.Rel(faceRoot, path)
				if err != nil {
					return err
				}
				return add(filepath.ToSlash(filepath.Join("faces", rel)), path)
			})
			if err != nil {
				return nil, nil, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	return sources, destinations, nil
}

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}
