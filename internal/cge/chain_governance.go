package cge

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type ChainStatus string

const (
	ChainStatusCandidate  ChainStatus = "candidate"
	ChainStatusShadow     ChainStatus = "shadow"
	ChainStatusEligible   ChainStatus = "eligible"
	ChainStatusPromoted   ChainStatus = "promoted"
	ChainStatusActive     ChainStatus = "active"
	ChainStatusDeclining  ChainStatus = "declining"
	ChainStatusSuperseded ChainStatus = "superseded"
	ChainStatusArchived   ChainStatus = "archived"
	ChainStatusRejected   ChainStatus = "rejected"
)

func (s ChainStatus) Validate() error {
	switch s {
	case ChainStatusCandidate, ChainStatusShadow, ChainStatusEligible, ChainStatusPromoted, ChainStatusActive, ChainStatusDeclining, ChainStatusSuperseded, ChainStatusArchived, ChainStatusRejected:
		return nil
	default:
		return fmt.Errorf("invalid chain status %q", s)
	}
}

type PromotionEvidence struct {
	CandidateOccurrences int
	ObservationWindow    time.Duration
	CandidatePerformance float64
	ActivePerformance    float64
	Contradictions       int
	InvariantViolations  int
	StableAfterRestart   bool
	RollbackAvailable    bool
	CandidateScope       string
	ActiveScope          string
}

type ChainPromotionPolicy struct {
	MinimumOccurrences     int
	MinimumWindow          time.Duration
	MinimumPerformanceGain float64
	MaximumContradictions  int
}

func (p ChainPromotionPolicy) Validate() error {
	if p.MinimumOccurrences <= 0 || p.MinimumWindow <= 0 || p.MinimumPerformanceGain < 0 || p.MaximumContradictions < 0 {
		return ErrInvalidPromotion
	}
	return nil
}

type ChainVersion struct {
	Reference ChainReference    `json:"reference" yaml:"reference"`
	Status    ChainStatus       `json:"status" yaml:"status"`
	Scope     string            `json:"scope" yaml:"scope"`
	CreatedAt time.Time         `json:"created_at" yaml:"created_at"`
	Evidence  PromotionEvidence `json:"evidence" yaml:"evidence"`
}

func (v ChainVersion) Validate() error {
	if err := v.Reference.Validate(); err != nil {
		return err
	}
	if err := v.Status.Validate(); err != nil {
		return err
	}
	if v.CreatedAt.IsZero() || strings.TrimSpace(v.Scope) == "" {
		return ErrInvalidPromotion
	}
	return nil
}

type ChainRegistry struct {
	mu       sync.RWMutex
	versions map[string][]ChainVersion
}

func NewChainRegistry() *ChainRegistry {
	return &ChainRegistry{versions: make(map[string][]ChainVersion)}
}

func (r *ChainRegistry) Register(version ChainVersion) error {
	if r == nil {
		return ErrInvalidPromotion
	}
	if err := version.Validate(); err != nil {
		return err
	}
	if version.Reference.Class == ChainClassInvariant && version.Status != ChainStatusActive {
		return fmt.Errorf("%w: invariant must remain active", ErrInvalidPromotion)
	}
	if version.Reference.Class == ChainClassLearned && version.Status == ChainStatusActive {
		return fmt.Errorf("%w: learned chain cannot be active at registration", ErrInvalidPromotion)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.versions[version.Reference.ChainID] {
		if existing.Reference.Version == version.Reference.Version {
			return fmt.Errorf("%w: duplicate chain version", ErrInvalidPromotion)
		}
	}
	r.versions[version.Reference.ChainID] = append(r.versions[version.Reference.ChainID], version)
	sort.Slice(r.versions[version.Reference.ChainID], func(i, j int) bool {
		return r.versions[version.Reference.ChainID][i].Reference.Version < r.versions[version.Reference.ChainID][j].Reference.Version
	})
	return nil
}

func (r *ChainRegistry) Select(chainID string) (ChainReference, bool) {
	if r == nil {
		return ChainReference{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := r.versions[chainID]
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].Status == ChainStatusActive {
			return versions[i].Reference, true
		}
	}
	return ChainReference{}, false
}

func (r *ChainRegistry) Promote(chainID string, candidate ChainReference, evidence PromotionEvidence, at time.Time, policy ChainPromotionPolicy) (ChainReference, error) {
	if r == nil {
		return ChainReference{}, ErrInvalidPromotion
	}
	if err := candidate.Validate(); err != nil || candidate.Class != ChainClassLearned || chainID == "" || candidate.ChainID != chainID {
		return ChainReference{}, ErrInvalidPromotion
	}
	if err := policy.Validate(); err != nil {
		return ChainReference{}, err
	}
	if evidence.CandidateOccurrences < policy.MinimumOccurrences || evidence.ObservationWindow < policy.MinimumWindow || evidence.CandidatePerformance < evidence.ActivePerformance+policy.MinimumPerformanceGain || evidence.Contradictions > policy.MaximumContradictions || evidence.InvariantViolations != 0 || !evidence.StableAfterRestart || !evidence.RollbackAvailable || !scopeWithin(evidence.CandidateScope, evidence.ActiveScope) {
		return ChainReference{}, ErrInvalidPromotion
	}
	if at.IsZero() {
		return ChainReference{}, ErrInvalidPromotion
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := r.versions[chainID]
	candidateIndex := -1
	activeIndex := -1
	for i := range versions {
		if versions[i].Reference == candidate && (versions[i].Status == ChainStatusCandidate || versions[i].Status == ChainStatusEligible || versions[i].Status == ChainStatusShadow) {
			candidateIndex = i
		}
		if versions[i].Status == ChainStatusActive && activeIndex < 0 {
			activeIndex = i
		}
	}
	if candidateIndex < 0 {
		return ChainReference{}, ErrInvalidPromotion
	}
	if activeIndex >= 0 && versions[activeIndex].Reference.Class == ChainClassInvariant {
		return ChainReference{}, ErrInvalidPromotion
	}
	if activeIndex >= 0 {
		versions[activeIndex].Status = ChainStatusSuperseded
	}
	versions[candidateIndex].Status = ChainStatusPromoted
	active := versions[candidateIndex]
	active.Reference.Version = candidate.Version + 1
	active.Status = ChainStatusActive
	active.CreatedAt = at
	active.Evidence = evidence
	versions = append(versions, active)
	r.versions[chainID] = versions
	return active.Reference, nil
}

func scopeWithin(candidate, active string) bool {
	candidate, active = strings.TrimSpace(candidate), strings.TrimSpace(active)
	if candidate == "" || active == "" {
		return false
	}
	return candidate == active || strings.HasPrefix(candidate, active+"/")
}
