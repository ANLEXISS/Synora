package mqtt

import (
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type PahoPublisher struct {
	client paho.Client
}

func NewPahoPublisher(broker string, clientID string) (*PahoPublisher, error) {
	opts := paho.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID(clientID)
	opts.SetConnectTimeout(5 * time.Second)

	client := paho.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return nil, err
	}

	return &PahoPublisher{
		client: client,
	}, nil
}

func (p *PahoPublisher) Publish(topic string, payload []byte) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("mqtt publisher not configured")
	}

	token := p.client.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}
