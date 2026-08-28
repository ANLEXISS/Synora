package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestProtocolHelloRequestSerializationIsStable(t *testing.T) {
	encoded, err := json.Marshal(ProtocolHelloRequest{
		RequestID:       "request-1",
		Operation:       VisionProtocolHello,
		ProtocolVersion: VisionProtocolVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"request_id":"request-1","operation":"protocol.hello","protocol_version":"synora.vision.v1"}`; got != want {
		t.Fatalf("hello JSON=%s want=%s", got, want)
	}
}

func TestRuntimeNegotiatesAndAcceptsFragmentedProtocolResponse(t *testing.T) {
	server, path := startRuntimeProtocolServer(t, "normal", func(conn net.Conn) error {
		var request Request
		if err := json.NewDecoder(conn).Decode(&request); err != nil {
			return err
		}
		if request.Operation != VisionClipProcess || request.RequestID != "clip-1" {
			return fmt.Errorf("unexpected clip request: %#v", request)
		}
		return writeFragmented(conn, `{"request_id":"clip-1","events":[{"type":"vision.unknown","source":"vision-worker","timestamp":1,"payload":{"camera_id":"cam-1"}}]}`+"\n")
	})
	defer server.close()

	manager := NewWorkerManager(nil, WorkerManagerConfig{Executor: &runtimeTestExecutor{}})
	runtime := NewRuntimeWithManagerAndSocketTimeout(manager, path, time.Second)
	response, err := runtime.Process(&ClipJob{ID: "clip-1", CameraID: "cam-1", Path: "/tmp/clip.mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Events) != 1 || response.Events[0].Type != contract.EventVisionUnknown {
		t.Fatalf("unexpected response=%#v", response)
	}
	if got := runtime.Snapshot(); got.ProtocolVersion != VisionProtocolVersion || got.CapabilityStatus != "normal" {
		t.Fatalf("unexpected capability snapshot=%#v", got)
	}
	_ = runtime.Close()
	_ = manager.Stop("cam-1")
	server.wait(t)
}

func TestRuntimeRejectsMalformedOrMismatchedWorkerResponse(t *testing.T) {
	server, path := startRuntimeProtocolServer(t, "normal", func(conn net.Conn) error {
		var request Request
		if err := json.NewDecoder(conn).Decode(&request); err != nil {
			return err
		}
		return writeFragmented(conn, `{"request_id":"other-clip","events":[]}`+"\n")
	})
	defer server.close()

	manager := NewWorkerManager(nil, WorkerManagerConfig{Executor: &runtimeTestExecutor{}})
	runtime := NewRuntimeWithManagerAndSocketTimeout(manager, path, time.Second)
	_, err := runtime.Process(&ClipJob{ID: "clip-1", CameraID: "cam-1", Path: "/tmp/clip.mp4"})
	if !errors.Is(err, ErrVisionMalformedResponse) {
		t.Fatalf("error=%v, want malformed response", err)
	}
	_ = runtime.Close()
	_ = manager.Stop("cam-1")
	server.wait(t)
}

func TestRuntimeRefusesProcessingWhenWorkerCapabilitiesAreDegraded(t *testing.T) {
	server, path := startRuntimeProtocolServer(t, "degraded", nil)
	defer server.close()

	manager := NewWorkerManager(nil, WorkerManagerConfig{Executor: &runtimeTestExecutor{}})
	runtime := NewRuntimeWithManagerAndSocketTimeout(manager, path, time.Second)
	_, err := runtime.Process(&ClipJob{ID: "clip-1", CameraID: "cam-1", Path: "/tmp/clip.mp4"})
	if !errors.Is(err, ErrVisionWorkerDegraded) {
		t.Fatalf("error=%v, want degraded worker", err)
	}
	if got := runtime.Snapshot(); got.CapabilityStatus != "degraded" {
		t.Fatalf("capability status=%q", got.CapabilityStatus)
	}
	_ = runtime.Close()
	_ = manager.Stop("cam-1")
	server.wait(t)
}

type runtimeProtocolServer struct {
	listener net.Listener
	done     chan error
	onClip   func(net.Conn) error
	once     sync.Once
}

func startRuntimeProtocolServer(t *testing.T, status string, onClip func(net.Conn) error) (*runtimeProtocolServer, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "vision.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &runtimeProtocolServer{listener: listener, done: make(chan error, 1), onClip: onClip}
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			server.done <- err
			return
		}
		defer conn.Close()
		var hello ProtocolHelloRequest
		if err := json.NewDecoder(conn).Decode(&hello); err != nil {
			server.done <- err
			return
		}
		if hello.Operation != VisionProtocolHello || hello.ProtocolVersion != VisionProtocolVersion {
			server.done <- fmt.Errorf("unexpected hello: %#v", hello)
			return
		}
		helloResponse, _ := json.Marshal(ProtocolHelloResponse{
			RequestID: hello.RequestID, Operation: VisionProtocolHello,
			ProtocolVersion: VisionProtocolVersion, Status: status,
			Backend: "dry_run", EmbeddingDimension: ArcFaceEmbeddingDimension,
			Models: map[string]map[string]any{}, Capabilities: map[string]map[string]any{},
			FaceDataset: map[string]any{"status": "not_configured"},
		})
		if err := writeFragmented(conn, string(helloResponse)+"\n"); err != nil {
			server.done <- err
			return
		}
		if onClip != nil {
			server.done <- onClip(conn)
			return
		}
		server.done <- nil
	}()
	return server, path
}

func (s *runtimeProtocolServer) close() {
	s.once.Do(func() { _ = s.listener.Close() })
}

func (s *runtimeProtocolServer) wait(t *testing.T) {
	t.Helper()
	select {
	case err := <-s.done:
		if err != nil && !errors.Is(err, net.ErrClosed) && !strings.Contains(err.Error(), "closed") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("protocol server did not finish")
	}
}

func writeFragmented(conn net.Conn, value string) error {
	cut := len(value) / 2
	if _, err := conn.Write([]byte(value[:cut])); err != nil {
		return err
	}
	_, err := conn.Write([]byte(value[cut:]))
	return err
}

type runtimeTestExecutor struct {
	mu      sync.Mutex
	process *runtimeTestProcess
}

func (e *runtimeTestExecutor) Start(string, ...string) (ManagedProcess, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.process = &runtimeTestProcess{done: make(chan struct{})}
	return e.process, nil
}

type runtimeTestProcess struct {
	once sync.Once
	done chan struct{}
}

func (p *runtimeTestProcess) PID() int { return os.Getpid() }
func (p *runtimeTestProcess) Wait() error {
	<-p.done
	return nil
}
func (p *runtimeTestProcess) Signal(os.Signal) error {
	p.once.Do(func() { close(p.done) })
	return nil
}
func (p *runtimeTestProcess) Kill() error {
	p.once.Do(func() { close(p.done) })
	return nil
}
