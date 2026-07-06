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
	Builder *Builder
	Bus     Sender
	Now     func() time.Time
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
	err = p.Bus.Send(contract.Message{
		ID:        idgen.New("msg"),
		Type:      "state.snapshot",
		Kind:      contract.KindEvent,
		Source:    "core",
		Target:    "api",
		Timestamp: now,
		Payload:   body,
	})
	if err != nil {
		log.Println("core: snapshot publish error", err)
	}
}
