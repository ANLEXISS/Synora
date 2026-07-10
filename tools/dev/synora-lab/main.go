package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"synora/pkg/contract"
)

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Synora Lab is a development simulator. Do not run in production.")
	if cfg.Verbose {
		printVerboseConfig(cfg)
	}

	client := SnapshotClient{URL: cfg.APIURL, HealthURL: cfg.HealthURL, Token: cfg.Token}

	switch {
	case cfg.ListScenarios:
		printScenarios()
	case cfg.ShowCGE && cfg.SendType == "" && cfg.Scenario == "":
		if err := showCGE(client, cfg); err != nil {
			log.Fatal(err)
		}
	case cfg.ShowDanger && cfg.SendType == "" && cfg.Scenario == "":
		if err := showDanger(client, cfg); err != nil {
			log.Fatal(err)
		}
	case cfg.SendType != "":
		if err := runSingleSend(cfg, client); err != nil {
			log.Fatal(err)
		}
	case cfg.Scenario != "":
		if err := runScenarioCommand(cfg, client); err != nil {
			log.Fatal(err)
		}
	case cfg.Watch:
		watch(client)
	case cfg.NoTUI:
		snapshot, err := client.Fetch()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(snapshotSummary(snapshot))
	default:
		if err := runInteractive(cfg, client); err != nil {
			log.Fatal(err)
		}
	}
}

func runSingleSend(cfg Config, client SnapshotClient) error {
	snapshot, err := client.Fetch()
	if err == nil && !deviceExists(snapshot, cfg.DeviceID) {
		fmt.Printf("warning: device %s is not visible in PublicSnapshot devices/cameras\n", cfg.DeviceID)
	}
	sender, err := newBusSender(cfg.BusPath)
	if err != nil {
		return err
	}
	msg, observation, err := sendEventObserved(sender, client, cfg, optionsFromConfig(cfg, cfg.SendType))
	if err != nil {
		return err
	}
	runID := ""
	if metadata := payloadMetadata(msg.Payload); metadata != nil {
		runID = valueString(metadata["test_run_id"])
	}
	fmt.Printf("SIMULATION sent to bus %s from %s to core test_run_id=%s dry_run_actions=%v\n", msg.Type, msg.Source, runID, cfg.DryRunActions)
	printObservation(observation)
	if snapshot, err := client.Fetch(); err == nil {
		fmt.Println(snapshotSummary(snapshot))
		if cfg.ShowDanger || hasDangerExpectations(cfg) {
			fmt.Print(renderDanger(snapshot))
			if err := expectDanger(snapshot, cfg); err != nil {
				return err
			}
		}
	} else {
		fmt.Println(err)
	}
	return nil
}

func printScenarios() {
	fmt.Print(listScenariosText())
}

func listScenariosText() string {
	items := scenarios()
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out strings.Builder
	for _, id := range ids {
		scenario := items[id]
		out.WriteString(fmt.Sprintf("%s\t%s\n", scenario.ID, scenario.Name))
		out.WriteString(fmt.Sprintf("  %s\n", scenario.Description))
		for _, step := range scenario.Steps {
			out.WriteString(fmt.Sprintf("  - %s: %s (%s)\n", step.ID, step.Label, step.EventType))
		}
	}
	return out.String()
}

func runScenarioCommand(cfg Config, client SnapshotClient) error {
	sender, err := newBusSender(cfg.BusPath)
	if err != nil {
		return err
	}
	for i := 0; i < cfg.Repeat; i++ {
		if err := runScenario(sender, client, cfg, cfg.Scenario, func(snapshot *contract.PublicSnapshot, status string) {
			fmt.Println(status)
			fmt.Println(snapshotSummary(snapshot))
		}); err != nil {
			return err
		}
	}
	if cfg.Repeat > 1 {
		fmt.Printf("Scenario %s repeated %d times.\n", cfg.Scenario, cfg.Repeat)
	}
	if cfg.InspectLearning || cfg.ExpectSequence != "" || cfg.ShowCGE {
		if err := showCGE(client, cfg); err != nil {
			return err
		}
	}
	if cfg.ShowDanger || hasDangerExpectations(cfg) {
		return showDanger(client, cfg)
	}
	return nil
}

