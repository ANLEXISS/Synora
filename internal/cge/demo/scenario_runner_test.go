package demo

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestScenarioLibraryAndRealRunner(t *testing.T) {
	files := map[string][]byte{}
	for _, scenario := range []Scenario{testScenario("runner-test", StepInjectEvent)} {
		data, err := json.Marshal(scenario)
		if err != nil {
			t.Fatal(err)
		}
		files[scenario.ID+".json"] = data
	}
	library, err := LoadScenarioLibrary(files)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewLiveSession(context.Background(), LiveOptions{Seed: 931})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	session.SetScenarioLibrary(library)
	if _, err := session.StartScenario(context.Background(), "runner-test"); err != nil {
		t.Fatal(err)
	}
	result, err := session.ScenarioNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Injection == nil || result.Injection.Association.Decision == "" {
		t.Fatalf("scenario did not use live engine: %#v", result)
	}
	if state := session.ScenarioState(); state == nil || state.Status != ScenarioCompleted {
		t.Fatalf("unexpected scenario state: %#v", state)
	}
}

func TestScenarioExpectedPropertiesDoNotAffectEngine(t *testing.T) {
	first := testScenario("expected-a", StepInjectEvent)
	second := testScenario("expected-a", StepInjectEvent)
	first.Steps[0].Expected = []ExpectedProperty{{Code: "a", Severity: Expected, Description: LocalizedText{FR: "A", EN: "A"}, Scope: ExpectedScopeRoutineCount, Operator: OperatorGreaterThan, Value: 99}}
	second.Steps[0].Expected = []ExpectedProperty{{Code: "b", Severity: Expected, Description: LocalizedText{FR: "B", EN: "B"}, Scope: ExpectedScopeRoutineCount, Operator: OperatorEquals, Value: 0}}
	run := func(scenario Scenario) (LiveInjectionResult, string) {
		session, err := NewLiveSession(context.Background(), LiveOptions{Seed: 932})
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		library, err := LoadScenarioLibrary(map[string][]byte{"scenario.json": mustJSON(scenario)})
		if err != nil {
			t.Fatal(err)
		}
		session.SetScenarioLibrary(library)
		if _, err := session.StartScenario(context.Background(), scenario.ID); err != nil {
			t.Fatal(err)
		}
		result, err := session.ScenarioNext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		digest, err := session.DurableDigest()
		if err != nil {
			t.Fatal(err)
		}
		return *result.Injection, digest
	}
	a, da := run(first)
	b, db := run(second)
	if da != db || a.Association.Decision != b.Association.Decision || a.Deviation.Status != b.Deviation.Status {
		t.Fatalf("expected properties influenced engine: %s/%s %#v %#v", da, db, a, b)
	}
}

func testScenario(id string, kind ScenarioStepKind) Scenario {
	event := &ScenarioEvent{EventID: id + "-event", EventType: "vision.identity", Identity: ScenarioIdentity{Kind: IdentityKnown, EntityID: "subject-a"}, NodeID: "entrance", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", TimestampMode: TimestampAbsolute, AbsoluteTime: ptrTime("2026-01-05T18:15:00Z")}
	step := ScenarioStep{ID: "event", Title: LocalizedText{FR: "Événement", EN: "Event"}, Explanation: LocalizedText{FR: "Explication", EN: "Explanation"}, Kind: kind, Event: event}
	return Scenario{SchemaVersion: ScenarioSchemaVersion, ID: id, Title: LocalizedText{FR: id, EN: id}, Description: LocalizedText{FR: "description", EN: "description"}, Category: ScenarioCategoryLearning, Difficulty: ScenarioDifficultyIntroductory, InitialState: InitialState{Mode: InitialStateEmpty, StartAt: mustTime("2026-01-05T17:00:00Z"), Timezone: "Europe/Paris", ResidentCount: 1, TopologyID: "demo-topology-v1"}, Steps: []ScenarioStep{step}}
}
func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
func mustTime(value string) time.Time {
	result, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return result
}
func ptrTime(value string) *time.Time { result := mustTime(value); return &result }
