package failurematrix

import (
	"errors"
	"testing"
)

func TestFailureMatrixCoversAllComponentsAndCutPoints(t *testing.T) {
	components := []Component{Core, Bus, Discovery, Vision, StateStore}
	cuts := []CutPoint{BeforePersist, AfterPersistBeforeSend, AfterSendBeforeAck, AfterAck}
	for _, component := range components {
		for _, cut := range cuts {
			maxLoss := 0
			if cut == BeforePersist {
				maxLoss = 1
			}
			name := string(component) + ":" + string(cut)
			result, err := Run(Scenario{
				Name: name, Component: component,
				CutPoint: cut, Load: 11, MaxLoss: maxLoss,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Lost > maxLoss || !result.StableReplays || !result.ShutdownBounded {
				t.Fatalf("scenario=%#v result=%#v", name, result)
			}
			if result.Durable < result.Acknowledged-result.Replayed {
				t.Fatalf("durability accounting invalid: %#v", result)
			}
		}
	}
}

func TestFailureMatrixCampaignIsDeterministicAndStable(t *testing.T) {
	scenarios := []Scenario{
		{Name: "core-load", Component: Core, CutPoint: AfterSendBeforeAck, Load: 128, MaxLoss: 0},
		{Name: "bus-load", Component: Bus, CutPoint: AfterPersistBeforeSend, Load: 128, MaxLoss: 0},
		{Name: "discovery-load", Component: Discovery, CutPoint: BeforePersist, Load: 128, MaxLoss: 1},
		{Name: "vision-load", Component: Vision, CutPoint: AfterSendBeforeAck, Load: 128, MaxLoss: 0},
		{Name: "state-load", Component: StateStore, CutPoint: AfterAck, Load: 128, MaxLoss: 0},
	}
	report := RunCampaign(scenarios, 100)
	if report.Runs != 500 || report.Passed != report.Runs || report.Failed != 0 {
		t.Fatalf("campaign report=%#v", report)
	}
	if report.MaxLoss > 1 || report.MaxRecovery <= 0 {
		t.Fatalf("campaign bounds=%#v", report)
	}
}

func TestFailureMatrixRejectsInvalidScenario(t *testing.T) {
	_, err := Run(Scenario{Name: "bad", Component: "unknown", CutPoint: BeforePersist, Load: 1})
	if !errors.Is(err, ErrInvalidScenario) {
		t.Fatalf("error=%v", err)
	}
}