func showCGE(client SnapshotClient, cfg Config) error {
	snapshot, err := client.Fetch()
	if err != nil {
		return err
	}
	fmt.Print(renderCGE(snapshot))
	if cfg.ExpectSequence != "" {
		if err := expectScenarioSequence(snapshot, cfg.ExpectSequence); err != nil {
			return err
		}
	}
	return nil
}

func showDanger(client SnapshotClient, cfg Config) error {
	snapshot, err := client.Fetch()
	if err != nil {
		return err
	}
	fmt.Print(renderDanger(snapshot))
	return expectDanger(snapshot, cfg)
}

func hasDangerExpectations(cfg Config) bool {
	return cfg.ExpectDangerLevel >= 0 || cfg.ExpectCategory != "" || cfg.ExpectSystemAction != ""
}

func watch(client SnapshotClient) {
	for {
		snapshot, err := client.Fetch()
		status := ""
		if err != nil {
			status = err.Error()
		}
		health, _ := client.FetchHealth()
		fmt.Print(renderSnapshot(snapshot, health, status))
		time.Sleep(2 * time.Second)
	}
}

func runInteractive(cfg Config, client SnapshotClient) error {
	reader := bufio.NewReader(os.Stdin)
	var sender EventSender
	var status string

	for {
		snapshot, err := client.Fetch()
		health, _ := client.FetchHealth()
		if err != nil {
			status = err.Error()
		} else if !deviceExists(snapshot, cfg.DeviceID) {
			status = fmt.Sprintf("warning: device %s is not visible in snapshot", cfg.DeviceID)
		}
		fmt.Print(renderSnapshot(snapshot, health, status))
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}
		if cmd == "q" {
			return nil
		}
		if cmd == "r" {
			status = "snapshot refreshed"
			continue
		}
		if cmd == "s" {
			fmt.Print("scenario name: ")
			name, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			if sender == nil {
				sender, err = newBusSender(cfg.BusPath)
				if err != nil {
					status = err.Error()
					continue
				}
			}
			err = runScenario(sender, client, cfg, strings.TrimSpace(name), func(_ *contract.PublicSnapshot, stepStatus string) {
				status = stepStatus
				fmt.Println(stepStatus)
			})
			if err != nil {
				status = err.Error()
			}
			continue
		}
		eventType, ok := commandEvent(cmd)
		if !ok {
			status = "unknown command: " + cmd
			continue
		}
		if sender == nil {
			sender, err = newBusSender(cfg.BusPath)
			if err != nil {
				status = err.Error()
				continue
			}
		}
		msg, observation, err := sendEventObserved(sender, client, cfg, optionsFromConfig(cfg, eventType))
		if err != nil {
			status = err.Error()
			continue
		}
		status = fmt.Sprintf("SIMULATION sent to bus %s from %s; %s", msg.Type, msg.Source, observationText(observation))
	}
}

func printObservation(observation Observation) {
	fmt.Println(observationText(observation))
}

func observationText(observation Observation) string {
	if observation.Observed {
		return "observed in snapshot observed_by=" + observation.Reason
	}
	if observation.Err != nil {
		return "WARNING: sent to bus but not observed by Core/PublicSnapshot: " + observation.Err.Error()
	}
	return "WARNING: sent to bus but not observed by Core/PublicSnapshot"
}

func commandEvent(cmd string) (string, bool) {
	switch cmd {
	case "1":
		return contract.EventVisionIdentity, true
	case "2":
		return contract.EventVisionUnknown, true
	case "3":
		return contract.EventVisionUncertain, true
	case "4":
		return contract.EventVisionMotion, true
	case "5":
		return contract.EventVisionWeapon, true
	case "6":
		return contract.EventVisionFall, true
	case "7":
		return contract.EventVisionTamper, true
	case "o":
		return contract.EventDiscoveryCameraOnline, true
	case "x":
		return contract.EventDeviceOffline, true
	default:
		return "", false
	}
}
