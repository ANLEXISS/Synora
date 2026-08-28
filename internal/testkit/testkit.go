// Package testkit contains deterministic, hermetic helpers shared by V1 tests.
// It intentionally has no dependency on the host filesystem, system services,
// network interfaces, model runtimes, or wall-clock based identifiers.
package testkit

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/discovery/vision"
	"synora/pkg/contract"
)

var (
	ErrBusClosed      = errors.New("testkit: memory bus closed")
	ErrInvalidPath    = errors.New("testkit: invalid temporary path")
	ErrBufferOverflow = errors.New("testkit: memory bus subscriber buffer full")
)

// Clock is a mutex-protected deterministic clock suitable for concurrent
// tests. It implements the Now method used by runtime components.
type Clock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewClock(at time.Time) *Clock {
	return &Clock{now: at.UTC()}
}

func (c *Clock) Now() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *Clock) Advance(delta time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
	return c.now
}

// IDs is a deterministic, concurrency-safe identifier generator.
type IDs struct {
	mu     sync.Mutex
	prefix string
	next   uint64
}

func NewIDs(prefix string) *IDs {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "id"
	}
	return &IDs{prefix: prefix}
}

func (g *IDs) Next() string {
	if g == nil {
		return "id-1"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next++
	return g.prefix + "-" + formatUint(g.next)
}

func formatUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// TempDir returns a test-owned directory. t.TempDir provides cleanup and
// keeps every generated path outside production/system directories.
func TempDir(t testing.TB) string {
	t.Helper()
	return t.TempDir()
}

// TempPath returns a path below a test-owned directory and rejects traversal.
func TempPath(t testing.TB, name string) string {
	t.Helper()
	clean := filepath.Clean(name)
	if name == "" || filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		t.Fatalf("%v: %q", ErrInvalidPath, name)
	}
	return filepath.Join(t.TempDir(), clean)
}

func WriteTempFile(t testing.TB, name string, data []byte) string {
	t.Helper()
	path := TempPath(t, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create temporary parent: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write temporary file: %v", err)
	}
	return path
}

// MemoryBus records messages and optionally broadcasts copies to bounded
// subscribers. It has no goroutine; cleanup is explicit and deterministic.
type MemoryBus struct {
	mu          sync.Mutex
	messages    []contract.Message
	subscribers map[uint64]chan contract.Message
	next        uint64
	closed      bool
}

func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subscribers: make(map[uint64]chan contract.Message)}
}

func (b *MemoryBus) Publish(message contract.Message) error {
	if b == nil {
		return ErrBusClosed
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBusClosed
	}
	message.Payload = append([]byte(nil), message.Payload...)
	b.messages = append(b.messages, message)
	for _, subscriber := range b.subscribers {
		copyMessage := message
		copyMessage.Payload = append([]byte(nil), message.Payload...)
		select {
		case subscriber <- copyMessage:
		default:
			return ErrBufferOverflow
		}
	}
	return nil
}

func (b *MemoryBus) Messages() []contract.Message {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]contract.Message, len(b.messages))
	for index, message := range b.messages {
		result[index] = message
		result[index].Payload = append([]byte(nil), message.Payload...)
	}
	return result
}

func (b *MemoryBus) Subscribe(t testing.TB, buffer int) <-chan contract.Message {
	t.Helper()
	if buffer < 1 {
		t.Fatalf("memory bus subscriber buffer must be positive")
	}
	channel := make(chan contract.Message, buffer)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		t.Fatalf("subscribe to closed memory bus")
	}
	b.next++
	id := b.next
	b.subscribers[id] = channel
	b.mu.Unlock()
	t.Cleanup(func() {
		b.mu.Lock()
		if current, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(current)
		}
		b.mu.Unlock()
	})
	return channel
}

func (b *MemoryBus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, subscriber := range b.subscribers {
		close(subscriber)
		delete(b.subscribers, id)
	}
}

// FakeVisionProcessor is the Go-side deterministic Vision boundary.
type FakeVisionProcessor struct {
	mu       sync.Mutex
	Response *vision.WorkerResponse
	Err      error
	Calls    int
}

func (p *FakeVisionProcessor) Process(_ *vision.ClipJob) (*vision.WorkerResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Calls++
	if p.Err != nil {
		return nil, p.Err
	}
	if p.Response == nil {
		return &vision.WorkerResponse{}, nil
	}
	copyResponse := *p.Response
	copyResponse.Events = append([]vision.Event(nil), p.Response.Events...)
	return &copyResponse, nil
}

// HTTPServer creates an httptest server and registers its close with the test.
func HTTPServer(t testing.TB, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// Body returns a fresh reader for request fixtures without sharing mutable
// state between calls.
func Body(data []byte) *bytes.Reader { return bytes.NewReader(append([]byte(nil), data...)) }
