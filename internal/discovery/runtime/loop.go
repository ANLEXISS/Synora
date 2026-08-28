package runtime

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

const (
	DeviceTimeout = 30 * time.Second

	Tick = 5 * time.Second
)

type Publisher interface {
	Send(msg contract.Message) error
}

func StartLoop(
	registry *Registry,
	publisher Publisher,
) {
	StartLoopContext(context.Background(), registry, publisher)
}

func StartLoopContext(
	ctx context.Context,
	registry *Registry,
	publisher Publisher,
) {
	if ctx == nil {
		ctx = context.Background()
	}

	ticker := time.NewTicker(
		Tick,
	)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()

			if registry == nil {
				continue
			}
			registry.ForEachLocked(func(device *Device) {

				if !device.Online {
					return
				}

				if now.Sub(device.LastSeen) < DeviceTimeout {
					return
				}

				device.Online = false

				log.Printf(
					"device offline id=%s",
					device.ID,
				)

				payload, err := json.Marshal(map[string]any{
					"device_id": device.ID,
					"camera_id": device.ID,
					"type":      device.Type,
					"online":    false,
					"timestamp": now,
				})

				if err != nil {

					log.Printf(
						"runtime payload error device=%s err=%v",
						device.ID,
						err,
					)

					return
				}

				if publisher != nil {
					err = publisher.Send(contract.Message{
						ID:        idgen.New("msg"),
						Type:      contract.EventDeviceOffline,
						Kind:      contract.KindEvent,
						Source:    "discovery",
						Target:    "core",
						Timestamp: now,
						Payload:   payload,
					})
				}

				if err != nil {

					log.Printf(
						"runtime publish failed device=%s err=%v",
						device.ID,
						err,
					)
				}

				deviceID := device.ID
				go registry.PublishCameraOffline(
					deviceID,
					now,
				)
			})
		}
	}
}
