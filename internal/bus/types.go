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
	mu      sync.Mutex
}

type Server struct {
	address         string
	debug           bool
	allowedServices map[string]struct{}

	mu       sync.RWMutex
	clients  map[string]*ClientConn
	listener net.Listener
}
