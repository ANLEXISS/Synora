package shadowworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type QualificationProfile string

const (
	QualificationProfileSmoke                 QualificationProfile = "smoke"
	QualificationProfileDurability            QualificationProfile = "durability"
	QualificationProfileEndurance             QualificationProfile = "endurance"
	QualificationProfileStress                QualificationProfile = "stress"
	QualificationProfileFullPipelineSynthetic QualificationProfile = "full_pipeline_synthetic"
	QualificationProfileCustom                QualificationProfile = "custom"
)

const (
	QualificationEnabledEnv        = "SYNORA_CGE_SHADOW_QUALIFICATION_ENABLED"
	QualificationRunIDEnv          = "SYNORA_CGE_SHADOW_QUALIFICATION_RUN_ID"
	QualificationProfileEnv        = "SYNORA_CGE_SHADOW_QUALIFICATION_PROFILE"
	QualificationOutputDirEnv      = "SYNORA_CGE_SHADOW_QUALIFICATION_OUTPUT_DIR"
	QualificationSampleIntervalEnv = "SYNORA_CGE_SHADOW_QUALIFICATION_SAMPLE_INTERVAL"
	QualificationMaxOutputBytesEnv = "SYNORA_CGE_SHADOW_QUALIFICATION_MAX_OUTPUT_BYTES"
)

type QualificationThresholds struct {
	MaxQueueDropRatioPermille int
	MaxTimeoutRatioPermille   int
	MaxTrySubmitP99           time.Duration
	MaxAdvisoryCycleP99       time.Duration
	MaxRSSGrowthBytesPerHour  int64
	MaxGoroutineGrowthPerHour int
	WarmupDuration            time.Duration
}

type QualificationConfig struct {
	Enabled bool

	RunID   string
	Profile QualificationProfile

	OutputDirectory string
	SampleInterval  time.Duration
	FlushInterval   time.Duration

	MaxSamples     int
	MaxOutputBytes int64
	MaxWALBytes    int64

	IncludeProcessMetrics bool
	IncludeStageMetrics   bool
	IncludeStorageMetrics bool

	Thresholds QualificationThresholds
}

func DefaultQualificationConfig() QualificationConfig {
	return QualificationConfig{
		Profile: QualificationProfileSmoke, SampleInterval: 5 * time.Second, FlushInterval: 30 * time.Second,
		MaxSamples: 100000, MaxOutputBytes: 512 * 1024 * 1024, MaxWALBytes: 256 * 1024 * 1024,
		IncludeProcessMetrics: true, IncludeStageMetrics: true, IncludeStorageMetrics: true,
		Thresholds: QualificationThresholds{
			MaxQueueDropRatioPermille: 1, MaxTimeoutRatioPermille: 1,
			MaxTrySubmitP99: 1 * time.Millisecond, MaxAdvisoryCycleP99: 250 * time.Millisecond,
			MaxRSSGrowthBytesPerHour: 5 * 1024 * 1024, MaxGoroutineGrowthPerHour: 1,
			WarmupDuration: 15 * time.Minute,
		},
	}
}

func (c QualificationConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.RunID) == "" || len([]rune(c.RunID)) > 128 || strings.ContainsAny(c.RunID, "/\\\r\n\t ") {
		return fmt.Errorf("%w: qualification run id", ErrInvalidConfig)
	}
	if c.Profile != QualificationProfileSmoke && c.Profile != QualificationProfileDurability && c.Profile != QualificationProfileEndurance && c.Profile != QualificationProfileStress && c.Profile != QualificationProfileFullPipelineSynthetic && c.Profile != QualificationProfileCustom {
		return fmt.Errorf("%w: qualification profile", ErrInvalidConfig)
	}
	if c.OutputDirectory == "" || !filepath.IsAbs(c.OutputDirectory) || filepath.Clean(c.OutputDirectory) != c.OutputDirectory || strings.ContainsRune(c.OutputDirectory, 0) {
		return fmt.Errorf("%w: qualification output directory", ErrInvalidConfig)
	}
	if c.SampleInterval <= 0 || c.FlushInterval <= 0 || c.MaxSamples <= 0 || c.MaxOutputBytes <= 0 || c.MaxWALBytes <= 0 || c.FlushInterval < c.SampleInterval {
		return fmt.Errorf("%w: qualification limits", ErrInvalidConfig)
	}
	thresholds := c.Thresholds
	if thresholds.MaxQueueDropRatioPermille < 0 || thresholds.MaxQueueDropRatioPermille > 1000 || thresholds.MaxTimeoutRatioPermille < 0 || thresholds.MaxTimeoutRatioPermille > 1000 || thresholds.MaxTrySubmitP99 <= 0 || thresholds.MaxAdvisoryCycleP99 <= 0 || thresholds.MaxRSSGrowthBytesPerHour < 0 || thresholds.MaxGoroutineGrowthPerHour < 0 || thresholds.WarmupDuration < 0 {
		return fmt.Errorf("%w: qualification thresholds", ErrInvalidConfig)
	}
	return nil
}

func (c QualificationConfig) Fingerprint() string {
	copy := c
	copy.OutputDirectory = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return "shadow-qualification-config-v1:" + hex.EncodeToString(digest[:])
}

func LoadQualificationConfig(getenv func(string) string) (QualificationConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	c := DefaultQualificationConfig()
	var err error
	if c.Enabled, err = parseQualificationBool(getenv(QualificationEnabledEnv), false); err != nil {
		return QualificationConfig{}, fmt.Errorf("%w: qualification enabled", ErrInvalidConfig)
	}
	if value := strings.TrimSpace(getenv(QualificationRunIDEnv)); value != "" {
		c.RunID = value
	}
	if value := strings.TrimSpace(getenv(QualificationProfileEnv)); value != "" {
		c.Profile = QualificationProfile(value)
	}
	if value := strings.TrimSpace(getenv(QualificationOutputDirEnv)); value != "" {
		c.OutputDirectory = value
	}
	if value := strings.TrimSpace(getenv(QualificationSampleIntervalEnv)); value != "" {
		c.SampleInterval, err = time.ParseDuration(value)
		if err != nil {
			return QualificationConfig{}, fmt.Errorf("%w: qualification sample interval", ErrInvalidConfig)
		}
	}
	if value := strings.TrimSpace(getenv(QualificationMaxOutputBytesEnv)); value != "" {
		parsed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil {
			return QualificationConfig{}, fmt.Errorf("%w: qualification max output bytes", ErrInvalidConfig)
		}
		c.MaxOutputBytes = parsed
	}
	if err := c.Validate(); err != nil {
		return QualificationConfig{}, err
	}
	return c, nil
}

func parseQualificationBool(value string, fallback bool) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.ParseBool(strings.TrimSpace(value))
}
