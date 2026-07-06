package actions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"synora/pkg/contract"
)

type recordingBus struct {
	messages []contract.Message
}

func (b *recordingBus) Send(msg contract.Message) error {
	b.messages = append(b.messages, msg)
	return nil
}

type recordingExecutor struct {
	calls int

	result ExecutionResult

	err error
}

func (e *recordingExecutor) Execute(_ context.Context, _ contract.ActionRequest) (ExecutionResult, error) {
	e.calls++
	return e.result, e.err
}

func TestServiceExecutesActionRequestAndPublishesResult(t *testing.T) {
	payload, err := json.Marshal(contract.ActionRequest{
		ID:             "act-1",
		RequestID:      "req-1",
		CorrelationID:  "corr-1",
		Source:         "core",
		Target:         "actions",
		IdempotencyKey: "idem-1",
		Action: contract.Action{
			Device:  "siren-1",
			Command: "on",
			Value:   true,
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	bus := &recordingBus{}
	executor := &recordingExecutor{
		result: ExecutionResult{
			Status:  StatusAccepted,
			Details: map[string]any{"adapter": "fake"},
		},
	}
	service := &Service{
		Bus:      bus,
		Executor: executor,
		Deduper:  NewDeduper(),
		Now:      func() time.Time { return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC) },
		NewID:    func(prefix string) string { return prefix + "-test" },
	}

	service.HandleMessage(context.Background(), contract.Message{
		ID:        "msg-1",
		Type:      contract.EventActionRequest,
		Kind:      contract.KindCommand,
		Source:    "core",
		Target:    "actions",
		Timestamp: time.Date(2026, 7, 4, 9, 59, 0, 0, time.UTC),
		Payload:   payload,
	})

	if executor.calls != 1 {
		t.Fatalf("expected executor call, got %d", executor.calls)
	}
	result := decodeOnlyResult(t, bus)
	if result.Status != StatusAccepted || result.ActionID != "act-1" || result.RequestID != "req-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestServiceDeduplicatesByIdempotencyKey(t *testing.T) {
	payload, err := json.Marshal(contract.ActionRequest{
		ID:             "act-1",
		RequestID:      "req-1",
		IdempotencyKey: "idem-1",
		Action: contract.Action{
			Device:  "siren-1",
			Command: "on",
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	bus := &recordingBus{}
	executor := &recordingExecutor{}
	service := &Service{
		Bus:      bus,
		Executor: executor,
		Deduper:  NewDeduper(),
	}
	msg := contract.Message{
		ID:      "msg-1",
		Type:    contract.EventActionRequest,
		Kind:    contract.KindCommand,
		Source:  "core",
		Payload: payload,
	}

	service.HandleMessage(context.Background(), msg)
	service.HandleMessage(context.Background(), msg)

	if executor.calls != 1 {
		t.Fatalf("expected one executor call, got %d", executor.calls)
	}
	if len(bus.messages) != 2 {
		t.Fatalf("expected two results, got %d", len(bus.messages))
	}

	var duplicate contract.ActionResult
	if err := json.Unmarshal(bus.messages[1].Payload, &duplicate); err != nil {
		t.Fatalf("unmarshal duplicate result: %v", err)
	}
	if duplicate.Status != StatusDuplicate {
		t.Fatalf("expected duplicate result, got %#v", duplicate)
	}
}

func TestServiceAcceptsLegacyActionPayload(t *testing.T) {
	payload, err := json.Marshal(contract.Action{
		Device:  "light-1",
		Command: "off",
	})
	if err != nil {
		t.Fatalf("marshal action: %v", err)
	}

	bus := &recordingBus{}
	executor := &recordingExecutor{}
	service := &Service{
		Bus:      bus,
		Executor: executor,
		Deduper:  NewDeduper(),
	}

	service.HandleMessage(context.Background(), contract.Message{
		ID:      "msg-legacy",
		Type:    contract.EventActionRequest,
		Kind:    contract.KindCommand,
		Source:  "core",
		Payload: payload,
	})

	if executor.calls != 1 {
		t.Fatalf("expected legacy executor call, got %d", executor.calls)
	}
	result := decodeOnlyResult(t, bus)
	if result.Status != StatusAccepted || result.ActionID != "msg-legacy" {
		t.Fatalf("unexpected legacy result: %#v", result)
	}
}

func TestServicePublishesFailedResult(t *testing.T) {
	payload, err := json.Marshal(contract.ActionRequest{
		ID: "act-1",
		Action: contract.Action{
			Device:  "siren-1",
			Command: "on",
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	bus := &recordingBus{}
	service := &Service{
		Bus:      bus,
		Executor: &recordingExecutor{err: errors.New("boom")},
		Deduper:  NewDeduper(),
	}

	service.HandleMessage(context.Background(), contract.Message{
		ID:      "msg-1",
		Type:    contract.EventActionRequest,
		Kind:    contract.KindCommand,
		Source:  "core",
		Payload: payload,
	})

	result := decodeOnlyResult(t, bus)
	if result.Status != StatusFailed || result.Error != "boom" {
		t.Fatalf("unexpected failed result: %#v", result)
	}
}

func decodeOnlyResult(t *testing.T, bus *recordingBus) contract.ActionResult {
	t.Helper()

	if len(bus.messages) != 1 {
		t.Fatalf("expected one result message, got %d", len(bus.messages))
	}
	msg := bus.messages[0]
	if msg.Type != contract.EventActionResult || msg.Kind != contract.KindEvent || msg.Target != "core" {
		t.Fatalf("unexpected result message: %#v", msg)
	}

	var result contract.ActionResult
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return result
}
