package contract

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

const (
	SourceDevice    = "device"
	SourceService   = "service"
	SourceSimulator = "simulator"
	SourceSystem    = "system"
)

const (
	KindEvent   = "event"
	KindCommand = "command"
	KindRPC     = "rpc"
)

type Message struct {
	ID            string          `json:"id,omitempty"`
	Version       string          `json:"version,omitempty"`
	Type          string          `json:"type"`
	Kind          string          `json:"kind,omitempty"`
	Source        string          `json:"source"`
	Target        string          `json:"target,omitempty"`
	SourceType    string          `json:"source_type,omitempty"`
	Timestamp     time.Time       `json:"timestamp,omitempty"`
	Priority      int             `json:"priority,omitempty"`
	TrackID       string          `json:"track_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	aux := struct {
		ID            string          `json:"id,omitempty"`
		Version       string          `json:"version,omitempty"`
		Type          string          `json:"type"`
		Kind          string          `json:"kind,omitempty"`
		Source        string          `json:"source"`
		Target        string          `json:"target,omitempty"`
		SourceType    string          `json:"source_type,omitempty"`
		Priority      int             `json:"priority,omitempty"`
		TrackID       string          `json:"track_id,omitempty"`
		CorrelationID string          `json:"correlation_id,omitempty"`
		RequestID     string          `json:"request_id,omitempty"`
		Payload       json.RawMessage `json:"payload,omitempty"`
		Timestamp     any             `json:"timestamp,omitempty"`
	}{
		ID:            m.ID,
		Version:       m.Version,
		Type:          m.Type,
		Kind:          m.Kind,
		Source:        m.Source,
		Target:        m.Target,
		SourceType:    m.SourceType,
		Priority:      m.Priority,
		TrackID:       m.TrackID,
		CorrelationID: m.CorrelationID,
		RequestID:     m.RequestID,
		Payload:       m.Payload,
	}
	if !m.Timestamp.IsZero() {
		aux.Timestamp = m.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return json.Marshal(aux)
}

func (m *Message) UnmarshalJSON(data []byte) error {
	aux := struct {
		ID            string          `json:"id,omitempty"`
		Version       string          `json:"version,omitempty"`
		Type          string          `json:"type"`
		Kind          string          `json:"kind,omitempty"`
		Source        string          `json:"source"`
		Target        string          `json:"target,omitempty"`
		SourceType    string          `json:"source_type,omitempty"`
		Priority      int             `json:"priority,omitempty"`
		TrackID       string          `json:"track_id,omitempty"`
		CorrelationID string          `json:"correlation_id,omitempty"`
		RequestID     string          `json:"request_id,omitempty"`
		Payload       json.RawMessage `json:"payload,omitempty"`
		Timestamp     any             `json:"timestamp,omitempty"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = Message{
		ID:            aux.ID,
		Version:       aux.Version,
		Type:          aux.Type,
		Kind:          aux.Kind,
		Source:        aux.Source,
		Target:        aux.Target,
		SourceType:    aux.SourceType,
		Priority:      aux.Priority,
		TrackID:       aux.TrackID,
		CorrelationID: aux.CorrelationID,
		RequestID:     aux.RequestID,
		Payload:       aux.Payload,
	}
	m.Timestamp = parseMessageTimestamp(aux.Timestamp)
	return nil
}

func parseMessageTimestamp(value any) time.Time {
	switch current := value.(type) {
	case nil:
		return time.Time{}
	case float64:
		if current > 1e12 {
			return time.UnixMilli(int64(current)).UTC()
		}
		seconds, frac := mathModf(current)
		return time.Unix(int64(seconds), int64(frac*float64(time.Second))).UTC()
	case int64:
		if current > 1e12 {
			return time.UnixMilli(current).UTC()
		}
		return time.Unix(current, 0).UTC()
	case string:
		current = strings.TrimSpace(current)
		if current == "" {
			return time.Time{}
		}
		if parsed, err := time.Parse(time.RFC3339Nano, current); err == nil {
			return parsed.UTC()
		}
		if parsed, err := strconv.ParseInt(current, 10, 64); err == nil {
			if parsed > 1e12 {
				return time.UnixMilli(parsed).UTC()
			}
			return time.Unix(parsed, 0).UTC()
		}
	}
	return time.Time{}
}

func mathModf(value float64) (float64, float64) {
	seconds := float64(int64(value))
	return seconds, value - seconds
}
