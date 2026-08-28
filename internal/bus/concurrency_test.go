package bus

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestServerAcceptsFragmentedAndConcatenatedFrames(t *testing.T) {
	server := NewServer("net-pipe-framing")
	coreConn, coreDecoder := registeredPipe(t, server, "core")
	defer coreConn.Close()
	labConn, _ := registeredPipe(t, server, "lab")
	defer labConn.Close()

	first, err := json.Marshal(contract.Message{ID: "event-1", Type: contract.EventVisionMotion, Kind: contract.KindEvent, Source: "lab", Target: "core"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(contract.Message{ID: "event-2", Type: contract.EventVisionUnknown, Kind: contract.KindEvent, Source: "lab", Target: "core"})
	if err != nil {
		t.Fatal(err)
	}
	frames := append(append(append([]byte{}, first[:7]...), first[7:]...), '\n')
	frames = append(frames, second...)
	frames = append(frames, '\n')
	writeFragments(t, labConn, frames, []int{1, 3, 11, len(frames)})

	for _, want := range []string{"event-1", "event-2"} {
		var got contract.Message
		if err := coreConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := coreDecoder.Decode(&got); err != nil {
			t.Fatalf("decode routed frame %s: %v", want, err)
		}
		if got.ID != want {
			t.Fatalf("got frame %q, want %q", got.ID, want)
		}
	}
}

func TestServerClosesOversizedFrame(t *testing.T) {
	server := NewServer("net-pipe-oversized")
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go server.handle(serverConn)
	registration := contract.Message{Type: "bus.register", Kind: contract.KindCommand, Source: "lab"}
	if err := json.NewEncoder(clientConn).Encode(registration); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.getClient("lab"); ok {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := server.getClient("lab"); !ok {
		t.Fatal("service was not registered")
	}
	frame := []byte(`{"id":"large","type":"vision.motion","kind":"event","source":"lab","target":"core","payload":"` + strings.Repeat("x", maxFrameSize) + `"}` + "\n")
	_ = clientConn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = clientConn.Write(frame)
	_ = clientConn.SetWriteDeadline(time.Time{})
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.getClient("lab"); !ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("oversized frame did not close the service")
}

func TestClientSerializesConcurrentWrites(t *testing.T) {
	server, path := startUnixServer(t)
	coreConn, coreDecoder := registeredPipe(t, server, "core")
	defer coreConn.Close()
	client, err := NewClient(path, "lab")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitFor(t, time.Second, func() bool {
		_, ok := server.getClient("lab")
		return ok
	})

	const count = 32
	errs := make(chan error, count)
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			errs <- client.Send(contract.Message{ID: "concurrent-" + strconv.Itoa(i), Type: contract.EventVisionMotion, Kind: contract.KindEvent, Target: "core"})
		}(i)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent send failed: %v", err)
		}
	}

	if err := coreConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, count)
	for i := 0; i < count; i++ {
		var message contract.Message
		if err := coreDecoder.Decode(&message); err != nil {
			t.Fatalf("decode concurrent frame %d: %v", i, err)
		}
		seen[message.ID] = true
	}
	if len(seen) != count {
		t.Fatalf("received %d unique concurrent frames, want %d", len(seen), count)
	}
}

func TestClientFailsPendingRPCWhenDisconnected(t *testing.T) {
	server, path := startUnixServer(t)
	client, err := NewClient(path, "api")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitFor(t, time.Second, func() bool {
		_, ok := server.getClient("api")
		return ok
	})

	result := make(chan error, 1)
	go func() {
		_, requestErr := client.RequestWithTimeout("health.check", "api", nil, "core", 5*time.Second)
		result <- requestErr
	}()
	waitFor(t, time.Second, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return len(client.pending) == 1
	})
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil || strings.Contains(err.Error(), "timeout") {
			t.Fatalf("pending RPC did not receive a disconnect error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending RPC waited for timeout after disconnect")
	}
}

func TestClientReconnectsAfterServerRestart(t *testing.T) {
	server, path := startUnixServer(t)
	client, err := NewClient(path, "api")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	waitFor(t, time.Second, func() bool {
		_, ok := server.getClient("api")
		return ok
	})
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	server, _ = startUnixServerAt(t, path)
	defer server.Close()
	waitFor(t, 6*time.Second, func() bool {
		_, ok := server.getClient("api")
		return ok
	})
}

func startUnixServer(t *testing.T) (*Server, string) {
	t.Helper()
	return startUnixServerAt(t, filepath.Join(t.TempDir(), "bus.sock"))
}

func startUnixServerAt(t *testing.T, path string) (*Server, string) {
	t.Helper()
	server := NewServer(path)
	go func() { _ = server.Start() }()
	waitFor(t, time.Second, func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
	t.Cleanup(func() { _ = server.Close() })
	return server, path
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func writeFragments(t *testing.T, conn net.Conn, data []byte, ends []int) {
	t.Helper()
	start := 0
	for _, end := range ends {
		if end <= start || end > len(data) {
			continue
		}
		if _, err := conn.Write(data[start:end]); err != nil {
			t.Fatal(err)
		}
		start = end
	}
	if start < len(data) {
		if _, err := conn.Write(data[start:]); err != nil {
			t.Fatal(err)
		}
	}
}
