package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"synora/pkg/contract"
)

func NewServer(address string) *Server {
	return &Server{address: address, clients: make(map[string]*ClientConn)}
}

func (s *Server) Start() error {

	if err := os.MkdirAll("/run/synora", 0755); err != nil {
		return err
	}

	if err := os.Remove(s.address); err != nil && !os.IsNotExist(err) {
		return err
	}

	listener, err := net.Listen("unix", s.address)
	if err != nil {
		return err
	}

	log.Println("bus listening on", s.address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}

		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	decoder := json.NewDecoder(conn)
	var service string

	for {
		var msg contract.Message
		if err := decoder.Decode(&msg); err != nil {
			reason := "connection closed"
			switch {
			case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
			case service == "":
				log.Printf("bus: closing unregistered connection: %v", err)
				reason = err.Error()
			default:
				log.Printf("bus read error for %s: %v", service, err)
				reason = err.Error()
			}
			s.disconnect(service, conn, reason)
			return
		}

		if msg.Type == "bus.register" {
			if err := validateRegistration(msg, service); err != nil {
				log.Printf("bus: invalid registration from %s: %v", messageActor(service, msg.Source), err)
				s.disconnect(service, conn, "invalid registration")
				return
			}
			service = msg.Source
			s.register(service, conn)
			continue
		}

		if err := validateMessage(msg, service); err != nil {
			log.Printf("bus: invalid message from %s: %v", messageActor(service, msg.Source), err)
			continue
		}

		s.touch(service, conn)
		if msg.Kind == contract.KindEvent && msg.Target == "" {
			log.Printf("bus broadcast: %s (%s)", msg.Source, msg.Type)
			s.broadcast(msg)
			continue
		}

		log.Printf("bus route: %s -> %s (%s)", msg.Source, msg.Target, msg.Type)
		target, ok := s.getClient(msg.Target)
		if !ok {
			log.Printf("bus: routing failed, target unavailable: %s for %s from %s", msg.Target, msg.Type, msg.Source)
			continue
		}
		if err := target.send(msg); err != nil {
			log.Printf("bus: routing failed to %s for %s: %v", msg.Target, msg.Type, err)
			s.disconnect(msg.Target, target.conn, "write failure")
		}
	}
}

func (s *Server) register(service string, conn net.Conn) {
	client := &ClientConn{name: service, conn: conn, lastSeen: time.Now(), encoder: json.NewEncoder(conn)}
	s.mu.Lock()
	previous := s.clients[service]
	s.clients[service] = client
	s.mu.Unlock()
	if previous != nil && previous.conn != conn {
		log.Printf("bus: service name conflict for %s, replacing connection", service)
		_ = previous.conn.Close()
	}
	log.Println("bus: registered service", service)
}

func (s *Server) disconnect(service string, conn net.Conn, reason string) {
	if conn == nil {
		return
	}
	removed := false
	s.mu.Lock()
	if service != "" {
		if current, ok := s.clients[service]; ok && current.conn == conn {
			delete(s.clients, service)
			removed = true
		}
	}
	s.mu.Unlock()
	_ = conn.Close()
	if service == "" {
		log.Printf("bus: closed unregistered connection (%s)", reason)
		return
	}
	if removed {
		log.Printf("bus: disconnected service %s (%s)", service, reason)
		return
	}
	log.Printf("bus: closed stale connection for %s (%s)", service, reason)
}

func (s *Server) touch(service string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	client, ok := s.clients[service]
	if !ok || client.conn != conn {
		return
	}
	client.lastSeen = time.Now()
}

func (s *Server) getClient(service string) (*ClientConn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client, ok := s.clients[service]
	return client, ok
}

func (s *Server) broadcast(msg contract.Message) {
	s.mu.RLock()
	clients := make([]*ClientConn, 0, len(s.clients))
	for name, client := range s.clients {
		if name == msg.Source || client == nil {
			continue
		}
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	for _, client := range clients {
		if err := client.send(msg); err != nil {
			log.Printf("bus: broadcast failed to %s for %s: %v", client.name, msg.Type, err)
			s.disconnect(client.name, client.conn, "broadcast write failure")
		}
	}
}

func (c *ClientConn) send(msg contract.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.encoder.Encode(msg); err != nil {
		return err
	}
	c.lastSeen = time.Now()
	return nil
}

func validateRegistration(msg contract.Message, registeredService string) error {
	if msg.Source == "" {
		return errors.New("service name required")
	}
	if msg.Type == "" {
		return errors.New("message type required")
	}
	if msg.Kind != contract.KindCommand {
		return fmt.Errorf("invalid registration kind %q", msg.Kind)
	}
	if registeredService != "" && registeredService != msg.Source {
		return fmt.Errorf("service rename not allowed: %s to %s", registeredService, msg.Source)
	}
	return nil
}

func validateMessage(msg contract.Message, registeredService string) error {
	if registeredService == "" {
		return errors.New("unregistered connection")
	}
	if msg.Source == "" {
		return errors.New("message source required")
	}
	if msg.Source != registeredService {
		return fmt.Errorf("source mismatch: %s != %s", msg.Source, registeredService)
	}
	if msg.Type == "" {
		return errors.New("message type required")
	}
	switch msg.Kind {
	case contract.KindEvent, contract.KindCommand, contract.KindRPC:
	default:
		return fmt.Errorf("invalid message kind %q", msg.Kind)
	}
	if msg.Target == "" {
		if msg.Kind == contract.KindRPC {
			return errors.New("rpc target required")
		}
		if msg.Kind == contract.KindCommand {
			return errors.New("command target required")
		}
	}
	return nil
}

func messageActor(service string, source string) string {
	if source != "" {
		return source
	}
	if service != "" {
		return service
	}
	return "unknown"
}
