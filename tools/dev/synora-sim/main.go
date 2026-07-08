package main

import (
	"encoding/json"
	"time"

	"synora/internal/bus"
	"synora/internal/idgen"
	"synora/pkg/contract"
)

func main() {

	client, err := bus.NewClient("/run/synora/bus.sock", "vision-worker")
	if err != nil {
		panic(err)
	}

	payload := map[string]any{
		"device_id":  "cam_01",
		"identity":   "alexis",
		"confidence": 0.92,
	}

	body, _ := json.Marshal(payload)

	msg := contract.Message{
		ID:        idgen.New("msg"),
		Type:      "vision.identity",
		Kind:      contract.KindEvent,
		Source:    "vision-worker",
		Target:    "core",
		Timestamp: time.Now().UTC(),
		Priority:  contract.PriorityNormal,
		Payload:   body,
	}

	client.Send(msg)
}
