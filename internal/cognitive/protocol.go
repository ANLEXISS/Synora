package cognitive

import "strings"

const (
	MessageTypeTask   = "cognitive.task"
	MessageTypeOutput = "cognitive.output"
	MessageTypeStatus = "cognitive.status"
	MessageTypeError  = "cognitive.error"
)

func IsProtocolMessage(messageType string) bool {
	return strings.HasPrefix(strings.TrimSpace(messageType), "cognitive.")
}

type Status struct {
	SchemaVersion string              `json:"schema_version"`
	Service       string              `json:"service"`
	Status        string              `json:"status"`
	AdvisoryOnly  bool                `json:"advisory_only"`
	DryRunOnly    bool                `json:"dry_run_only"`
	Adapters      []AdapterDescriptor `json:"adapters"`
}

type ErrorNotice struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id,omitempty"`
	Error         string `json:"error"`
	AdvisoryOnly  bool   `json:"advisory_only"`
}
