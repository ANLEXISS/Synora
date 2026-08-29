package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"synora/internal/actions"
	"synora/internal/actions/devicecmd"
	actionmqtt "synora/internal/actions/mqtt"
	actionrecorder "synora/internal/actions/recorder"
	actionwhatsapp "synora/internal/actions/whatsapp"
	"synora/internal/bus"
	"synora/internal/runtimeconfig"
	"synora/pkg/contract"
)

func main() {
	log.Println("starting synora actions")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	busClient, err := bus.ConnectContext(ctx, runtime.Paths.BusSocket, "actions")
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("actions bus connection stopped: %v", err)
		}
		return
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
			actionMQTTClientID(),
		)
		if err != nil {
			log.Fatal(err)
		}
		mqttAdapter.Publisher = publisher
		mqttAdapter.Topic = os.Getenv("SYNORA_ACTIONS_MQTT_TOPIC")
		log.Printf("actions: mqtt adapter enabled broker=%s", broker)
	}
	resultStore, err := actions.OpenFileResultStore(runtime.Paths.ActionResults)
	if err != nil {
		log.Fatal("invalid action result store: ", err)
	}

	service := &actions.Service{
		Bus:   busClient,
		Store: resultStore,
		Executor: actions.Router{
			MQTT:      mqttAdapter,
			DeviceCmd: devicecmd.Adapter{},
			Recorder:  actionrecorder.Adapter{},
			WhatsApp:  whatsappAdapter,
			Fallback:  actions.DryRunExecutor{Adapter: "dry_run"},
		},
		Deduper: actions.NewDeduper(),
	}

	messages := busClient.SubscribeChannel("actions")
	for {
		select {
		case <-ctx.Done():
			_ = busClient.Close()
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			// TODO: remove automation.action once all deployed automations emit action.request.
			if msg.Kind != contract.KindCommand || (msg.Type != contract.EventActionRequest && msg.Type != contract.EventAutomationAction) {
				continue
			}
			service.HandleMessage(ctx, msg)
		}
	}
}

func actionMQTTClientID() string {
	if value := os.Getenv("SYNORA_ACTIONS_MQTT_CLIENT_ID"); value != "" {
		return value
	}
	return "synora-actions"
}
