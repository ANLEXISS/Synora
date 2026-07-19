package fieldtrial

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type ExportOptions struct {
	IncludeEvents         bool
	IncludeAnnotations    bool
	IncludeDailySummaries bool
	RemoveHashPrefixes    bool
}

type ExportFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type ExportManifest struct {
	SchemaVersion string          `json:"schema_version"`
	Session       SessionManifest `json:"session"`
	Files         []ExportFile    `json:"files"`
}

func ExportSession(ctx context.Context, sessionDir, outputDir string, options ExportOptions) (ExportManifest, error) {
	if err := contextErr(ctx); err != nil {
		return ExportManifest{}, err
	}
	events, annotations, manifest, err := ReadEvents(ctx, sessionDir)
	if err != nil {
		return ExportManifest{}, err
	}
	if filepath.Clean(outputDir) == "." || outputDir == "" {
		return ExportManifest{}, ErrExportInvalid
	}
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return ExportManifest{}, err
	}
	export := ExportManifest{SchemaVersion: SchemaVersion, Session: manifest, Files: make([]ExportFile, 0)}
	if options.IncludeEvents {
		if err := writeNDJSON(filepath.Join(outputDir, "events.ndjson"), events); err != nil {
			return ExportManifest{}, err
		}
	}
	if options.IncludeAnnotations {
		if err := writeNDJSON(filepath.Join(outputDir, "annotations.ndjson"), annotations); err != nil {
			return ExportManifest{}, err
		}
	}
	if options.IncludeDailySummaries {
		daily, err := BuildDailySummaries(ctx, sessionDir)
		if err != nil {
			return ExportManifest{}, err
		}
		if err := writeJSON(filepath.Join(outputDir, "daily-summaries.json"), daily); err != nil {
			return ExportManifest{}, err
		}
		report, err := BuildReport(ctx, sessionDir)
		if err != nil {
			return ExportManifest{}, err
		}
		if err := writeJSON(filepath.Join(outputDir, "report.json"), report); err != nil {
			return ExportManifest{}, err
		}
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return ExportManifest{}, err
	}
	for _, entry := range entries {
		if entry.Name() == "export-manifest.json" || entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outputDir, entry.Name()))
		if err != nil {
			return ExportManifest{}, err
		}
		sum := sha256.Sum256(data)
		export.Files = append(export.Files, ExportFile{Name: entry.Name(), SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))})
	}
	sort.Slice(export.Files, func(i, j int) bool { return export.Files[i].Name < export.Files[j].Name })
	if options.RemoveHashPrefixes {
		export.Session.LastSegmentHash = ""
	}
	if err := writeJSON(filepath.Join(outputDir, "export-manifest.json"), export); err != nil {
		return ExportManifest{}, err
	}
	return export, nil
}

func writeNDJSON(path string, values any) error {
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	for _, item := range items {
		if _, err := file.Write(append(item, '\n')); err != nil {
			file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func ExportPath(sessionDir string) (string, error) {
	manifest, err := ReadManifest(sessionDir)
	if err != nil {
		return "", err
	}
	if manifest.SessionID == "" {
		return "", fmt.Errorf("%w: session", ErrExportInvalid)
	}
	return sessionDir, nil
}
