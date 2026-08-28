package bus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"synora/pkg/contract"
)

const clientWriteTimeout = 2 * time.Second

func NewServer(address string) *Server {
	return NewServerWithConfig(address, ServerConfig{Auth: AuthConfigFromEnv()})
}

func NewServerWithConfig(address string, cfg ServerConfig) *Server {
	allowed := make(map[string]struct{})
	for _, service := range []string{"actions", "api", "connectivity", "core", "core-2", "discovery", "lab", "runtime-manager", "vision"} {
		allowed[service] = struct{}{}
	}
	if cfg.ReplayWindow <= 0 {
		cfg.ReplayWindow = 5 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Server{
		address:         address,
		clients:         make(map[string]*ClientConn),
		seenNonces:      make(map[string]time.Time),
		seenMessages:    make(map[string]messageReplay),
		allowedServices: allowed,
		auth:            cfg.Auth,
		replayWindow:    cfg.ReplayWindow,
		now:             cfg.Now,
		debug:           os.Getenv("SYNORA_BUS_DEBUG") == "1",
	}
}

func (s *Server) Start() error {
	if err := s.auth.validate(); err != nil {
		return err
	}

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
	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()
	configureSocket(s.address)

	log.Println("bus listening on", s.address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.RLock()
			closed := s.listener == nil
			s.mu.RUnlock()
			if closed {
				return nil
			}
			log.Println(err)
			continue
		}

		go s.handle(conn)
	}
}

// Close stops an embedded server and all registered connections. Production
// callers still use Start's blocking lifetime; tests can now own that
// lifetime without a process-wide bus or leaked accept goroutine.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	listener := s.listener
	s.listener = nil
	clients := make([]*ClientConn, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.clients = make(map[string]*ClientConn)
	s.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	for _, client := range clients {
		if client != nil && client.conn != nil {
			_ = client.conn.Close()
		}
	}
	return nil
}

func (s *Server) handle(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), maxFrameSize)
	var service string
	peer := peerCredentials(conn)

	for scanner.Scan() {
		var msg contract.Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("bus frame decode error for %s: %v", messageActor(service, ""), err)
			s.disconnect(service, conn, "invalid frame")
			return
		}

		if msg.Type == "bus.register" {
			if err := validateRegistration(msg, service); err != nil {
				log.Printf("bus service rejected: %s reason=%v", messageActor(service, msg.Source), err)
				s.disconnect(service, conn, "invalid registration")
				return
			}
			if err := verifyMessageAuthentication(msg, s.auth, s.now(), s.replayWindow); err != nil {
				log.Printf("bus service rejected: %s reason=%v", messageActor(service, msg.Source), err)
				s.disconnect(service, conn, "invalid registration authentication")
				return
			}
			if s.auth.enabled() {
				if err := s.rememberNonce(msg, s.now().UTC()); err != nil {
					log.Printf("bus service rejected: %s reason=%v", messageActor(service, msg.Source), err)
					s.disconnect(service, conn, "replayed registration")
					return
				}
			}
			if !processIdentityAllowed(peer, msg.Source) {
				log.Printf("bus service rejected: %s reason=peer credentials not authorized", messageActor(service, msg.Source))
				s.disconnect(service, conn, "unauthorized peer")
				return
			}
			if _, ok := s.allowedServices[msg.Source]; !ok {
				log.Printf("bus service rejected: %s reason=service not allowlisted", msg.Source)
				s.disconnect(service, conn, "service not allowlisted")
				return
			}
			service = msg.Source
			s.register(service, conn)
			if err := s.ackRegistration(service, conn, msg.ID); err != nil {
				log.Printf("bus registration ACK failed for %s: %v", service, err)
				s.disconnect(service, conn, "registration ACK failure")
				return
			}
			continue
		}

		if err := validateMessage(msg, service); err != nil {
			log.Printf("bus invalid message: source=%s target=%s kind=%s type=%s reason=%v", messageActor(service, msg.Source), msg.Target, msg.Kind, msg.Type, err)
			continue
		}
		if err := s.authorizeMessage(msg, service); err != nil {
			log.Printf("bus unauthorized message: source=%s target=%s kind=%s type=%s reason=%v", service, msg.Target, msg.Kind, msg.Type, err)
			continue
		}

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
	reason := "connection closed"
	if err := scanner.Err(); err != nil {
		reason = err.Error()
		if service == "" {
			log.Printf("bus: closing unregistered connection: %v", err)
		} else {
			log.Printf("bus read error for %s: %v", service, err)
		}
	}
	s.disconnect(service, conn, reason)
}

type peerCredential struct {
	pid   int
	uid   uint32
	gid   uint32
	known bool
}

func peerCredentials(conn net.Conn) peerCredential {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		// net.Pipe and other in-process transports have no peer credentials;
		// keeping them usable preserves hermetic unit tests while production
		// Unix sockets remain subject to the OS credential check.
		return peerCredential{}
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return peerCredential{}
	}
	var credentials *syscall.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		value, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			controlErr = err
			return
		}
		credentials = value
	}); err != nil {
		return peerCredential{}
	}
	if controlErr != nil || credentials == nil {
		return peerCredential{}
	}
	return peerCredential{pid: int(credentials.Pid), uid: credentials.Uid, gid: credentials.Gid, known: true}
}

func peerOwnedByProcess(conn net.Conn) bool {
	peer := peerCredentials(conn)
	return !peer.known || peer.uid == uint32(os.Getuid())
}

func processIdentityAllowed(peer peerCredential, service string) bool {
	if !peer.known {
		// net.Pipe has no kernel identity and is deliberately retained for
		// hermetic in-process tests; real Bus listeners are Unix sockets.
		return true
	}
	if peer.pid <= 0 {
		return false
	}
	executable, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(peer.pid), "exe"))
	if err != nil {
		return false
	}
	base := strings.ToLower(filepath.Base(executable))
	if strings.HasSuffix(base, ".test") && peer.pid == os.Getpid() {
		return true
	}
	for _, expected := range expectedExecutables(service) {
		if base == expected {
			return true
		}
	}
	return false
}

func expectedExecutables(service string) []string {
	switch service {
	case "actions":
		return []string{"synora-actions"}
	case "api":
		return []string{"synora-api"}
	case "connectivity":
		return []string{"synora-connect"}
	case "core", "core-2":
		return []string{"synora-core"}
	case "discovery":
		return []string{"synora-discovery"}
	case "runtime-manager":
		return []string{"synora-runtime-manager"}
	case "vision":
		return []string{"synora-vision", "synora-discovery"}
	case "lab":
		return []string{"synora-lab"}
	default:
		return nil
	}
}

func (s *Server) register(service string, conn net.Conn) {
	client := &ClientConn{name: service, conn: conn, encoder: json.NewEncoder(conn), auth: s.auth}
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

func (s *Server) getClient(service string) (*ClientConn, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client, ok := s.clients[service]
	return client, ok
}

func (s *Server) ackRegistration(service string, conn net.Conn, registrationID string) error {
	client, ok := s.getClient(service)
	if !ok || client.conn != conn {
		return errors.New("registration was replaced before ACK")
	}
	return client.send(contract.Message{
		ID:            registrationID,
		Type:          "bus.registered",
		Kind:          contract.KindRPC,
		Source:        "bus",
		Target:        service,
		CorrelationID: registrationID,
	})
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
	var err error
	msg, err = authenticateMessage(msg, c.auth, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(clientWriteTimeout)); err != nil {
		return err
	}
	defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()
	if err := c.encoder.Encode(msg); err != nil {
		return err
	}
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
