package cognitive

import (
	"errors"
	"strings"
)

const CognitiveDryRunEnv = "SYNORA_COGNITIVE_DRY_RUN"

var ErrPhysicalExecutionDisabled = errors.New("cognitive physical execution is disabled; proposal remains dry-run")

// DryRunPolicy fails closed. Even when the environment variable is omitted,
// cognitive output cannot reach an actuator. Setting SYNORA_COGNITIVE_DRY_RUN=1
// makes the intended mode explicit in deployments and demonstrations.
type DryRunPolicy struct {
	Enabled bool
}

func NewDryRunPolicy(getenv func(string) string) DryRunPolicy {
	if getenv == nil {
		return DryRunPolicy{Enabled: true}
	}
	value := strings.ToLower(strings.TrimSpace(getenv(CognitiveDryRunEnv)))
	return DryRunPolicy{Enabled: value == "" || value == "1" || value == "true" || value == "yes" || value == "on"}
}

func (p DryRunPolicy) PublishOnly() bool { return p.Enabled }

func (p DryRunPolicy) Execute(_ string) error {
	return ErrPhysicalExecutionDisabled
}
