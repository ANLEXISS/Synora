package ota

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMarkGoodRunsReadonlyHealthcheckBeforeRAUC(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	}
	c := NewController(run, "rauc", "synora-boot-healthcheck")
	if err := c.MarkGood(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"synora-boot-healthcheck", "run", "--readonly"},
		{"rauc", "status", "mark-good"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%#v want=%#v", calls, want)
	}
}

func TestMarkGoodRefusesWhenHealthcheckFails(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, errors.New("health failed")
	}
	if err := NewController(run, "rauc", "healthcheck").MarkGood(context.Background()); err == nil {
		t.Fatal("mark-good must fail when healthcheck fails")
	}
	if len(calls) != 1 || calls[0][0] != "healthcheck" {
		t.Fatalf("RAUC was called after failed healthcheck: %#v", calls)
	}
}

func TestInstallRequiresNonEmptyRegularBundle(t *testing.T) {
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		t.Fatalf("unexpected command %s %#v", name, args)
		return nil, nil
	}
	c := NewController(run, "rauc", "healthcheck")
	if err := c.Install(context.Background(), filepath.Join(t.TempDir(), "missing.raucb")); err == nil {
		t.Fatal("missing bundle accepted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "empty.raucb"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Install(context.Background(), filepath.Join(dir, "empty.raucb")); err == nil {
		t.Fatal("empty bundle accepted")
	}
}

func TestStatusWithoutBackendIsUnmanaged(t *testing.T) {
	status, err := NewController(nil, "", "").Status(context.Background())
	if err != nil || status.Backend != "unmanaged" || status.Managed {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}
