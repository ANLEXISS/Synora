// Package delivery binds the durable outbox and dispatcher to the existing
// local bus. Only delivery metadata crosses the ACK boundary; business
// payloads are never copied into an ACK.
package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"synora/internal/dispatcher"
	"synora/internal/outbox"
	"synora/pkg/contract"
)

const AckMessageType = "delivery.ack"

type MessageSource interface {
	SubscribeChannel(string) <-chan contract.Message
}

type Publisher struct {
	Store      *outbox.Store
	Dispatcher *dispatcher.Dispatcher
	AckSource  MessageSource

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	start  bool
}

func New(path string, sender dispatcher.Sender, config dispatcher.Config, ackSource MessageSource) (*Publisher, error) {
	store, err := outbox.Open(path, config.Now)
	if err != nil {
		return nil, err
	}
	return NewWithStore(store, sender, config, ackSource)
}

func NewWithStore(store *outbox.Store, sender dispatcher.Sender, config dispatcher.Config, ackSource MessageSource) (*Publisher, error) {
	d, err := dispatcher.New(store, sender, config)
	if err != nil {
		return nil, err
	}
	return &Publisher{Store: store, Dispatcher: d, AckSource: ackSource}, nil
}

func (p *Publisher) Start(parent context.Context) error {
	if p == nil || p.Dispatcher == nil {
		return errors.New("delivery publisher is not configured")
	}
	if parent == nil {
		parent = context.Background()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.start {
		return errors.New("delivery publisher already started")
	}
	ctx, cancel := context.WithCancel(parent)
	if err := p.Dispatcher.Start(ctx); err != nil {
		cancel()
		return err
	}
	p.cancel = cancel
	p.done = make(chan struct{})
	p.start = true
	if p.AckSource != nil {
		go func() {
			defer close(p.done)
			_ = RunAckLoop(ctx, p.AckSource, p.Dispatcher)
		}()
	} else {
		close(p.done)
	}
	return nil
}

func (p *Publisher) Stop() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if !p.start {
		p.mu.Unlock()
		return nil
	}
	cancel, done := p.cancel, p.done
	p.mu.Unlock()
	cancel()
	if err := p.Dispatcher.Stop(); err != nil {
		return err
	}
	<-done
	p.mu.Lock()
	p.start = false
	p.mu.Unlock()
	return nil
}

func (p *Publisher) Publish(message contract.Message) error {
	if p == nil || p.Store == nil || p.Dispatcher == nil {
		return errors.New("delivery publisher is not configured")
	}
	if err := message.DeliveryIdentity().Validate(); err != nil {
		return err
	}
	if err := p.Store.Enqueue(message); err != nil {
		return err
	}
	p.Dispatcher.Wake()
	return nil
}

func NewAckMessage(source string, original contract.Message, ack contract.DeliveryAck) (contract.Message, error) {
	if err := ack.Validate(); err != nil {
		return contract.Message{}, err
	}
	if source == "" || original.Source == "" {
		return contract.Message{}, errors.New("ACK source and original source are required")
	}
	payload, err := json.Marshal(ack)
	if err != nil {
		return contract.Message{}, fmt.Errorf("marshal delivery ACK: %w", err)
	}
	return contract.Message{
		ID:            original.ID + ".ack",
		Version:       contract.RealtimeSchemaVersion,
		Type:          AckMessageType,
		Kind:          contract.KindEvent,
		Source:        source,
		Target:        original.Source,
		Epoch:         ack.Identity.Epoch,
		Sequence:      ack.Identity.Sequence,
		Revision:      ack.Identity.Revision,
		CorrelationID: original.ID,
		Payload:       payload,
	}, nil
}

func DecodeAck(message contract.Message) (contract.DeliveryAck, error) {
	if message.Type != AckMessageType {
		return contract.DeliveryAck{}, errors.New("not a delivery ACK")
	}
	var ack contract.DeliveryAck
	if err := json.Unmarshal(message.Payload, &ack); err != nil {
		return contract.DeliveryAck{}, fmt.Errorf("decode delivery ACK: %w", err)
	}
	if err := ack.Validate(); err != nil {
		return contract.DeliveryAck{}, err
	}
	return ack, nil
}

func RunAckLoop(ctx context.Context, source MessageSource, d *dispatcher.Dispatcher) error {
	if source == nil || d == nil {
		return errors.New("ACK loop is not configured")
	}
	channel := source.SubscribeChannel("")
	if channel == nil {
		return errors.New("ACK source returned a nil channel")
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case message, ok := <-channel:
			if !ok {
				return nil
			}
			if message.Type != AckMessageType {
				continue
			}
			ack, err := DecodeAck(message)
			if err != nil {
				continue
			}
			if err := d.HandleAck(ack); err != nil {
				continue
			}
		}
	}
}
