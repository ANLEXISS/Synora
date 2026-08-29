package resourcebudget

import "testing"

func TestV1BudgetsArePositiveAndConsistent(t *testing.T) {
	limits := Defaults()
	if err := limits.Validate(); err != nil {
		t.Fatal(err)
	}
	if limits.MaxMessageBytes != 1<<20 || limits.MaxVisionWorkers != 3 || limits.MaxConcurrentClipUploads != 3 {
		t.Fatalf("unexpected V1 budgets: %#v", limits)
	}
}

func TestBudgetsRejectInvalidConfiguration(t *testing.T) {
	limits := Defaults()
	limits.MaxWebSocketClients = 0
	if err := limits.Validate(); err == nil {
		t.Fatal("zero websocket budget was accepted")
	}
	limits = Defaults()
	limits.MaxClipUploadsPerCamera = limits.MaxConcurrentClipUploads + 1
	if err := limits.Validate(); err == nil {
		t.Fatal("per-camera budget larger than global budget was accepted")
	}
}
