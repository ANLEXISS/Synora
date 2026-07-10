package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"synora/pkg/contract"
)

func NewServer(address string) *Server {
	return &Server{
		address: address,
		clients: make(map[string]*ClientConn),
		debug:   os.Getenv("SYNORA_BUS_DEBUG") == "1",
	}
}

func (s *Server) Start() error {

	if err := os.MkdirAll(filepath.Dir(s.address), 0770); err != nil {
		return err
	}
	configureRunDir(filepath.Dir(s.address))

	if err := os.Remove(s.address); err != nil && !os.IsNotExist(err) {
		return err
	}

	listener, err := net.Listen("unix", s.address)
	if err != nil {
		return err
	}
	configureSocket(s.address)

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
				log.Printf("bus service rejected: %s reason=%v", messageActor(service, msg.Source), err)
				s.disconnect(service, conn, "invalid registration")
				return
			}
			service = msg.Source
			s.register(service, conn)
			continue
		}

		if err := validateMessage(msg, service); err != nil {
			log.Printf("bus invalid message: source=%s target=%s kind=%s type=%s reason=%v", messageActor(service, msg.Source), msg.Target, msg.Kind, msg.Type, err)
			continue
		}

		s.touch(service, conn)
		s.debugf("bus message received: source=%s target=%s kind=%s type=%s", msg.Source, msg.Target, msg.Kind, msg.Type)
		if msg.Kind == contract.KindEvent && msg.Target == "" {
			s.debugf("bus broadcast: source=%s type=%s", msg.Source, msg.Type)
			s.broadcast(msg)
			continue
		}

		target, ok := s.getClient(msg.Target)
		if !ok {
			log.Printf("bus route failed: source=%s target=%s type=%s reason=target unavailable", msg.Source, msg.Target, msg.Type)
			continue
		}
		if err := target.send(msg); err != nil {
			log.Printf("bus route failed: source=%s target=%s type=%s reason=%v", msg.Source, msg.Target, msg.Type, err)
			s.disconnect(msg.Target, target.conn, "write failure")
			continue
		}
		s.debugf("bus route ok: source=%s target=%s type=%s", msg.Source, msg.Target, msg.Type)
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
	log.Printf("bus service registered: %s", service)
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

func (s *Server) debugf(format string, args ...any) {
	if s.debug {
		log.Printf(format, args...)
	}
}

func configureRunDir(path string) {
	if filepath.Base(path) != "synora" {
		return
	}
	gid, ok := synoraGroupID()
	if ok {
		if err := os.Chown(path, -1, gid); err != nil && !errors.Is(err, os.ErrPermission) {
			log.Printf("bus: run dir chgrp warning path=%s err=%v", path, err)
		}
	}
	if err := os.Chmod(path, 0770); err != nil {
		log.Printf("bus: run dir chmod warning path=%s err=%v", path, err)
	}
}

func configureSocket(path string) {
	gid, ok := synoraGroupID()
	if ok {
		if err := os.Chown(path, -1, gid); err != nil && !errors.Is(err, os.ErrPermission) {
			log.Printf("bus: socket chgrp warning path=%s err=%v", path, err)
		}
	} else {
		log.Printf("bus: group synora not found; socket access may be limited")
	}
	mode := os.FileMode(0660)
	if raw := os.Getenv("SYNORA_BUS_SOCKET_MODE"); raw != "" {
		if parsed, err := strconv.ParseUint(raw, 8, 32); err == nil {
			mode = os.FileMode(parsed)
		} else {
			log.Printf("bus: invalid SYNORA_BUS_SOCKET_MODE=%q", raw)
		}
	}
	if err := os.Chmod(path, mode); err != nil {
		log.Printf("bus: socket chmod warning path=%s mode=%#o err=%v", path, mode, err)
	}
}

func synoraGroupID() (int, bool) {
	group, err := user.LookupGroup("synora")
	if err != nil {
		return 0, false
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return 0, false
	}
	return gid, true
}
