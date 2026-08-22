// Package dispatcher drains the durable outbox without making the bus itself
// the source of truth. A successful Send only means the attempt reached the
// transport; the record remains in_flight until an explicit ACK arrives.
package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"synora/internal/outbox"
	"synora/pkg/contract"
)

type Sender interface {
	Send(contract.Message) error
}

// ContextSender is optional. It lets Stop cancel a blocked transport attempt;
// legacy Senders remain supported for compatibility.
type ContextSender interface {
	SendContext(context.Context, contract.Message) error
}

type Config struct {
	Interval          time.Duration
	AckTimeout        time.Duration
	BatchSize         int
	MaxAttempts       uint32
	BaseBackoff       time.Duration
	MaxBackoff        time.Duration
	Jitter            float64
	Now               func() time.Time
	Random            func() float64
	TerminalRetention time.Duration
	RetentionInterval time.Duration
}

func DefaultConfig() Config {
	return Config{
		Interval:          250 * time.Millisecond,
		AckTimeout:        5 * time.Second,
		BatchSize:         32,
		MaxAttempts:       8,
		BaseBackoff:       250 * time.Millisecond,
		MaxBackoff:        30 * time.Second,
		Jitter:            0.2,
		Now:               time.Now,
		Random:            func() float64 { return 0.5 },
		TerminalRetention: 7 * 24 * time.Hour,
		RetentionInterval: time.Hour,
	}
}

func (c *Config) normalize() error {
	if c.Interval <= 0 {
		c.Interval = 250 * time.Millisecond
	}
	if c.AckTimeout <= 0 {
		c.AckTimeout = 5 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 8
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 250 * time.Millisecond
	}
	if c.MaxBackoff < c.BaseBackoff {
		c.MaxBackoff = c.BaseBackoff
	}
	if c.Jitter < 0 || c.Jitter > 1 {
		return errors.New("dispatcher jitter must be between 0 and 1")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Random == nil {
		c.Random = func() float64 { return 0.5 }
	}
	if c.TerminalRetention <= 0 {
		c.TerminalRetention = 7 * 24 * time.Hour
	}
	if c.RetentionInterval <= 0 {
		c.RetentionInterval = time.Hour
	}
	return nil
}

type Dispatcher struct {
	store  *outbox.Store
	sender Sender
	config Config

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
	start  bool
}

func New(store *outbox.Store, sender Sender, config Config) (*Dispatcher, error) {
	if store == nil {
		return nil, errors.New("dispatcher outbox is required")
	}
	if sender == nil {
		return nil, errors.New("dispatcher sender is required")
	}
	if err := config.normalize(); err != nil {
		return nil, err
	}
	return &Dispatcher{store: store, sender: sender, config: config, wake: make(chan struct{}, 1)}, nil
}

func (d *Dispatcher) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.start {
		return errors.New("dispatcher already started")
	}
	ctx, cancel := context.WithCancel(parent)
	d.cancel = cancel
	d.done = make(chan struct{})
	d.start = true
	go d.loop(ctx)
	return nil
}

func (d *Dispatcher) Stop() error {
	d.mu.Lock()
	if !d.start {
		d.mu.Unlock()
		return nil
	}
	cancel, done := d.cancel, d.done
	d.mu.Unlock()
	cancel()
	<-done
	return nil
}

func (d *Dispatcher) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) HandleAck(ack contract.DeliveryAck) error {
	if err := ack.Validate(); err != nil {
		return err
	}
	if err := d.store.Ack(ack); err != nil {
		return fmt.Errorf("handle delivery ACK: %w", err)
	}
	d.Wake()
	return nil
}

func (d *Dispatcher) loop(ctx context.Context) {
	defer func() {
		d.mu.Lock()
		d.start = false
		d.mu.Unlock()
		close(d.done)
	}()
	ticker := time.NewTicker(d.config.Interval)
	defer ticker.Stop()
	lastRetention := time.Time{}
	for {
		now := d.config.Now().UTC()
		if lastRetention.IsZero() || now.Sub(lastRetention) >= d.config.RetentionInterval {
			_ = d.store.CompactTerminalBefore(now.Add(-d.config.TerminalRetention))
			lastRetention = now
		}
		d.requeueExpired(ctx)
		d.dispatchReady(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wake:
		}
	}
}

func (d *Dispatcher) requeueExpired(ctx context.Context) {
	now := d.config.Now().UTC()
	for _, record := range d.store.Snapshot() {
		if ctx.Err() != nil {
			return
		}
		if record.State != contract.DeliveryInFlight || now.Sub(record.UpdatedAt) < d.config.AckTimeout {
			continue
		}
		d.schedule(record, "ack timeout", now)
	}
}

func (d *Dispatcher) dispatchReady(ctx context.Context) {
	now := d.config.Now().UTC()
	for _, record := range d.store.Ready(now, d.config.BatchSize) {
		if ctx.Err() != nil {
			return
		}
		if err := d.store.MarkInFlight(record.Identity); err != nil {
			continue
		}
		err := d.send(ctx, record.Message)
		if err == nil {
			continue
		}
		d.schedule(record, err.Error(), d.config.Now().UTC())
	}
}

func (d *Dispatcher) send(ctx context.Context, message contract.Message) error {
	if sender, ok := d.sender.(ContextSender); ok {
		return sender.SendContext(ctx, message)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return d.sender.Send(message)
}

func (d *Dispatcher) schedule(record contract.DeliveryRecord, reason string, now time.Time) {
	if record.Attempts >= d.config.MaxAttempts {
		_ = d.store.Fail(record.Identity, reason)
		return
	}
	next := now.Add(Backoff(record.Attempts, d.config.BaseBackoff, d.config.MaxBackoff, d.config.Jitter, d.config.Random))
	_ = d.store.MarkRetry(record.Identity, reason, next)
}

// Backoff returns a bounded exponential delay. The attempt count starts at
// one because it is incremented when a record enters in_flight.
func Backoff(attempt uint32, base, maximum time.Duration, jitter float64, random func() float64) time.Duration {
	if base <= 0 {
		base = time.Millisecond
	}
	if maximum < base {
		maximum = base
	}
	if attempt == 0 {
		attempt = 1
	}
	delay := float64(base)
	for i := uint32(1); i < attempt && delay < float64(maximum); i++ {
		delay *= 2
	}
	if delay > float64(maximum) {
		delay = float64(maximum)
	}
	if jitter > 0 {
		value := 0.5
		if random != nil {
			value = random()
		}
		if value < 0 {
			value = 0
		}
		if value > 1 {
			value = 1
		}
		delay *= 1 + (value*2-1)*jitter
	}
	if delay > float64(maximum) {
		delay = float64(maximum)
	}
	if delay < float64(base)*(1-jitter) {
		delay = float64(base) * (1 - jitter)
	}
	return time.Duration(math.Max(0, delay))
}
