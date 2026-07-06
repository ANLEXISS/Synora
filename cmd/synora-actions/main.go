package main

import (
	"context"
	"log"
	"os"

	"synora/internal/actions"
	"synora/internal/actions/devicecmd"
	actionmqtt "synora/internal/actions/mqtt"
	actionrecorder "synora/internal/actions/recorder"
	"synora/internal/bus"
	"synora/pkg/contract"
)

func main() {
	log.Println("starting synora actions")

	busClient, err := bus.NewClient(getenv("SYNORA_BUS", "/run/synora/bus.sock"), "actions")
	if err != nil {
		log.Fatal(err)
	}

	mqttAdapter := actionmqtt.Adapter{}
	if broker := os.Getenv("SYNORA_ACTIONS_MQTT_BROKER"); broker != "" {
		publisher, err := actionmqtt.NewPahoPublisher(
			broker,
			getenv("SYNORA_ACTIONS_MQTT_CLIENT_ID", "synora-actions"),
		)
		if err != nil {
			log.Fatal(err)
		}
		mqttAdapter.Publisher = publisher
		mqttAdapter.Topic = os.Getenv("SYNORA_ACTIONS_MQTT_TOPIC")
		log.Printf("actions: mqtt adapter enabled broker=%s", broker)
	}

	service := &actions.Service{
		Bus: busClient,
		Executor: actions.Router{
			MQTT:      mqttAdapter,
			DeviceCmd: devicecmd.Adapter{},
			Recorder:  actionrecorder.Adapter{},
			Fallback:  actions.DryRunExecutor{Adapter: "dry_run"},
		},
		Deduper: actions.NewDeduper(),
	}

	for msg := range busClient.SubscribeChannel("actions") {
		if msg.Kind != contract.KindCommand || msg.Type != contract.EventActionRequest {
			continue
		}
		service.HandleMessage(context.Background(), msg)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
