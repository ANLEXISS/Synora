package mqtt

type PublishedMessage struct {
	Topic string

	Payload []byte
}

type FakePublisher struct {
	Messages []PublishedMessage

	Err error
}

func (p *FakePublisher) Publish(topic string, payload []byte) error {
	if p.Err != nil {
		return p.Err
	}

	copied := append([]byte(nil), payload...)
	p.Messages = append(p.Messages, PublishedMessage{
		Topic:   topic,
		Payload: copied,
	})
	return nil
}
