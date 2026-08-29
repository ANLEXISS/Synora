// Package ota contains the small, fail-closed boundary between Synora and
// the platform OTA backend. It never implements bundle verification itself;
// RAUC remains the authority for signed bundles and boot slots.
package ota

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Runner func(context.Context, string, ...string) ([]byte, error)

type Controller struct {
	run          Runner
	raucBinary   string
	healthcheck  string
	verification Verification
	journalPath  string
}

type Verification struct {
	ManifestPath     string
	PublicKey        ed25519.PublicKey
	CurrentVersion   string
	Hardware         string
	CurrentMigration int
}

type UpdateJournal struct {
	SchemaVersion int       `json:"schema_version"`
	Bundle        string    `json:"bundle"`
	Phase         string    `json:"phase"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	return &Controller{run: run, raucBinary: strings.TrimSpace(raucBinary), healthcheck: strings.TrimSpace(healthcheck), journalPath: strings.TrimSpace(os.Getenv("SYNORA_OTA_JOURNAL"))}
}

func (c *Controller) SetVerification(value Verification) {
	if c == nil {
		return
	}
	c.verification = value
	c.verification.ManifestPath = strings.TrimSpace(value.ManifestPath)
}

func (c *Controller) SetJournalPath(path string) {
	if c != nil {
		c.journalPath = filepath.Clean(strings.TrimSpace(path))
	}
}

// Apply verifies, installs and marks the new slot good. Any failure after the
// install command requests RAUC mark-bad, leaving bootloader rollback in
// charge of the platform rather than pretending userspace can switch slots.
func (c *Controller) Apply(ctx context.Context, bundle string) error {
	if c == nil {
		return errors.New("OTA controller unavailable")
	}
	if c.verification.ManifestPath != "" {
		data, err := os.ReadFile(c.verification.ManifestPath)
		if err != nil {
			return fmt.Errorf("OTA manifest unavailable: %w", err)
		}
		var manifest BundleManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("OTA manifest decode failed: %w", err)
		}
		if err := verifyBundleManifest(bundle, manifest, c.verification.PublicKey, c.verification.CurrentVersion, c.verification.Hardware, c.verification.CurrentMigration); err != nil {
			return err
		}
	}
	if err := c.writeJournal("installing", bundle); err != nil {
		return err
	}
	if err := c.Install(ctx, bundle); err != nil {
		_ = c.MarkBad(ctx)
		_ = c.writeJournal("rolled_back", bundle)
		return err
	}
	if err := c.writeJournal("installed", bundle); err != nil {
		_ = c.MarkBad(ctx)
		return err
	}
	if err := c.MarkGood(ctx); err != nil {
		_ = c.MarkBad(ctx)
		_ = c.writeJournal("rolled_back", bundle)
		return err
	}
	return c.clearJournal()
}

// Recover rolls back an update whose process stopped before the journal was
// cleared. It is safe to call at every startup.
func (c *Controller) Recover(ctx context.Context) error {
	journal, err := c.readJournal()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if journal.Phase == "installed" || journal.Phase == "installing" {
		if err := c.MarkBad(ctx); err != nil {
			return err
		}
		journal.Phase = "rolled_back"
		journal.UpdatedAt = time.Now().UTC()
		return c.writeJournalValue(journal)
	}
	return nil
}

func (c *Controller) writeJournal(phase, bundle string) error {
	if strings.TrimSpace(c.journalPath) == "" {
		return nil
	}
	now := time.Now().UTC()
	return c.writeJournalValue(UpdateJournal{SchemaVersion: 1, Bundle: bundle, Phase: phase, StartedAt: now, UpdatedAt: now})
}

func (c *Controller) writeJournalValue(value UpdateJournal) error {
	if c.journalPath == "" {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.journalPath), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.journalPath), ".ota-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, c.journalPath)
}

func (c *Controller) readJournal() (UpdateJournal, error) {
	if c.journalPath == "" {
		return UpdateJournal{}, os.ErrNotExist
	}
	data, err := os.ReadFile(c.journalPath)
	if err != nil {
		return UpdateJournal{}, err
	}
	var journal UpdateJournal
	if err := json.Unmarshal(data, &journal); err != nil || journal.SchemaVersion != 1 {
		return UpdateJournal{}, errors.New("invalid OTA journal")
	}
	return journal, nil
}

func (c *Controller) clearJournal() error {
	if c.journalPath == "" {
		return nil
	}
	if err := os.Remove(c.journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
