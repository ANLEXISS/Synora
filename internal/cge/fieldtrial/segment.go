package fieldtrial

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"synora/internal/cge/contractcatalog"
)

type Envelope struct {
	Sequence      uint64     `json:"sequence"`
	PreviousHash  string     `json:"previous_hash"`
	PayloadSHA256 string     `json:"payload_sha256"`
	RecordHash    string     `json:"record_hash"`
	Payload       TrialEvent `json:"payload"`
}

type AnnotationEnvelope struct {
	Sequence      uint64     `json:"sequence"`
	PreviousHash  string     `json:"previous_hash"`
	PayloadSHA256 string     `json:"payload_sha256"`
	RecordHash    string     `json:"record_hash"`
	Payload       Annotation `json:"payload"`
}

func segmentName(index int) string { return fmt.Sprintf("events-%06d.ndjson", index) }

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func recordHash(sequence uint64, previous, payloadHash string) string {
	data, _ := json.Marshal(struct {
		Sequence uint64 `json:"sequence"`
		Previous string `json:"previous_hash"`
		Payload  string `json:"payload_sha256"`
	}{sequence, previous, payloadHash})
	return hashBytes(data)
}

func encodeEnvelope(envelope Envelope) ([]byte, error) {
	if err := contractcatalog.ValidateStoreWrite("synora.store.field-trial-recorder", "synora.cge.field-trial-envelope.v1", envelope); err != nil {
		return nil, err
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func encodeAnnotationEnvelope(envelope AnnotationEnvelope) ([]byte, error) {
	if err := contractcatalog.ValidateStoreWrite("synora.store.field-trial-recorder", "synora.cge.field-trial-annotation-envelope.v1", envelope); err != nil {
		return nil, err
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

type segmentState struct {
	Index    int
	Bytes    int64
	Sequence uint64
	Hash     string
	Events   uint64
	Refs     map[string]struct{}
	Values   []TrialEvent
}

func verifyEventSegment(path string, expected uint64, previous string, repair bool, sessionID string) (segmentState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return segmentState{}, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if !repair {
			return segmentState{}, ErrPartialRecord
		}
		last := bytes.LastIndexByte(data, '\n')
		if last < 0 {
			last = 0
		} else {
			last++
		}
		if err := os.WriteFile(path, data[:last], 0o640); err != nil {
			return segmentState{}, err
		}
		data = data[:last]
	}
	state := segmentState{Bytes: int64(len(data)), Sequence: expected - 1, Hash: previous, Refs: map[string]struct{}{}, Values: make([]TrialEvent, 0)}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 4<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var envelope Envelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			return segmentState{}, fmt.Errorf("%w: %s", ErrTelemetryCorrupt, filepath.Base(path))
		}
		if envelope.Sequence != expected || envelope.PreviousHash != state.Hash || envelope.Payload.SessionID != sessionID || envelope.Payload.Sequence != envelope.Sequence {
			return segmentState{}, fmt.Errorf("%w: event sequence", ErrTelemetryCorrupt)
		}
		payload, err := json.Marshal(envelope.Payload)
		if err != nil || envelope.PayloadSHA256 != hashBytes(payload) || envelope.RecordHash != recordHash(envelope.Sequence, envelope.PreviousHash, envelope.PayloadSHA256) {
			return segmentState{}, fmt.Errorf("%w: event hash", ErrTelemetryCorrupt)
		}
		state.Sequence, state.Hash, state.Events = envelope.Sequence, envelope.RecordHash, state.Events+1
		if envelope.Payload.EventRef != "" {
			state.Refs[envelope.Payload.EventRef] = struct{}{}
		}
		state.Values = append(state.Values, envelope.Payload)
		expected++
	}
	if err := scanner.Err(); err != nil {
		return segmentState{}, err
	}
	return state, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(output, input); err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err != nil {
		return err
	}
	return closeErr
}
