package main

import (
	"context"
	"errors"
	"testing"

	"synora/pkg/contract"
)

type lifecycleBus struct {
	messages chan contract.Message
}

func (b *lifecycleBus) Send(contract.Message) error { return nil }

func (b *lifecycleBus) SubscribeChannel(string) <-chan contract.Message {
	return b.messages
}

func TestRunBusLoopContextStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app := &coreApp{
		bus:         &lifecycleBus{messages: make(chan contract.Message)},
		processStop: make(chan struct{}),
	}

	err := app.runBusLoopContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runBusLoopContext error = %v, want context.Canceled", err)
	}
}
