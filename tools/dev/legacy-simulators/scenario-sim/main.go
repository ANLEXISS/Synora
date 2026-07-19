package main

import (
	"log"
	"time"

	"synora/internal/bus"
	"synora/pkg/contract"
)

type Step struct {
	Delay   time.Duration
	Type    string
	Source  string
	Payload string
}

type Scenario struct {
	Name  string
	Steps []Step
}

func send(client *bus.Client, step Step) {
	msg := contract.Message{
		Type:    step.Type,
		Source:  step.Source,
		Target:  "core",
		Payload: []byte(step.Payload),
	}

	err := client.Send(msg)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("SENT:", step.Type, step.Source)
}

func runScenario(client *bus.Client, s Scenario) {

	log.Println("=================================")
	log.Println("RUN:", s.Name)
	log.Println("=================================")

	for _, step := range s.Steps {

		time.Sleep(step.Delay)

		send(client, step)
	}

	time.Sleep(2 * time.Second)
}

// =====================================================
// MAIN
// =====================================================

func main() {

	client, err := bus.NewClient("/run/synora/bus.sock", "vision-worker")
	if err != nil {
		log.Fatal(err)
	}

	scenarios := []Scenario{

		// =========================================
		// 1. PRESENCE SIMPLE
		// =========================================
		{
			Name: "presence simple",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"alexis","confidence":0.98}`},
			},
		},

		// =========================================
		// 2. DEPLACEMENT COHERENT
		// =========================================
		{
			Name: "deplacement coherent",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"alexis","confidence":0.95}`},
				{500 * time.Millisecond, "vision.identity", "cam_03", `{"identity":"uncertain","best_match":"alexis","confidence":0.7}`},
			},
		},

		// =========================================
		// 3. DEPLACEMENT IMPOSSIBLE (STRETCH)
		// =========================================
		{
			Name: "deplacement impossible",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"alexis","confidence":0.95}`},
				{200 * time.Millisecond, "vision.identity", "cam_05", `{"identity":"uncertain","confidence":0.6}`},
			},
		},

		// =========================================
		// 4. UNKNOWN → INTRUSION
		// =========================================
		{
			Name: "unknown intrusion",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"unknown","confidence":0.9}`},
			},
		},

		// =========================================
		// 5. UNCERTAIN LINKED (IA + PATH)
		// =========================================
		{
			Name: "uncertain linked",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"alexis","confidence":0.98}`},
				{300 * time.Millisecond, "vision.identity", "cam_03", `{"identity":"uncertain","best_match":"alexis","confidence":0.6}`},
			},
		},

		// =========================================
		// 6. UNCERTAIN ISOLATED
		// =========================================
		{
			Name: "uncertain isolated",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"uncertain","confidence":0.7}`},
			},
		},

		// =========================================
		// 7. MULTI PERSON TRACKING
		// =========================================
		{
			Name: "multi person",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"alexis","confidence":0.95}`},
				{0, "vision.identity", "cam_03", `{"identity":"carole","confidence":0.95}`},
				{500 * time.Millisecond, "vision.identity", "vision-worker", `{"identity":"uncertain","confidence":0.6}`},
			},
		},

		// =========================================
		// 8. PROPAGATION SPATIALE
		// =========================================
		{
			Name: "propagation",
			Steps: []Step{
				{0, "motion.detected", "vision-worker", `{}`},
			},
		},

		// =========================================
		// 9. DECAY (disparition)
		// =========================================
		{
			Name: "decay",
			Steps: []Step{
				{0, "vision.identity", "vision-worker", `{"identity":"alexis","confidence":0.95}`},
				{10 * time.Second, "noop", "none", `{}`},
			},
		},

		// =========================================
		// 10. FAUX POSITIF CAPTEUR
		// =========================================
		{
			Name: "sensor noise",
			Steps: []Step{
				{0, "motion.detected", "vision-worker", `{}`},
				{100 * time.Millisecond, "motion.detected", "vision-worker", `{}`},
				{100 * time.Millisecond, "motion.detected", "vision-worker", `{}`},
			},
		},
	}

	for _, s := range scenarios {
		runScenario(client, s)
		time.Sleep(3 * time.Second)
	}

	log.Println("==== ALL TESTS DONE ====")
}
