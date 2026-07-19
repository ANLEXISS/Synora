package fieldtrial

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	SchemaVersion             = "cge-field-trial-v1"
	DefaultRootDir            = "/var/lib/synora/cge-field-trials"
	DefaultSegmentMaxBytes    = int64(16 << 20)
	DefaultRetentionDays      = 45
	DefaultMaximumTotalBytes  = int64(512 << 20)
	MaxSessionIDLength        = 128
	MaxErrorCodesPerEvent     = 32
	MaxErrorCodeLength        = 128
	MaxAnnotationNoteCodeSize = 128
)

type Config struct {
	Enabled bool

	RootDir   string
	SessionID string

	SegmentMaxBytes   int64
	RetentionDays     int
	MaximumTotalBytes int64

	SyncEachEvent bool

	PseudonymizationKeyFile string

	IncludeContextCategories bool
	IncludeLatencyBreakdown  bool

	// RepairTerminalPartial must be explicitly enabled by a recovery command.
	// It is never inferred from a normal startup.
	RepairTerminalPartial bool

	// TopologyFile is read once at startup by the Shadow adapter.
	TopologyFile string
}

func DefaultConfig() Config {
	return Config{RootDir: DefaultRootDir, SegmentMaxBytes: DefaultSegmentMaxBytes, RetentionDays: DefaultRetentionDays, MaximumTotalBytes: DefaultMaximumTotalBytes, IncludeContextCategories: true, IncludeLatencyBreakdown: true}
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.RootDir) == "" || !filepath.IsAbs(c.RootDir) || filepath.Clean(c.RootDir) != c.RootDir || c.SegmentMaxBytes <= 0 || c.SegmentMaxBytes > 1<<30 || c.RetentionDays < 0 || c.RetentionDays > 3650 || c.MaximumTotalBytes <= 0 || c.MaximumTotalBytes < c.SegmentMaxBytes {
		return fmt.Errorf("%w: bounds", ErrInvalidConfig)
	}
	if c.SessionID != "" && !validSessionID(c.SessionID) {
		return fmt.Errorf("%w: session id", ErrInvalidConfig)
	}
	if c.PseudonymizationKeyFile != "" && filepath.Clean(c.PseudonymizationKeyFile) == "." {
		return fmt.Errorf("%w: key path", ErrInvalidConfig)
	}
	return nil
}

func LoadConfig(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	c := DefaultConfig()
	var err error
	if c.Enabled, err = parseBool(getenv("SYNORA_CGE_FIELD_TRIAL_ENABLED"), false); err != nil {
		return Config{}, fmt.Errorf("%w: enabled", ErrInvalidConfig)
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_FIELD_TRIAL_ROOT")); value != "" {
		c.RootDir = value
	}
	if value := strings.TrimSpace(getenv("SYNORA_CGE_FIELD_TRIAL_SESSION_ID")); value != "" {
		c.SessionID = value
	}
	if c.SegmentMaxBytes, err = parseInt64(getenv("SYNORA_CGE_FIELD_TRIAL_SEGMENT_MAX_BYTES"), c.SegmentMaxBytes); err != nil {
		return Config{}, fmt.Errorf("%w: segment size", ErrInvalidConfig)
	}
	if c.RetentionDays, err = parseInt(getenv("SYNORA_CGE_FIELD_TRIAL_RETENTION_DAYS"), c.RetentionDays); err != nil {
		return Config{}, fmt.Errorf("%w: retention", ErrInvalidConfig)
	}
	if c.MaximumTotalBytes, err = parseInt64(getenv("SYNORA_CGE_FIELD_TRIAL_MAX_TOTAL_BYTES"), c.MaximumTotalBytes); err != nil {
		return Config{}, fmt.Errorf("%w: quota", ErrInvalidConfig)
	}
	if c.SyncEachEvent, err = parseBool(getenv("SYNORA_CGE_FIELD_TRIAL_SYNC_EACH_EVENT"), false); err != nil {
		return Config{}, fmt.Errorf("%w: sync", ErrInvalidConfig)
	}
	c.PseudonymizationKeyFile = strings.TrimSpace(getenv("SYNORA_CGE_FIELD_TRIAL_KEY_FILE"))
	c.TopologyFile = strings.TrimSpace(getenv("SYNORA_CGE_FIELD_TRIAL_TOPOLOGY_FILE"))
	if c.IncludeContextCategories, err = parseBool(getenv("SYNORA_CGE_FIELD_TRIAL_INCLUDE_CONTEXT"), true); err != nil {
		return Config{}, fmt.Errorf("%w: context", ErrInvalidConfig)
	}
	if c.IncludeLatencyBreakdown, err = parseBool(getenv("SYNORA_CGE_FIELD_TRIAL_INCLUDE_LATENCIES"), true); err != nil {
		return Config{}, fmt.Errorf("%w: latencies", ErrInvalidConfig)
	}
	return c, c.Validate()
}

func parseBool(value string, fallback bool) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.ParseBool(strings.TrimSpace(value))
}

func parseInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.Atoi(strings.TrimSpace(value))
}

func parseInt64(value string, fallback int64) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

func validSessionID(value string) bool {
	if value == "" || len([]rune(value)) > MaxSessionIDLength || value == "." || value == ".." || strings.ContainsAny(value, "/\\\x00\r\n") {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == ':' {
			return false
		}
	}
	return true
}

func sessionID(now time.Time, randomValue string) string {
	return fmt.Sprintf("cge-trial-%s-%s", now.UTC().Format("20060102"), randomValue)
}
