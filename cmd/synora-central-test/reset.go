package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"synora/internal/bus"
	"synora/internal/cognitivecore"
	"synora/pkg/contract"
)

func runCentralStateReset(client *bus.Client, storeDir string, reset fixtureReset, now time.Time) (string, error) {
	if client == nil {
		return "unavailable", errors.New("API bus client is unavailable")
	}
	if strings.TrimSpace(reset.Reason) == "" || strings.TrimSpace(reset.CreatedBy) == "" {
		return "rejected", errors.New("reset reason and actor are required")
	}
	payload, err := json.Marshal(contract.SystemStateResetRequest{
		TargetState: "empty",
		Reason:      strings.TrimSpace(reset.Reason),
		CreatedBy:   strings.TrimSpace(reset.CreatedBy),
	})
	if err != nil {
		return "rejected", err
	}
	for _, target := range []string{"core", "discovery"} {
		result, err := requestCentralResetScope(client, target, payload, now)
		if err != nil {
			return "unknown", err
		}
		if result.Status != "erased" || result.Scope != target || result.TargetState != "empty" {
			return "rejected", fmt.Errorf("%s reset status=%q scope=%q target=%q", target, result.Status, result.Scope, result.TargetState)
		}
	}
	store, err := cognitivecore.OpenUniversalStore(storeDir)
	if err != nil {
		return "unknown", err
	}
	if history, err := store.History(); err != nil {
		return "unknown", err
	} else if len(history) != 0 || len(store.Journal()) != 0 {
		return "failed", fmt.Errorf("core durable state remains: history=%d journal=%d", len(history), len(store.Journal()))
	}
	for _, root := range []string{os.Getenv("SYNORA_CLIP_ROOT"), os.Getenv("SYNORA_FACE_DATA_ROOT")} {
		if err := verifyNoRegularFiles(root); err != nil {
			return "failed", fmt.Errorf("discovery data remains: %w", err)
		}
	}
	if reset.ExpectState != "" && reset.ExpectState != "empty" {
		return "failed", fmt.Errorf("unexpected fixture reset expectation %q", reset.ExpectState)
	}
	return "erased", nil
}

func requestCentralResetScope(client *bus.Client, target string, payload []byte, now time.Time) (contract.SystemStateResetResult, error) {
	requestID := uuid.New().String()
	if err := client.Send(contract.Message{ID: requestID, Type: contract.RPCSystemResetState, Kind: contract.KindRPC, Source: "api", Target: target, Timestamp: now, Payload: payload}); err != nil {
		return contract.SystemStateResetResult{}, err
	}
	deadline := time.NewTimer(centralTimeout)
	defer deadline.Stop()
	for {
		select {
		case message := <-client.SubscribeChannel("api"):
			if message.ID != requestID {
				continue
			}
			var result contract.SystemStateResetResult
			if err := json.Unmarshal(message.Payload, &result); err != nil {
				return contract.SystemStateResetResult{}, err
			}
			return result, nil
		case <-deadline.C:
			return contract.SystemStateResetResult{}, errors.New("bus timeout")
		}
	}
}

func verifyNoRegularFiles(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("empty data root")
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if path == filepath.Clean(root) || entry.IsDir() {
			return nil
		}
		if entry.Type().IsRegular() {
			return fmt.Errorf("regular file %s", path)
		}
		return nil
	})
}
