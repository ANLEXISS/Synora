package bus

import (
	"encoding/json"
	"net"
	"sync"
	"time"

	"synora/pkg/contract"
)

type Client struct {
	address string
	service string

	mu          sync.Mutex
	writeMu     sync.Mutex
	reconnectMu sync.Mutex

	conn                 net.Conn
	lastReconnectAttempt time.Time
	lastReconnectErr     error

	pending map[string]chan contract.Message

	incoming     chan contract.Message
	closeCh      chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
	incomingOnce sync.Once
}

type ClientConn struct {
	name     string
	conn     net.Conn
	lastSeen time.Time
	encoder  *json.Encoder
	mu       sync.Mutex
}

type Server struct {
	address string
	debug   bool

	mu       sync.RWMutex
	clients  map[string]*ClientConn
	listener net.Listener
}
