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

	client := SnapshotClient{URL: cfg.APIURL, HealthURL: cfg.HealthURL, Token: cfg.Token}

	switch {
	case cfg.ListScenarios:
		printScenarios()
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
	msg, err := sendEvent(sender, optionsFromConfig(cfg, cfg.SendType))
	if err != nil {
		return err
	}
	runID := ""
	if metadata := payloadMetadata(msg.Payload); metadata != nil {
		runID = valueString(metadata["test_run_id"])
	}
	fmt.Printf("SIMULATION sent %s from %s to core test_run_id=%s dry_run_actions=%v\n", msg.Type, msg.Source, runID, cfg.DryRunActions)
	if snapshot, err := client.Fetch(); err == nil {
		fmt.Println(snapshotSummary(snapshot))
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
	return runScenario(sender, client, cfg, cfg.Scenario, func(snapshot *contract.PublicSnapshot, status string) {
		fmt.Println(status)
		fmt.Println(snapshotSummary(snapshot))
	})
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
		msg, err := sendEvent(sender, optionsFromConfig(cfg, eventType))
		if err != nil {
			status = err.Error()
			continue
		}
		status = fmt.Sprintf("SIMULATION sent %s from %s", msg.Type, msg.Source)
	}
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
