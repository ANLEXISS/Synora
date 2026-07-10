package bus

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestServerRoutesLabEventToCore(t *testing.T) {
	server := NewServer("net-pipe-test")
	server.debug = true

	coreConn, coreDecoder := registeredPipe(t, server, "core")
	defer coreConn.Close()
	labConn, _ := registeredPipe(t, server, "lab")
	defer labConn.Close()

	msg := contract.Message{
		Type:       contract.EventVisionUnknown,
		Kind:       contract.KindEvent,
		Source:     "lab",
		SourceType: contract.SourceSimulator,
		Target:     "core",
	}
	if err := json.NewEncoder(labConn).Encode(msg); err != nil {
		t.Fatalf("send lab event: %v", err)
	}

	gotCh := make(chan contract.Message, 1)
	go func() {
		var got contract.Message
		if err := coreDecoder.Decode(&got); err == nil {
			gotCh <- got
		}
	}()
	select {
	case got := <-gotCh:
		if got.Type != contract.EventVisionUnknown || got.Source != "lab" || got.Target != "core" {
			t.Fatalf("unexpected routed message: %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("core did not receive lab event")
	}
}

func TestValidateMessageRejectsSourceMismatch(t *testing.T) {
	err := validateMessage(contract.Message{
		Type:   contract.EventVisionMotion,
		Kind:   contract.KindEvent,
		Source: "cam_01",
		Target: "core",
	}, "lab")
	if err == nil || !strings.Contains(err.Error(), "source mismatch") {
		t.Fatalf("expected source mismatch, got %v", err)
	}
}

func registeredPipe(t *testing.T, server *Server, service string) (net.Conn, *json.Decoder) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	go server.handle(serverConn)
	registration := contract.Message{
		Type:   "bus.register",
		Kind:   contract.KindCommand,
		Source: service,
	}
	if err := json.NewEncoder(clientConn).Encode(registration); err != nil {
		t.Fatalf("register %s: %v", service, err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.getClient(service); ok {
			return clientConn, json.NewDecoder(clientConn)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("service %s was not registered", service)
	return nil, nil
}
