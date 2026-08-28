package bus

import (
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"synora/pkg/contract"
)

const maxFrameSize = 1024 * 1024

var errClientClosed = errors.New("bus client closed")

type pendingResponse struct {
	message contract.Message
	err     error
}

type Client struct {
	address string
	service string
	auth    AuthConfig

	mu          sync.Mutex
	writeMu     sync.Mutex
	reconnectMu sync.Mutex

	conn                 net.Conn
	lastReconnectAttempt time.Time
	lastReconnectErr     error

	pending map[string]chan pendingResponse

	incoming     chan contract.Message
	closeCh      chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
	incomingOnce sync.Once
}

type ClientConn struct {
	name    string
	conn    net.Conn
	encoder *json.Encoder
	auth    AuthConfig
	mu      sync.Mutex
}

type ServerConfig struct {
	Auth         AuthConfig
	ReplayWindow time.Duration
	Now          func() time.Time
}

type Server struct {
	address         string
	debug           bool
	allowedServices map[string]struct{}
	auth            AuthConfig
	replayWindow    time.Duration
	now             func() time.Time

	mu           sync.RWMutex
	clients      map[string]*ClientConn
	seenNonces   map[string]time.Time
	seenMessages map[string]messageReplay
	listener     net.Listener
}

type messageReplay struct {
	seenAt      time.Time
	fingerprint string
}
