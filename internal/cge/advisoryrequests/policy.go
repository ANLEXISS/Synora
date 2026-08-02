package advisoryrequests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type Policy struct {
	MaxActiveRequestsPerEpisode    int
	MaxStoredRequestsPerEpisode    int
	MaxReasonCodes                 int
	MaxFactCodes                   int
	MaxHypothesisPairs             int
	MinUtilityPermille             int
	MinDiscriminationPermille      int
	MinCoverageGainPermille        int
	SuppressRedundancyPermille     int
	MinPreferredMarginPermille     int
	DefaultTTL                     time.Duration
	DeferredRecheckInterval        time.Duration
	IncludeHighSensitivityRequests bool
	PreserveSuppressedRequests     bool
}

func DefaultPolicy() Policy {
	return Policy{MaxActiveRequestsPerEpisode: 4, MaxStoredRequestsPerEpisode: 32, MaxReasonCodes: 32, MaxFactCodes: 32, MaxHypothesisPairs: 64, MinUtilityPermille: 250, MinDiscriminationPermille: 200, MinCoverageGainPermille: 100, SuppressRedundancyPermille: 700, MinPreferredMarginPermille: 75, DefaultTTL: 15 * time.Minute, DeferredRecheckInterval: 5 * time.Minute, PreserveSuppressedRequests: true}
}

func (p Policy) Validate() error {
	if p.MaxActiveRequestsPerEpisode <= 0 || p.MaxStoredRequestsPerEpisode < p.MaxActiveRequestsPerEpisode || p.MaxReasonCodes <= 0 || p.MaxFactCodes <= 0 || p.MaxHypothesisPairs <= 0 || p.MinUtilityPermille < 0 || p.MinUtilityPermille > 1000 || p.MinDiscriminationPermille < 0 || p.MinDiscriminationPermille > 1000 || p.MinCoverageGainPermille < 0 || p.MinCoverageGainPermille > 1000 || p.SuppressRedundancyPermille < 0 || p.SuppressRedundancyPermille > 1000 || p.MinPreferredMarginPermille < 0 || p.MinPreferredMarginPermille > 1000 || p.DefaultTTL <= 0 || p.DeferredRecheckInterval <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}
func (p Policy) Fingerprint() string {
	payload, _ := json.Marshal(struct {
		MaxActive, MaxStored, MaxReasons, MaxFacts, MaxPairs                            int
		MinUtility, MinDiscrimination, MinCoverage, SuppressRedundancy, PreferredMargin int
		DefaultTTL, DeferredInterval                                                    int64
		IncludeHighSensitivity, PreserveSuppressed                                      bool
	}{p.MaxActiveRequestsPerEpisode, p.MaxStoredRequestsPerEpisode, p.MaxReasonCodes, p.MaxFactCodes, p.MaxHypothesisPairs, p.MinUtilityPermille, p.MinDiscriminationPermille, p.MinCoverageGainPermille, p.SuppressRedundancyPermille, p.MinPreferredMarginPermille, p.DefaultTTL.Nanoseconds(), p.DeferredRecheckInterval.Nanoseconds(), p.IncludeHighSensitivityRequests, p.PreserveSuppressedRequests})
	d := sha256.Sum256(payload)
	return "advisory-evidence-policy-v1:" + hex.EncodeToString(d[:])
}
func clamp(value int) int {
	if value < 0 {
		return 0
	}
	if value > 1000 {
		return 1000
	}
	return value
}
