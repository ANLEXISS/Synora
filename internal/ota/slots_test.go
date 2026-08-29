package ota

import "testing"

func TestABSlotFallbackAfterThreeBootAttempts(t *testing.T) {
	slots := NewSlotManager(SlotA)
	if err := slots.Activate(SlotB); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		booted, err := slots.BootAttempt()
		if err != nil || booted != SlotB {
			t.Fatalf("attempt %d booted=%q err=%v", attempt, booted, err)
		}
	}
	booted, err := slots.BootAttempt()
	if err != nil || booted != SlotA || slots.Active != SlotA || slots.Pending != "" {
		t.Fatalf("fallback booted=%q active=%q pending=%q err=%v", booted, slots.Active, slots.Pending, err)
	}
}

func TestABSlotMarksGoodOnlyAfterReadiness(t *testing.T) {
	slots := NewSlotManager(SlotA)
	if err := slots.Activate(SlotB); err != nil {
		t.Fatal(err)
	}
	if err := slots.MarkGood(SlotB); err != nil {
		t.Fatal(err)
	}
	if slots.Active != SlotB || slots.Pending != "" {
		t.Fatalf("unexpected slot state: %#v", slots)
	}
}
