package fieldtrial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func VerifyExport(ctx context.Context, outputDir string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "export-manifest.json"))
	if err != nil {
		return err
	}
	var manifest ExportManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ErrExportInvalid
	}
	if manifest.SchemaVersion != SchemaVersion {
		return ErrExportInvalid
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.Contains(strings.ToLower(entry.Name()), "key") {
			return ErrExportInvalid
		}
	}
	for _, file := range manifest.Files {
		if strings.Contains(strings.ToLower(file.Name), "key") || filepath.IsAbs(file.Name) || strings.Contains(file.Name, "..") {
			return ErrExportInvalid
		}
		contents, err := os.ReadFile(filepath.Join(outputDir, file.Name))
		if err != nil {
			return err
		}
		digest := sha256.Sum256(contents)
		if hex.EncodeToString(digest[:]) != file.SHA256 || int64(len(contents)) != file.Bytes {
			return fmt.Errorf("%w: checksum %s", ErrExportInvalid, file.Name)
		}
	}
	if events, err := os.ReadFile(filepath.Join(outputDir, "events.ndjson")); err == nil && len(events) > 0 {
		lines := strings.Split(strings.TrimSuffix(string(events), "\n"), "\n")
		for index, line := range lines {
			var event TrialEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil || event.Sequence != uint64(index+1) || event.SessionID != manifest.Session.SessionID {
				return ErrExportInvalid
			}
		}
	}
	if annotations, err := os.ReadFile(filepath.Join(outputDir, "annotations.ndjson")); err == nil && len(annotations) > 0 {
		lines := strings.Split(strings.TrimSuffix(string(annotations), "\n"), "\n")
		for _, line := range lines {
			var annotation Annotation
			if err := json.Unmarshal([]byte(line), &annotation); err != nil || annotation.SessionID != manifest.Session.SessionID || annotation.EventRef == "" {
				return ErrExportInvalid
			}
		}
	}
	return nil
}
