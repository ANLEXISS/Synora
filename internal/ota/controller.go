// Package ota contains the small, fail-closed boundary between Synora and
// the platform OTA backend. It never implements bundle verification itself;
// RAUC remains the authority for signed bundles and boot slots.
package ota

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

type Runner func(context.Context, string, ...string) ([]byte, error)

type Controller struct {
	run         Runner
	raucBinary  string
	healthcheck string
}

type Status struct {
	Backend string `json:"backend"`
	Managed bool   `json:"managed"`
	Raw     string `json:"raw,omitempty"`
}

func NewController(run Runner, raucBinary, healthcheck string) *Controller {
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return nil, fmt.Errorf("command runner unavailable: %s %s", name, strings.Join(args, " "))
		}
	}
	return &Controller{run: run, raucBinary: strings.TrimSpace(raucBinary), healthcheck: strings.TrimSpace(healthcheck)}
}

func (c *Controller) Status(ctx context.Context) (Status, error) {
	if c == nil || c.raucBinary == "" {
		return Status{Backend: "unmanaged"}, nil
	}
	output, err := c.run(ctx, c.raucBinary, "status", "--detailed")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{Backend: "unmanaged"}, nil
		}
		return Status{Backend: "unavailable"}, err
	}
	return Status{Backend: "rauc", Managed: true, Raw: strings.TrimSpace(string(output))}, nil
}

func (c *Controller) Install(ctx context.Context, bundle string) error {
	if c == nil || c.raucBinary == "" {
		return errors.New("RAUC backend is unavailable")
	}
	bundle = strings.TrimSpace(bundle)
	if bundle == "" {
		return errors.New("OTA bundle path is required")
	}
	info, err := os.Stat(bundle)
	if err != nil {
		return fmt.Errorf("OTA bundle unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("OTA bundle must be a non-empty regular file")
	}
	_, err = c.run(ctx, c.raucBinary, "install", bundle)
	if err != nil {
		return fmt.Errorf("RAUC install failed: %w", err)
	}
	return nil
}

func (c *Controller) MarkGood(ctx context.Context) error {
	if c == nil || c.raucBinary == "" {
		return errors.New("RAUC backend is unavailable")
	}
	if c.healthcheck == "" {
		return errors.New("boot healthcheck is not configured")
	}
	if _, err := c.run(ctx, c.healthcheck, "run", "--readonly"); err != nil {
		return fmt.Errorf("boot healthcheck failed; mark-good refused: %w", err)
	}
	if _, err := c.run(ctx, c.raucBinary, "status", "mark-good"); err != nil {
		return fmt.Errorf("RAUC mark-good failed: %w", err)
	}
	return nil
}

func (c *Controller) MarkBad(ctx context.Context) error {
	if c == nil || c.raucBinary == "" {
		return errors.New("RAUC backend is unavailable")
	}
	if _, err := c.run(ctx, c.raucBinary, "status", "mark-bad"); err != nil {
		return fmt.Errorf("RAUC mark-bad failed: %w", err)
	}
	return nil
}
