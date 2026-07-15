package cge

import (
	"math"
	"sort"
	"strings"
	"time"

	eventpkg "synora/internal/event"
	"synora/internal/state"
	"synora/pkg/contract"
)

// DangerRuntime is the temporal projection of CGE evidence into the current
// system state. It deliberately reads immutable-ish chain projections and
// never rewrites events or chain evaluations.
type DangerRuntime struct {
	config contract.DangerDecayConfig
	now    func() time.Time
	debug  bool

	belowSince time.Time
	idleSince  time.Time
}

type DangerRuntimeResult struct {
	Changed       bool
	Significant   bool
	PreviousScore float64
	CurrentScore  float64
	PreviousLevel string
	CurrentLevel  string
	PreviousState string
	CurrentState  string
	Reasons       []string
	Locked        bool
}

func NewDangerRuntime(config contract.DangerDecayConfig) *DangerRuntime {
	return &DangerRuntime{config: contract.NormalizeDangerDecayConfig(config), now: func() time.Time { return time.Now().UTC() }}
}

func (r *DangerRuntime) SetNow(now func() time.Time) {
	if r != nil && now != nil {
		r.now = now
	}
}

func (r *DangerRuntime) SetDebug(enabled bool) {
	if r != nil {
		r.debug = enabled
	}
}

func (r *DangerRuntime) Config() contract.DangerDecayConfig {
	if r == nil {
		return contract.DefaultDangerDecayConfig()
	}
	return r.config
}

func (r *DangerRuntime) SetConfig(config contract.DangerDecayConfig) {
	if r != nil {
		r.config = contract.NormalizeDangerDecayConfig(config)
	}
}

