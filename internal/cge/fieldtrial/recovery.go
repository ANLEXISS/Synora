package fieldtrial

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ReadManifest(sessionDir string) (SessionManifest, error) {
	var manifest SessionManifest
	data, err := os.ReadFile(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("%w: manifest", ErrTelemetryCorrupt)
	}
	if !validSessionID(manifest.SessionID) || manifest.SchemaVersion != SchemaVersion {
		return manifest, ErrTelemetryCorrupt
	}
	return manifest, nil
}

func VerifySession(ctx context.Context, sessionDir string, repairTerminalPartial bool) (SessionManifest, error) {
	if err := contextErr(ctx); err != nil {
		return SessionManifest{}, err
	}
	manifest, err := ReadManifest(sessionDir)
	if err != nil {
		return manifest, err
	}
	if err := ensureDirectory(sessionDir); err != nil {
		return manifest, err
	}
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return manifest, err
	}
	indices := make([]int, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "events-") && strings.HasSuffix(entry.Name(), ".ndjson") {
			var index int
			if _, err := fmt.Sscanf(entry.Name(), "events-%06d.ndjson", &index); err != nil || index <= 0 {
				return manifest, ErrTelemetryCorrupt
			}
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)
	if len(indices) != manifest.SegmentCount && manifest.SegmentCount != 0 {
		return manifest, fmt.Errorf("%w: segment count", ErrTelemetryCorrupt)
	}
	expected, previous := uint64(1), ""
	var events uint64
	for index, value := range indices {
		if value != index+1 {
			return manifest, fmt.Errorf("%w: segment order", ErrTelemetryCorrupt)
		}
		if err := ensureRegular(filepath.Join(sessionDir, segmentName(value))); err != nil {
			return manifest, err
		}
		state, err := verifyEventSegment(filepath.Join(sessionDir, segmentName(value)), expected, previous, repairTerminalPartial && index == len(indices)-1, manifest.SessionID)
		if err != nil {
			return manifest, err
		}
		expected, previous, events = state.Sequence+1, state.Hash, events+state.Events
	}
	if events != manifest.EventCount || (events > 0 && previous != manifest.LastSegmentHash) {
		return manifest, fmt.Errorf("%w: manifest event head", ErrTelemetryCorrupt)
	}
	annotationFile, _, _, count, err := openAnnotations(filepath.Join(sessionDir, "annotations.ndjson"))
	if annotationFile != nil {
		_ = annotationFile.Close()
	}
	if err != nil {
		return manifest, err
	} else if count != manifest.AnnotationCount {
		return manifest, fmt.Errorf("%w: annotation count", ErrTelemetryCorrupt)
	}
	return manifest, nil
}

func ReadEvents(ctx context.Context, sessionDir string) ([]TrialEvent, []Annotation, SessionManifest, error) {
	manifest, err := VerifySession(ctx, sessionDir, false)
	if err != nil {
		return nil, nil, manifest, err
	}
	events, err := readEventValues(sessionDir)
	if err != nil {
		return nil, nil, manifest, err
	}
	annotations, err := readAnnotationValues(sessionDir)
	if err != nil {
		return nil, nil, manifest, err
	}
	return events, annotations, manifest, nil
}

func readEventValues(sessionDir string) ([]TrialEvent, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "events-") && strings.HasSuffix(entry.Name(), ".ndjson") {
			paths = append(paths, filepath.Join(sessionDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	result := make([]TrialEvent, 0)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 4<<20)
		for scanner.Scan() {
			var envelope Envelope
			if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
				file.Close()
				return nil, err
			}
			result = append(result, envelope.Payload)
		}
		if err := scanner.Err(); err != nil {
			file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readAnnotationValues(sessionDir string) ([]Annotation, error) {
	path := filepath.Join(sessionDir, "annotations.ndjson")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	result := make([]Annotation, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		var envelope AnnotationEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return nil, err
		}
		result = append(result, envelope.Payload)
	}
	return result, scanner.Err()
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: session directory", ErrTelemetryCorrupt)
	}
	return nil
}

func ensureRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: segment is not regular", ErrTelemetryCorrupt)
	}
	return nil
}
