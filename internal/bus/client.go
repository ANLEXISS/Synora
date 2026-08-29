package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"time"

	"github.com/google/uuid"
	"synora/internal/resourcebudget"
	"synora/pkg/contract"
)

func NewClient(path string, service string) (*Client, error) {
	return NewClientWithConfig(path, service, ClientConfig{Auth: AuthConfigFromEnv()})
}

// ConnectContext retries startup connection failures at a bounded cadence and
// stops promptly when the owning service is shutting down.
func ConnectContext(ctx context.Context, path string, service string) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		client, err := NewClient(path, service)
		if err == nil {
			return client, nil
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

type ClientConfig struct {
	Auth AuthConfig
}

func NewClientWithConfig(path string, service string, cfg ClientConfig) (*Client, error) {
	if err := cfg.Auth.validate(); err != nil {
		return nil, err
	}
	c := &Client{
		address:  path,
		service:  service,
		auth:     cfg.Auth,
		pending:  make(map[string]chan pendingResponse),
		incoming: make(chan contract.Message, resourcebudget.BusIncomingQueue),
		closeCh:  make(chan struct{}),
		done:     make(chan struct{}),
	}

	if err := c.reconnect(); err != nil {
		return nil, err
	}

	go c.listen()

	return c, nil
}

func (c *Client) listen() {
	defer close(c.done)
	for {
		select {
		case <-c.closeCh:
			return
		default:
		}
		conn, err := c.ensureConn()
		if err != nil {
			select {
			case <-c.closeCh:
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		err = c.readLoop(conn)
		switch {
		case err == nil:
		case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
			log.Printf("bus disconnected: %s", c.service)
		default:
			log.Printf("bus listen error for %s: %v", c.service, err)
		}

		c.invalidateConn(conn)
		select {
		case <-c.closeCh:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// Close stops the client listener and closes its Unix connection. It is
// intentionally idempotent so hermetic callers can use t.Cleanup safely.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		close(c.closeCh)
		c.mu.Lock()
		conn := c.conn
		c.conn = nil
		c.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		c.failPending(errClientClosed)
	})
	<-c.done
	c.incomingOnce.Do(func() { close(c.incoming) })
	return nil
}

func (c *Client) readLoop(conn net.Conn) error {
	scanner := bufio.NewScanner(conn)

	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, maxFrameSize)

	for scanner.Scan() {
		var msg contract.Message

		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("bus decode error for %s: %v", c.service, err)
			return err
		}
		if err := verifyMessageAuthentication(msg, c.auth, time.Now().UTC(), 5*time.Minute); err != nil {
			log.Printf("bus authentication failed for %s: %v", c.service, err)
			return err
		}

		if c.deliverPending(msg) {
			continue
		}

		select {
		case c.incoming <- msg:
		default:
			log.Printf("bus incoming channel full for %s, dropping message %s", c.service, msg.Type)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return io.EOF
}

func (c *Client) Send(msg contract.Message) error {
	if c.isClosed() {
		return errClientClosed
	}
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}

	if msg.Source == "" {
		msg.Source = c.service
	}

	if msg.Kind == "" {
		msg.Kind = contract.KindEvent
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}

	if msg.SourceType == "" {
		msg.SourceType = inferSourceType(msg.Source)
	}
	msg, err := authenticateMessage(msg, c.auth, time.Now().UTC())
	if err != nil {
		return err
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	data = append(data, '\n')
	if len(data) > maxFrameSize {
		return errors.New("bus message exceeds maximum frame size")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := c.ensureConn()
		if err != nil {
			return err
		}

		c.writeMu.Lock()
		if deadlineErr := conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); deadlineErr != nil {
			c.writeMu.Unlock()
			return deadlineErr
		}
		err = writeFrame(conn, data)
		_ = conn.SetWriteDeadline(time.Time{})
		c.writeMu.Unlock()
		if err == nil {
			return nil
		}

		lastErr = err
		log.Printf("bus write failed for %s: %v", c.service, err)
		c.invalidateConn(conn)
	}

	if lastErr == nil {
		lastErr = errors.New("bus disconnected")
	}

	return lastErr
}

func (c *Client) Request(
	msgType string,
	source string,
	payload []byte,
	target string,
) (*contract.Message, error) {
	return c.RequestWithTimeout(msgType, source, payload, target, 5*time.Second)
}

// RequestWithTimeout is used by health and diagnostics probes so an unhealthy
// component cannot hold an HTTP request for the legacy five-second RPC limit.
func (c *Client) RequestWithTimeout(
	msgType string,
	source string,
	payload []byte,
	target string,
	timeout time.Duration,
) (*contract.Message, error) {
	id := uuid.New().String()

	msg := contract.Message{
		ID:      id,
		Type:    msgType,
		Kind:    contract.KindRPC,
		Source:  source,
		Target:  target,
		Payload: payload,
	}

	ch := make(chan pendingResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer c.removePending(id)

	if err := c.Send(msg); err != nil {
		return nil, err
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case response := <-ch:
		if response.err != nil {
			return nil, response.err
		}
		return &response.message, nil
	case <-timer.C:
		return nil, errors.New("bus timeout")
	}
}

func (c *Client) SubscribeChannel(_ string) <-chan contract.Message {
	return c.incoming
}

func (c *Client) ensureConn() (net.Conn, error) {
	if c.isClosed() {
		return nil, errClientClosed
	}
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn != nil {
		return conn, nil
	}

	if err := c.reconnect(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	conn = c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, errors.New("bus disconnected")
	}

	return conn, nil
}

func (c *Client) reconnect() error {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	c.mu.Lock()
	if c.conn != nil {
		c.mu.Unlock()
		return nil
	}

	now := time.Now()
	if !c.lastReconnectAttempt.IsZero() && now.Sub(c.lastReconnectAttempt) < 2*time.Second {
		err := c.lastReconnectErr
		c.mu.Unlock()
		if err == nil {
			return errors.New("bus reconnect in progress")
		}
		return err
	}
	c.lastReconnectAttempt = now
	c.mu.Unlock()

	log.Printf("bus reconnecting: %s", c.service)

	conn, err := net.Dial("unix", c.address)
	if err != nil {
		c.mu.Lock()
		c.lastReconnectErr = err
		c.mu.Unlock()
		log.Printf("bus reconnect failed for %s: %v", c.service, err)
		return err
	}

	if err := c.register(conn); err != nil {
		_ = conn.Close()
		c.mu.Lock()
		c.lastReconnectErr = err
		c.mu.Unlock()
		log.Printf("bus registration failed for %s: %v", c.service, err)
		return err
	}

	c.mu.Lock()
	old := c.conn
	c.conn = conn
	c.lastReconnectAttempt = time.Time{}
	c.lastReconnectErr = nil
	c.mu.Unlock()

	if old != nil && old != conn {
		_ = old.Close()
	}

	log.Printf("bus connected: %s", c.service)

	return nil
}

func (c *Client) register(conn net.Conn) error {
	reg := contract.Message{
		ID:         uuid.New().String(),
		Type:       "bus.register",
		Kind:       contract.KindCommand,
		Source:     c.service,
		SourceType: contract.SourceSystem,
		Timestamp:  time.Now().UTC(),
	}
	var err error
	reg, err = authenticateMessage(reg, c.auth, time.Now().UTC())
	if err != nil {
		return err
	}

	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}

	data = append(data, '\n')

	c.writeMu.Lock()
	if deadlineErr := conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); deadlineErr != nil {
		c.writeMu.Unlock()
		return deadlineErr
	}
	err = writeFrame(conn, data)
	_ = conn.SetWriteDeadline(time.Time{})
	c.writeMu.Unlock()
	if err != nil {
		return err
	}

	if err := conn.SetReadDeadline(time.Now().Add(clientWriteTimeout)); err != nil {
		return err
	}
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	var ack contract.Message
	if err := json.NewDecoder(conn).Decode(&ack); err != nil {
		return err
	}
	if err := verifyMessageAuthentication(ack, c.auth, time.Now().UTC(), 5*time.Minute); err != nil {
		return err
	}
	if ack.Type != "bus.registered" || ack.Target != c.service || ack.CorrelationID != reg.ID {
		return errors.New("invalid bus registration ACK")
	}
	return nil
}

func (c *Client) invalidateConn(conn net.Conn) {
	if conn == nil {
		return
	}

	c.mu.Lock()
	current := c.conn == conn
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()

	_ = conn.Close()
	if current {
		c.failPending(errors.New("bus disconnected"))
	}
}

func (c *Client) deliverPending(msg contract.Message) bool {
	c.mu.Lock()
	ch, ok := c.pending[msg.ID]
	if ok {
		delete(c.pending, msg.ID)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}

	select {
	case ch <- pendingResponse{message: msg}:
	default:
	}

	return true
}

func (c *Client) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan pendingResponse)
	c.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- pendingResponse{err: err}:
		default:
		}
	}
}

func (c *Client) isClosed() bool {
	select {
	case <-c.closeCh:
		return true
	default:
		return false
	}
}

func writeFrame(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		written, err := conn.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func (c *Client) removePending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func inferSourceType(source string) string {
	switch source {
	case "api", "actions", "bus", "core", "discovery", "runtime", "vision":
		return contract.SourceSystem
	case "lab", "synora-lab", "simulation":
		return contract.SourceSimulator
	default:
		return contract.SourceDevice
	}
}
