package bus

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"time"

	"github.com/google/uuid"
	"synora/pkg/contract"
)

func NewClient(path string, service string) (*Client, error) {
	c := &Client{
		address:  path,
		service:  service,
		pending:  make(map[string]chan contract.Message),
		incoming: make(chan contract.Message, 100),
	}

	if err := c.reconnect(); err != nil {
		return nil, err
	}

	go c.listen()

	return c, nil
}

func (c *Client) listen() {
	for {
		conn, err := c.ensureConn()
		if err != nil {
			time.Sleep(2 * time.Second)
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
		time.Sleep(2 * time.Second)
	}
}

func (c *Client) readLoop(conn net.Conn) error {
	scanner := bufio.NewScanner(conn)

	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var msg contract.Message

		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("bus decode error for %s: %v", c.service, err)
			continue
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
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}

	if msg.Source == "" {
		msg.Source = c.service
	}

	if msg.Kind == "" {
		msg.Kind = contract.KindEvent
	}

	if msg.SourceType == "" {
		msg.SourceType = inferSourceType(msg.Source)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	data = append(data, '\n')

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		conn, err := c.ensureConn()
		if err != nil {
			return err
		}

		c.writeMu.Lock()
		_, err = conn.Write(data)
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
	id := uuid.New().String()

	msg := contract.Message{
		ID:      id,
		Type:    msgType,
		Kind:    contract.KindRPC,
		Source:  source,
		Target:  target,
		Payload: payload,
	}

	ch := make(chan contract.Message, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer c.removePending(id)

	if err := c.Send(msg); err != nil {
		return nil, err
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case resp := <-ch:
		return &resp, nil
	case <-timer.C:
		return nil, errors.New("bus timeout")
	}
}

func (c *Client) SubscribeChannel(_ string) <-chan contract.Message {
	return c.incoming
}

func (c *Client) ensureConn() (net.Conn, error) {
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
	}

	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}

	data = append(data, '\n')

	c.writeMu.Lock()
	_, err = conn.Write(data)
	c.writeMu.Unlock()
	return err
}

func (c *Client) invalidateConn(conn net.Conn) {
	if conn == nil {
		return
	}

	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()

	_ = conn.Close()
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
	case ch <- msg:
	default:
	}

	return true
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
