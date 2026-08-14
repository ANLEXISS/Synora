package snapshot

import (
	"encoding/json"
	"log"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Sender interface {
	Send(contract.Message) error
}

type Publisher struct {
	Builder  *Builder
	Bus      Sender
	Now      func() time.Time
	Metadata func() (epoch string, sequence uint64, revision uint64)
}

func (p Publisher) PublishStateSnapshot() {
	if p.Builder == nil || p.Bus == nil {
		return
	}
	body, err := json.Marshal(p.Builder.StatePayload())
	if err != nil {
		log.Println("core: snapshot marshal error", err)
		return
	}
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	message := contract.Message{
		ID:        idgen.New("msg"),
		Version:   contract.RealtimeSchemaVersion,
		Type:      "state.snapshot",
		Kind:      contract.KindEvent,
		Source:    "core",
		Target:    "api",
		Timestamp: now,
		Payload:   body,
	}
	if p.Metadata != nil {
		message.Epoch, message.Sequence, message.Revision = p.Metadata()
	}
	err = p.Bus.Send(message)
	if err != nil {
		log.Println("core: snapshot publish error", err)
	}
}
