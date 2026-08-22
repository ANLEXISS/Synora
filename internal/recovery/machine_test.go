package recovery

import (
	"errors"
	"testing"
	"time"
)

func TestRecoveryDoesNotBecomeReadyBeforeRequiredDependencies(t *testing.T) {
	now := time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC)
	machine, err := New([]Dependency{{Name: "state", Required: true}, {Name: "vision", Required: false}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if got := machine.Snapshot(); got.State != Starting || got.Ready || got.Healthy || got.RecoveryComplete {
		t.Fatalf("initial snapshot=%#v", got)
	}
	if err := machine.BeginRecovery(); err != nil {
		t.Fatal(err)
	}
	if err := machine.SetDependency("vision", false, "model unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := machine.CompleteRecovery(); !errors.Is(err, ErrRequiredDependencies) {
		t.Fatalf("complete without state dependency err=%v", err)
	}
	if got := machine.Snapshot(); got.State != Recovering || got.Ready || got.Healthy || got.RecoveryComplete {
		t.Fatalf("recovery claimed readiness too early=%#v", got)
	}
	if err := machine.SetDependency("state", true, "loaded"); err != nil {
		t.Fatal(err)
	}
	if err := machine.CompleteRecovery(); err != nil {
		t.Fatal(err)
	}
	got := machine.Snapshot()
	if got.State != Degraded || !got.Ready || got.Healthy || !got.RecoveryComplete {
		t.Fatalf("optional failure must be ready but not healthy=%#v", got)
	}
}

func TestRecoveryTransitionsAreAtomicAndObservable(t *testing.T) {
	machine, err := New([]Dependency{{Name: "state", Required: true}}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := machine.BeginRecovery(); err != nil {
		t.Fatal(err)
	}
	if err := machine.SetDependency("state", true, "loaded"); err != nil {
		t.Fatal(err)
	}
	if err := machine.CompleteRecovery(); err != nil {
		t.Fatal(err)
	}
	if got := machine.Snapshot(); got.State != Running || !got.Ready || !got.Healthy {
		t.Fatalf("running snapshot=%#v", got)
	}
	if err := machine.SetDependency("state", false, "storage read-only"); err != nil {
		t.Fatal(err)
	}
	if got := machine.Snapshot(); got.State != Degraded || got.Ready || got.Healthy || got.Reason != "required dependency degraded" {
		t.Fatalf("required runtime failure not revoked=%#v", got)
	}
	if err := machine.Fail("integrity unknown"); err != nil {
		t.Fatal(err)
	}
	if got := machine.Snapshot(); got.State != Failed || got.Ready || got.Healthy || !got.RecoveryComplete {
		t.Fatalf("failed snapshot=%#v", got)
	}
	if err := machine.BeginRecovery(); err != nil {
		t.Fatal(err)
	}
	if err := machine.SetDependency("state", true, "recovered"); err != nil {
		t.Fatal(err)
	}
	if err := machine.CompleteRecovery(); err != nil {
		t.Fatal(err)
	}
	if got := machine.Snapshot(); got.State != Running || !got.Ready || !got.Healthy {
		t.Fatalf("recovered snapshot=%#v", got)
	}
}

func TestRecoveryRejectsUnknownAndInvalidTransitions(t *testing.T) {
	machine, err := New([]Dependency{{Name: "state", Required: true}}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := machine.SetDependency("missing", true, ""); err == nil {
		t.Fatal("unknown dependency accepted")
	}
	if err := machine.CompleteRecovery(); !errors.Is(err, ErrRequiredDependencies) {
		t.Fatalf("starting completion err=%v", err)
	}
	if err := machine.Fail("fatal"); err != nil {
		t.Fatal(err)
	}
	if err := machine.CompleteRecovery(); err == nil {
		t.Fatal("failed machine completed without recovery")
	}
}
