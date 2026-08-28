package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
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
	switch os.Args[1] {
	case "create":
		store := state.NewStore(state.WithPersistencePath(statePath))
		if _, err := store.LoadPersisted(); err != nil {
			log.Fatal(err)
		}
		manifest, err := manager.Create(context.Background(), store, nil)
		if err != nil {
			log.Fatal(err)
		}
		printJSON(manifest)
	case "restore":
		if len(os.Args) != 3 {
			log.Fatal("usage: synora-backup restore SNAPSHOT_ID")
		}
		store := state.NewStore(state.WithPersistencePath(statePath))
		if err := manager.Restore(context.Background(), os.Args[2], store, nil); err != nil {
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

func printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}
