package cognitivecore

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const CaptureSchemaVersion = "synora.cognitive-v1-capture/v1"

type CaptureRecord struct {
	SchemaVersion string            `json:"schema_version"`
	CapturedAt    time.Time         `json:"captured_at"`
	EventID       string            `json:"event_id"`
	EventType     string            `json:"event_type"`
	Source        string            `json:"source"`
	Snapshot      CognitiveSnapshot `json:"snapshot"`
	Encoded       EncodedSnapshot   `json:"encoded"`
	Decision      Decision          `json:"decision"`
	ActionResult  *ActionResultFact `json:"action_result,omitempty"`
	Label         string            `json:"label,omitempty"`
	Provenance    string            `json:"provenance"`
}

type CaptureWriter struct {
	queue  chan CaptureRecord
	done   chan struct{}
	closed chan struct{}
	once   sync.Once
	path   string
}

func NewCaptureWriter(path string, capacity int) (*CaptureWriter, error) {
	if path == "" {
		return nil, errors.New("capture path is empty")
	}
	if capacity < 1 {
		capacity = 128
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	writer := &CaptureWriter{queue: make(chan CaptureRecord, capacity), done: make(chan struct{}), closed: make(chan struct{}), path: path}
	go writer.run()
	return writer, nil
}

func (w *CaptureWriter) Enqueue(commit Commit, result CommitResult) bool {
	if w == nil {
		return false
	}
	record := CaptureRecord{SchemaVersion: CaptureSchemaVersion, CapturedAt: time.Now().UTC(), EventID: commit.Event.ID, EventType: commit.Event.Type, Source: commit.Event.Source, Snapshot: commit.Snapshot, Encoded: commit.Training.Encoded, Decision: commit.Decision, Provenance: commit.Training.Provenance, Label: commit.Training.Label}
	select {
	case w.queue <- record:
		return true
	default:
		return false
	}
}

func (w *CaptureWriter) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() { close(w.done) })
	<-w.closed
	return nil
}

func (w *CaptureWriter) run() {
	defer close(w.closed)
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	flush := func(record CaptureRecord) { _ = json.NewEncoder(writer).Encode(record); _ = writer.Flush() }
	for {
		select {
		case record := <-w.queue:
			flush(record)
		case <-w.done:
			for {
				select {
				case record := <-w.queue:
					flush(record)
				default:
					_ = writer.Flush()
					return
				}
			}
		}
	}
}

func CaptureRecordFromCommit(commit Commit) CaptureRecord {
	return CaptureRecord{SchemaVersion: CaptureSchemaVersion, CapturedAt: commit.CommittedAt, EventID: commit.Event.ID, EventType: commit.Event.Type, Source: commit.Event.Source, Snapshot: commit.Snapshot, Encoded: commit.Training.Encoded, Decision: commit.Decision, Provenance: commit.Training.Provenance, Label: commit.Training.Label}
}

func ValidateCaptureRecord(record CaptureRecord) error {
	if record.SchemaVersion != CaptureSchemaVersion || record.EventID == "" || record.EventType == "" || record.Snapshot.Validate() != nil || record.Encoded.Validate() != nil {
		return errors.New("invalid V1 capture record")
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("empty capture record")
	}
	return nil
}