// Recompute projects all active and recent closed chains at now. initial is
// used at boot: expired persisted danger must not survive a restart.
func (r *DangerRuntime) Recompute(store *state.Store, chains *eventpkg.ChainManager, now time.Time, initial bool) DangerRuntimeResult {
	if r == nil || store == nil {
		return DangerRuntimeResult{}
	}
	if now.IsZero() {
		now = r.now().UTC()
	}
	config := contract.NormalizeDangerDecayConfig(r.config)
	current := store.SystemState()
	previous := current
	current.DangerDecayEnabled = config.Enabled
	current.DangerDecayWindowMinutes = config.WindowMinutes
	current.DangerDecayHalfLifeMinutes = config.HalfLifeMinutes
	current.DangerDecayLastTick = now.UTC()
	current.DangerDecay = map[string]any{
		"enabled": config.Enabled, "last_tick": now.UTC(),
		"window_minutes": config.WindowMinutes, "half_life_minutes": config.HalfLifeMinutes,
	}

	result := DangerRuntimeResult{
		PreviousScore: current.DangerScoreCurrent,
		PreviousLevel: current.DangerLevel,
		PreviousState: current.LastState,
	}
	if current.DangerScoreCurrent == 0 && current.DangerScore != 0 {
		result.PreviousScore = current.DangerScore
	}
	if !config.Enabled {
		store.SetSystemState(current)
		return result
	}

	contributions := make([]contribution, 0)
	if chains != nil {
		for _, chain := range chains.List(eventpkg.ChainFilter{Status: "all"}) {
			if chain == nil || chain.Simulated || chainContainsManualRisk(chain) {
				continue
			}
			at := chain.LastEventAt
			if at.IsZero() {
				at = chain.LastSignificantEventAt
			}
			if at.IsZero() {
				at = chain.UpdatedAt
			}
			age := now.Sub(at)
			if age < 0 {
				age = 0
			}
			if age > time.Duration(config.WindowMinutes)*time.Minute {
				continue
			}
			score := clampScore(chain.DangerScore)
			if score <= 0 {
				score = clampScore(chain.MaxDangerScore)
			}
			if score <= 0 {
				continue
			}
			// Security mode is context, not a standalone alert. It adjusts the
			// effective contribution while the original chain score remains
			// unchanged and historical.
			effective := clampScore(score * securityMultiplier(current.Security) * math.Pow(0.5, age.Seconds()/(float64(config.HalfLifeMinutes)*60)))
			contributions = append(contributions, contribution{
				Score: effective, Original: score, At: at, Reason: chainReason(chain),
				Critical: chain.Critical && chain.Status == contract.EventChainOpen &&
					(chain.DangerLevel == contract.DangerCritical || chain.CurrentState == "intrusion" || chain.CurrentState == "break-in"),
			})
		}
	}

	manualActive := current.ManualRiskActive && (current.ManualRiskExpiresAt.IsZero() || now.Before(current.ManualRiskExpiresAt))
	if manualActive && !current.ManualRiskTest {
		manualScore := clampScore(current.ManualRiskScore)
		if manualScore <= 0 {
			manualScore = manualRiskScore(current.ManualRiskLevel)
		}
		if manualScore > 0 {
			contributions = append(contributions, contribution{Score: manualScore, Original: manualScore, At: now, Reason: "manual risk active", Manual: true})
		}
	}

	locked := config.LockIntrusionUntilReset && (current.IntrusionActive || current.EmergencyActive)
	for _, item := range contributions {
		if item.Critical {
			locked = config.LockIntrusionUntilReset
			break
		}
	}
	if locked {
		contributions = append(contributions, contribution{Score: 0.95, Original: 0.95, At: now, Reason: "critical/intrusion unresolved", Critical: true})
	}

	peak := current.DangerScorePeak
	for _, item := range contributions {
		if item.Original > peak {
			peak = item.Original
		}
	}
	score, reasons := maxContribution(contributions)
	if len(contributions) > 0 && current.Security.Mode != contract.SecurityModeHome {
		reasons = append(reasons, "security mode: "+string(current.Security.Mode))
	}
	level := dangerLevelForScore(score)
	if locked {
		level = contract.DangerCritical
		score = math.Max(score, 0.95)
	}
	level = r.applyHysteresis(current.DangerLevel, level, score, now, initial, locked, config)

	current.DangerScoreCurrent = score
	current.DangerScore = score
	current.DangerScorePeak = peak
	current.DangerLevel = string(level)
	current.DangerKnown = true
	current.DangerReasonsCurrent = reasons
	current.DangerSource = dangerSource(contributions, locked)
	if manualActive && current.DangerSource == "manual" {
		current.DangerSource = "manual"
	}

	if score < config.IdleBelowScore && !locked && !manualActive {
		if r.idleSince.IsZero() {
			r.idleSince = now
		}
		if initial || now.Sub(r.idleSince) >= time.Duration(config.IdleStableSeconds)*time.Second {
			current.LastState = "idle"
		}
	} else {
		r.idleSince = time.Time{}
		if !locked {
			if level == contract.DangerHigh || level == contract.DangerCritical || level == contract.DangerMediumHigh {
				current.LastState = "suspicious"
			} else if level == contract.DangerMedium || level == contract.DangerLow {
				current.LastState = "activity"
			}
		}
	}
	if locked {
		current.LastState = "intrusion"
		current.IntrusionActive = true
		if current.IntrusionTime.IsZero() {
			current.IntrusionTime = now
		}
	}
	if current.LastState != previous.LastState {
		current.PreviousState = previous.LastState
		current.LastStateTime = now
	}
	if current.DangerScoreUpdatedAt.IsZero() || math.Abs(current.DangerScoreCurrent-previous.DangerScoreCurrent) >= 0.0001 || initial {
		current.DangerScoreUpdatedAt = now
	}
	if r.debug {
		current.DangerDecayDebug = map[string]any{"contributions": debugContributions(contributions, now)}
		current.DangerDecay["debug"] = current.DangerDecayDebug
	} else {
		current.DangerDecayDebug = nil
	}
	store.SetSystemState(current)

	result.CurrentScore = score
	result.CurrentLevel = current.DangerLevel
	result.CurrentState = current.LastState
	result.Reasons = append([]string(nil), reasons...)
	result.Locked = locked
	result.Changed = initial || math.Abs(score-result.PreviousScore) >= 0.0001 || result.CurrentLevel != result.PreviousLevel || result.CurrentState != result.PreviousState
	result.Significant = math.Abs(score-result.PreviousScore) >= 0.03 || result.CurrentLevel != result.PreviousLevel || result.CurrentState != result.PreviousState
	return result
}

type contribution struct {
	Score, Original float64
	At              time.Time
	Reason          string
	Critical        bool
	Manual          bool
}

