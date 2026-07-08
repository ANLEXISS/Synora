package mqtt_bridge

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	client mqtt.Client
}

func NewClient(broker string) *Client {

	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("synora-core")

	client := mqtt.NewClient(opts)

	return &Client{
		client: client,
	}
}

func (c *Client) Connect() error {

	token := c.client.Connect()
	token.Wait()

	return token.Error()
}

func (c *Client) Subscribe(topic string, handler mqtt.MessageHandler) error {

	token := c.client.Subscribe(topic, 0, handler)
	token.Wait()

	return token.Error()
}
