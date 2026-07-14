package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"synora/internal/actions"
	"synora/internal/actions/devicecmd"
	actionmqtt "synora/internal/actions/mqtt"
	actionrecorder "synora/internal/actions/recorder"
	actionwhatsapp "synora/internal/actions/whatsapp"
	"synora/internal/bus"
	"synora/pkg/contract"
)

func main() {
	log.Println("starting synora actions")

	busClient, err := bus.NewClient(getenv("SYNORA_BUS", "/run/synora/bus.sock"), "actions")
	if err != nil {
		log.Fatal(err)
	}
	startupPayload, _ := json.Marshal(map[string]any{
		"component": "actions",
		"status":    "ok",
		"message":   "bus client registered",
	})
	if err := busClient.Send(contract.Message{
		Type:      contract.EventActionServiceStarted,
		Kind:      contract.KindEvent,
		Source:    "actions",
		Timestamp: time.Now().UTC(),
		Payload:   startupPayload,
	}); err != nil {
		log.Printf("actions: startup status publish failed: %v", err)
	}

	mqttAdapter := actionmqtt.Adapter{}
	whatsappAdapter := actionwhatsapp.Adapter{Config: actionwhatsapp.ConfigFromEnv()}
	if whatsappAdapter.Config.Enabled {
		log.Printf("actions: whatsapp provider enabled dry_run=%t", whatsappAdapter.Config.DryRun)
	}
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
			WhatsApp:  whatsappAdapter,
			Fallback:  actions.DryRunExecutor{Adapter: "dry_run"},
		},
		Deduper: actions.NewDeduper(),
	}

	for msg := range busClient.SubscribeChannel("actions") {
		// TODO: remove automation.action once all deployed automations emit action.request.
		if msg.Kind != contract.KindCommand || (msg.Type != contract.EventActionRequest && msg.Type != contract.EventAutomationAction) {
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
