package cognitive

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	CaptureEnabledEnv  = "SYNORA_COGNITIVE_CAPTURE"
	CapturePathEnv     = "SYNORA_COGNITIVE_DATASET_PATH"
	DefaultCapturePath = "/var/lib/synora/cognitive/dataset.jsonl"
)

// DatasetInput is the frozen boundary presented to a future model. The maps
// are serialized at Before time, so later Core mutations cannot rewrite it.
type DatasetInput struct {
	Event         json.RawMessage `json:"event"`
	StateFrame    json.RawMessage `json:"state_frame"`
	EncodedState  json.RawMessage `json:"encoded_state"`
	ActionCatalog json.RawMessage `json:"action_catalog"`
	ActionLedger  json.RawMessage `json:"action_ledger"`
	StateBefore   json.RawMessage `json:"state_before"`
	RecentEvents  json.RawMessage `json:"recent_events"`
	Topology      json.RawMessage `json:"topology"`
	Residents     json.RawMessage `json:"residents"`
}

type TeacherDecision struct {
	EventID               string          `json:"event_id"`
	EventType             string          `json:"event_type"`
	Decision              json.RawMessage `json:"decision,omitempty"`
	InferredState         string          `json:"inferred_state,omitempty"`
	DangerLevel           string          `json:"danger_level,omitempty"`
	DangerScore           float64         `json:"danger_score,omitempty"`
	StateChanged          bool            `json:"state_changed"`
	CognitiveActionIssued bool            `json:"cognitive_action_issued"`
}

type DatasetRecord struct {
	SchemaVersion string          `json:"schema_version"`
	CapturedAt    time.Time       `json:"captured_at"`
	Input         DatasetInput    `json:"input"`
	Teacher       TeacherDecision `json:"teacher"`
	Redaction     RedactionInfo   `json:"redaction"`
}

// RedactionInfo leaves an explicit seam for a future minimization pass before
// training. Capture itself is not a claim that the JSONL is training-ready.
type RedactionInfo struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Applied bool   `json:"applied"`
}

type CapturePending struct {
	input DatasetInput
}

type Recorder struct {
	path   string
	queue  chan DatasetRecord
	done   chan struct{}
	closed chan struct{}
	once   sync.Once
}

// NewRecorderFromEnv is disabled unless SYNORA_COGNITIVE_CAPTURE is truthy.
// When enabled it uses an append-only JSONL writer with a bounded, nonblocking
// enqueue path so capture cannot backpressure Core.
func NewRecorderFromEnv(getenv func(string) string) (*Recorder, bool) {
	if getenv == nil || !truthy(getenv(CaptureEnabledEnv)) {
		return nil, false
	}
	path := strings.TrimSpace(getenv(CapturePathEnv))
	if path == "" {
		path = DefaultCapturePath
	}
	r := &Recorder{
		path: path, queue: make(chan DatasetRecord, 256), done: make(chan struct{}), closed: make(chan struct{}),
	}
	go r.writeLoop()
	return r, true
}

func (r *Recorder) Before(values map[string]any) *CapturePending {
	if r == nil {
		return nil
	}
	input := DatasetInput{}
	input.Event = marshalOrEmpty(values["event"])
	input.StateFrame = marshalOrEmpty(values["state_frame"])
	input.EncodedState = marshalOrEmpty(values["encoded_state"])
	input.ActionCatalog = marshalOrEmpty(values["action_catalog"])
	input.ActionLedger = marshalOrEmpty(values["action_ledger"])
	input.StateBefore = marshalOrEmpty(values["state_before"])
	input.RecentEvents = marshalOrEmpty(values["recent_events"])
	input.Topology = marshalOrEmpty(values["topology"])
	input.Residents = marshalOrEmpty(values["residents"])
	return &CapturePending{input: input}
}

func (r *Recorder) After(pending *CapturePending, teacher TeacherDecision) {
	if r == nil || pending == nil {
		return
	}
	record := DatasetRecord{
		SchemaVersion: SchemaVersion,
		CapturedAt:    time.Now().UTC(),
		Input:         pending.input,
		Teacher:       teacher,
		Redaction:     RedactionInfo{Status: "not_applied", Version: "redaction/v1", Applied: false},
	}
	select {
	case r.queue <- record:
	default:
		// Dropping a capture is preferable to altering Core timing or behavior.
		log.Printf("cognitive capture queue full; dropping event=%s", teacher.EventID)
	}
}

func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.once.Do(func() { close(r.done) })
	<-r.closed
	return nil
}

func (r *Recorder) writeLoop() {
	defer close(r.closed)
	file, buffered, err := openJSONL(r.path)
	if err != nil {
		log.Printf("cognitive capture disabled after open error path=%s err=%v", r.path, err)
		return
	}
	defer file.Close()
	writer := json.NewEncoder(buffered)
	for {
		select {
		case record := <-r.queue:
			if err := writer.Encode(record); err != nil {
				log.Printf("cognitive capture write error path=%s err=%v", r.path, err)
			}
			_ = buffered.Flush()
		case <-r.done:
			for {
				select {
				case record := <-r.queue:
					if err := writer.Encode(record); err != nil {
						log.Printf("cognitive capture write error path=%s err=%v", r.path, err)
					}
				default:
					_ = buffered.Flush()
					return
				}
			}
		}
	}
}

func openJSONL(path string) (*os.File, *bufio.Writer, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil, errors.New("capture path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, nil, err
	}
	return file, bufio.NewWriter(file), nil
}

func marshalOrEmpty(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage(`null`)
	}
	body, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return append(json.RawMessage(nil), body...)
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
