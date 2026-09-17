package homemodel

import "testing"

func TestReferenceScenariosAreDeterministicAndMaterialFree(t *testing.T) {
	items := ReferenceScenarios()
	if len(items) != 10 {
		t.Fatalf("reference scenario count=%d", len(items))
	}
	for _, item := range items {
		first := RunScenario(item)
		second := RunScenario(item)
		if first.WorkingSet != second.WorkingSet || len(first.Entities) != len(second.Entities) || len(first.Situations) != len(second.Situations) {
			t.Fatalf("scenario %s is not deterministic", item.ID)
		}
		if err := ValidateScenario(item); err != nil {
			t.Fatalf("scenario %s: %v", item.ID, err)
		}
	}
}
