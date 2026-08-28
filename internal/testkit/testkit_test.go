package testkit

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/discovery/vision"
	"synora/pkg/contract"
)

func TestClockIDsPathsAndMemoryBusAreDeterministic(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := NewClock(at)
	if !clock.Now().Equal(at) || !clock.Advance(time.Second).Equal(at.Add(time.Second)) {
		t.Fatalf("unexpected clock value: %v", clock.Now())
	}
	ids := NewIDs("event")
	if ids.Next() != "event-1" || ids.Next() != "event-2" {
		t.Fatalf("unexpected IDs")
	}
	path := WriteTempFile(t, "nested/fixture.json", []byte("{}"))
	if path == "" || filepath.Base(path) != "fixture.json" {
		t.Fatalf("unexpected temporary path: %q", path)
	}
	bus := NewMemoryBus()
	defer bus.Close()
	seen := bus.Subscribe(t, 1)
	message := contract.Message{ID: "event-1", Payload: []byte(`{"ok":true}`)}
	if err := bus.Publish(message); err != nil {
		t.Fatal(err)
	}
	got := <-seen
	got.Payload[0] = 'X'
	if messages := bus.Messages(); len(messages) != 1 || string(messages[0].Payload) != `{"ok":true}` {
		t.Fatalf("memory bus did not return defensive copy: %#v", messages)
	}
	if err := bus.Publish(contract.Message{}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(bus.Publish(contract.Message{}), ErrBufferOverflow) {
		t.Fatal("expected explicit subscriber overflow")
	}
}

func TestHTTPServerAndFakeVisionProcessor(t *testing.T) {
	server := HTTPServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(request.Method))
	}))
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	processor := &FakeVisionProcessor{Response: &vision.WorkerResponse{}}
	if _, err := processor.Process(nil); err != nil || processor.Calls != 1 {
		t.Fatalf("processor=%+v err=%v", processor, err)
	}
}