func (r *DangerRuntime) applyHysteresis(current string, desired contract.DangerLevel, score float64, now time.Time, initial, locked bool, config contract.DangerDecayConfig) contract.DangerLevel {
	if initial || locked {
		r.belowSince = time.Time{}
		return desired
	}
	currentLevel := normalizeLevel(current)
	if dangerRank(desired) >= dangerRank(currentLevel) {
		r.belowSince = time.Time{}
		return desired
	}
	if score >= levelThreshold(currentLevel) {
		r.belowSince = time.Time{}
		return currentLevel
	}
	if r.belowSince.IsZero() {
		r.belowSince = now
	}
	if now.Sub(r.belowSince) < time.Duration(config.DowngradeStableSeconds)*time.Second {
		return currentLevel
	}
	r.belowSince = time.Time{}
	return desired
}

func maxContribution(items []contribution) (float64, []string) {
	if len(items) == 0 {
		return 0, []string{}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	return items[0].Score, uniqueReasons(items)
}

func uniqueReasons(items []contribution) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		if item.Reason != "" && !seen[item.Reason] {
			seen[item.Reason] = true
			out = append(out, item.Reason)
		}
	}
	return out
}

func debugContributions(items []contribution, now time.Time) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"reason": item.Reason, "original_score": item.Original, "effective_score": item.Score, "age_seconds": now.Sub(item.At).Seconds(), "critical": item.Critical, "manual": item.Manual})
	}
	return out
}

func chainReason(chain *contract.EventChain) string {
	if chain == nil {
		return ""
	}
	if len(chain.DangerReasons) > 0 {
		return strings.Join(chain.DangerReasons, "; ")
	}
	if chain.Title != "" {
		return chain.Title
	}
	return "recent CGE chain"
}

func chainContainsManualRisk(chain *contract.EventChain) bool {
	if chain == nil {
		return false
	}
	for _, eventType := range chain.SignificantEventTypes {
		if contract.NormalizeEventType(eventType) == contract.EventManualRisk {
			return true
		}
	}
	for _, recent := range chain.RecentEvents {
		if contract.NormalizeEventType(recent.Type) == contract.EventManualRisk {
			return true
		}
	}
	return false
}

func dangerSource(items []contribution, locked bool) string {
	if locked {
		return "intrusion"
	}
	for _, item := range items {
		if item.Manual {
			return "manual"
		}
	}
	if len(items) > 0 {
		return "real"
	}
	return "none"
}

func securityMultiplier(mode contract.SecurityModeState) float64 {
	switch mode.Mode {
	case contract.SecurityModeNight:
		return 1.05
	case contract.SecurityModeAway:
		return 1.10
	case contract.SecurityModeHighSecurity:
		return 1.15
	default:
		return 1
	}
}

func manualRiskScore(level string) float64 {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return 0.95
	case "high":
		return 0.75
	case "medium_high":
		return 0.65
	case "medium":
		return 0.50
	case "low":
		return 0.25
	default:
		return 0
	}
}

func clampScore(score float64) float64 {
	if score < 0 || math.IsNaN(score) {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func dangerLevelForScore(score float64) contract.DangerLevel {
	switch {
	case score >= .90:
		return contract.DangerCritical
	case score >= .75:
		return contract.DangerHigh
	case score >= .65:
		return contract.DangerMediumHigh
	case score >= .50:
		return contract.DangerMedium
	case score >= .25:
		return contract.DangerLow
	default:
		return contract.DangerNone
	}
}

func normalizeLevel(value string) contract.DangerLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return contract.DangerCritical
	case "high":
		return contract.DangerHigh
	case "medium_high":
		return contract.DangerMediumHigh
	case "medium":
		return contract.DangerMedium
	case "low":
		return contract.DangerLow
	default:
		return contract.DangerNone
	}
}

func dangerRank(level contract.DangerLevel) int {
	switch level {
	case contract.DangerCritical:
		return 5
	case contract.DangerHigh:
		return 4
	case contract.DangerMediumHigh:
		return 3
	case contract.DangerMedium:
		return 2
	case contract.DangerLow:
		return 1
	default:
		return 0
	}
}

func levelThreshold(level contract.DangerLevel) float64 {
	switch level {
	case contract.DangerCritical:
		return .90
	case contract.DangerHigh:
		return .75
	case contract.DangerMediumHigh:
		return .65
	case contract.DangerMedium:
		return .50
	case contract.DangerLow:
		return .25
	default:
		return 0
	}
}
