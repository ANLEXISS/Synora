package cge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	CandidateOccurrences int           `json:"candidate_occurrences" yaml:"candidate_occurrences"`
	ObservationWindow    time.Duration `json:"observation_window" yaml:"observation_window"`
	CandidatePerformance float64       `json:"candidate_performance" yaml:"candidate_performance"`
	ActivePerformance    float64       `json:"active_performance" yaml:"active_performance"`
	Contradictions       int           `json:"contradictions" yaml:"contradictions"`
	InvariantViolations  int           `json:"invariant_violations" yaml:"invariant_violations"`
	StableAfterRestart   bool          `json:"stable_after_restart" yaml:"stable_after_restart"`
	RollbackAvailable    bool          `json:"rollback_available" yaml:"rollback_available"`
	CandidateScope       string        `json:"candidate_scope" yaml:"candidate_scope"`
	ActiveScope          string        `json:"active_scope" yaml:"active_scope"`
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

// ChainGovernanceRecord is the append-only audit record for learned-chain
// lifecycle changes. Critical and invariant chains remain sourced from their
// bootstrap contracts; learned records never overwrite a prior version.
type ChainGovernanceRecord struct {
	Operation      string          `json:"operation" yaml:"operation"`
	Chain          ChainVersion    `json:"chain" yaml:"chain"`
	PreviousActive *ChainReference `json:"previous_active,omitempty" yaml:"previous_active,omitempty"`
	RecordedAt     time.Time       `json:"recorded_at" yaml:"recorded_at"`
	Reason         string          `json:"reason,omitempty" yaml:"reason,omitempty"`
}

const (
	ChainGovernancePromotion = "promotion"
	ChainGovernanceRollback  = "rollback"
)

func (r ChainGovernanceRecord) Validate() error {
	if r.Operation != ChainGovernancePromotion && r.Operation != ChainGovernanceRollback {
		return fmt.Errorf("%w: invalid governance operation", ErrInvalidPromotion)
	}
	if err := r.Chain.Validate(); err != nil {
		return err
	}
	if r.Chain.Reference.Class != ChainClassLearned {
		return fmt.Errorf("%w: governance records are learned-only", ErrInvalidPromotion)
	}
	if r.Chain.Status != ChainStatusActive {
		return fmt.Errorf("%w: governance records must contain an active immutable version", ErrInvalidPromotion)
	}
	if r.PreviousActive != nil {
		if err := r.PreviousActive.Validate(); err != nil {
			return err
		}
		if r.PreviousActive.ChainID != r.Chain.Reference.ChainID || *r.PreviousActive == r.Chain.Reference {
			return fmt.Errorf("%w: invalid previous active reference", ErrInvalidPromotion)
		}
	}
	if r.RecordedAt.IsZero() {
		return fmt.Errorf("%w: governance record timestamp is required", ErrInvalidPromotion)
	}
	if r.RecordedAt.Before(r.Chain.CreatedAt) {
		return fmt.Errorf("%w: governance record predates chain version", ErrInvalidPromotion)
	}
	if r.Chain.Evidence.CandidatePerformance < 0 || r.Chain.Evidence.CandidatePerformance > 1 || math.IsNaN(r.Chain.Evidence.CandidatePerformance) || math.IsInf(r.Chain.Evidence.CandidatePerformance, 0) || r.Chain.Evidence.ActivePerformance < 0 || r.Chain.Evidence.ActivePerformance > 1 || math.IsNaN(r.Chain.Evidence.ActivePerformance) || math.IsInf(r.Chain.Evidence.ActivePerformance, 0) {
		return fmt.Errorf("%w: invalid governance performance evidence", ErrInvalidPromotion)
	}
	if err := validateGovernanceText(r.Reason, 512); err != nil {
		return err
	}
	return nil
}

func validateGovernanceText(value string, max int) error {
	value = strings.TrimSpace(value)
	if value != "" && len(value) > max {
		return fmt.Errorf("%w: governance text is too long", ErrInvalidPromotion)
	}
	return nil
}

// ChainGovernanceStore is the durable lifecycle boundary for learned chains.
// Load returns validated records; malformed or truncated records fail closed.
type ChainGovernanceStore interface {
	Load(context.Context) ([]ChainVersion, error)
	Append(context.Context, ChainGovernanceRecord) error
}

type FileChainGovernanceStore struct {
	mu   sync.Mutex
	path string
}

func NewFileChainGovernanceStore(path string) (*FileChainGovernanceStore, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrInvalidPromotion
	}
	return &FileChainGovernanceStore{path: path}, nil
}

func (s *FileChainGovernanceStore) Append(ctx context.Context, record ChainGovernanceRecord) error {
	if s == nil {
		return ErrInvalidPromotion
	}
	if err := ValidateStoreWrite(record); err != nil {
		return err
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%w: governance encode", ErrInvalidPromotion)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("%w: governance directory", ErrInvalidPromotion)
	}
	_ = os.Chmod(filepath.Dir(s.path), 0o700)
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w: governance open", ErrInvalidPromotion)
	}
	defer file.Close()
	_ = file.Chmod(0o600)
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("%w: governance append", ErrInvalidPromotion)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("%w: governance sync", ErrInvalidPromotion)
	}
	return nil
}

func (s *FileChainGovernanceStore) Load(ctx context.Context) ([]ChainVersion, error) {
	if s == nil {
		return nil, ErrInvalidPromotion
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: governance read", ErrInvalidPromotion)
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr != nil || info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("%w: governance permissions", ErrInvalidPromotion)
	}
	var out []ChainVersion
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 256*1024)
	for scanner.Scan() {
		var record ChainGovernanceRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("%w: governance corruption", ErrInvalidPromotion)
		}
		if err := record.Validate(); err != nil {
			return nil, fmt.Errorf("%w: governance corruption", ErrInvalidPromotion)
		}
		out = append(out, record.Chain)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: governance scan", ErrInvalidPromotion)
	}
	return out, nil
}

func (v ChainVersion) Validate() error {
	if err := v.Reference.Validate(); err != nil {
		return err
	}
	if err := v.Status.Validate(); err != nil {
		return err
	}
	if v.CreatedAt.IsZero() || strings.TrimSpace(v.Scope) == "" || len([]rune(v.Scope)) > 256 || strings.ContainsAny(v.Scope, "\r\n") {
		return ErrInvalidPromotion
	}
	if v.Evidence.CandidatePerformance < 0 || v.Evidence.CandidatePerformance > 1 || math.IsNaN(v.Evidence.CandidatePerformance) || math.IsInf(v.Evidence.CandidatePerformance, 0) || v.Evidence.ActivePerformance < 0 || v.Evidence.ActivePerformance > 1 || math.IsNaN(v.Evidence.ActivePerformance) || math.IsInf(v.Evidence.ActivePerformance, 0) {
		return ErrInvalidPromotion
	}
	return nil
}

type ChainRegistry struct {
	mu       sync.RWMutex
	versions map[string][]ChainVersion
	store    ChainGovernanceStore
}

func NewChainRegistry() *ChainRegistry {
	return &ChainRegistry{versions: make(map[string][]ChainVersion)}
}

func NewChainRegistryWithStore(store ChainGovernanceStore) (*ChainRegistry, error) {
	r := NewChainRegistry()
	r.store = store
	if store == nil {
		return r, nil
	}
	versions, err := store.Load(context.Background())
	if err != nil {
		return nil, err
	}
	for _, version := range versions {
		if version.Reference.Class == ChainClassInvariant && version.Status != ChainStatusActive {
			return nil, ErrInvalidPromotion
		}
		if err := r.registerLoaded(version); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *ChainRegistry) registerLoaded(version ChainVersion) error {
	if err := version.Validate(); err != nil {
		return err
	}
	for _, existing := range r.versions[version.Reference.ChainID] {
		if existing.Reference.Version == version.Reference.Version {
			return ErrInvalidPromotion
		}
	}
	r.versions[version.Reference.ChainID] = append(r.versions[version.Reference.ChainID], version)
	sort.Slice(r.versions[version.Reference.ChainID], func(i, j int) bool {
		return r.versions[version.Reference.ChainID][i].Reference.Version < r.versions[version.Reference.ChainID][j].Reference.Version
	})
	return nil
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
	version, ok := r.SelectVersion(chainID)
	if !ok {
		return ChainReference{}, false
	}
	return version.Reference, true
}

// SelectVersion returns the immutable active version, including the persisted
// governance scope and evidence used by the selector's fail-closed checks.
func (r *ChainRegistry) SelectVersion(chainID string) (ChainVersion, bool) {
	if r == nil {
		return ChainVersion{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	versions := r.versions[chainID]
	for i := len(versions) - 1; i >= 0; i-- {
		if versions[i].Status == ChainStatusActive {
			return versions[i], true
		}
	}
	return ChainVersion{}, false
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
	nextVersions := append([]ChainVersion(nil), versions...)
	candidateIndex := -1
	activeIndex := -1
	for i := range nextVersions {
		if nextVersions[i].Reference == candidate && (nextVersions[i].Status == ChainStatusCandidate || nextVersions[i].Status == ChainStatusEligible || nextVersions[i].Status == ChainStatusShadow) {
			candidateIndex = i
		}
		if nextVersions[i].Status == ChainStatusActive {
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
		nextVersions[activeIndex].Status = ChainStatusSuperseded
	}
	nextVersions[candidateIndex].Status = ChainStatusPromoted
	active := nextVersions[candidateIndex]
	active.Reference.Version = nextChainVersion(nextVersions)
	active.Status = ChainStatusActive
	active.CreatedAt = at
	active.Evidence = evidence
	if r.store != nil {
		record := ChainGovernanceRecord{Operation: ChainGovernancePromotion, Chain: active, RecordedAt: at, Reason: "governed promotion"}
		if activeIndex >= 0 {
			previous := versions[activeIndex].Reference
			record.PreviousActive = &previous
		}
		if err := r.store.Append(context.Background(), record); err != nil {
			return ChainReference{}, err
		}
	}
	nextVersions = append(nextVersions, active)
	r.versions[chainID] = nextVersions
	return active.Reference, nil
}

// Rollback activates a previously persisted learned version as a new immutable
// version. The superseded active version remains in the registry and the
// rollback itself is append-only journaled.
func (r *ChainRegistry) Rollback(chainID string, target ChainReference, at time.Time) (ChainReference, error) {
	if r == nil || chainID == "" || target.Class != ChainClassLearned || at.IsZero() {
		return ChainReference{}, ErrInvalidPromotion
	}
	if err := target.Validate(); err != nil {
		return ChainReference{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := r.versions[chainID]
	next := append([]ChainVersion(nil), versions...)
	activeIndex, targetIndex := -1, -1
	for i := range next {
		if next[i].Status == ChainStatusActive {
			activeIndex = i
		}
		if next[i].Reference == target && next[i].Status != ChainStatusArchived && next[i].Status != ChainStatusRejected {
			targetIndex = i
		}
	}
	if activeIndex < 0 || targetIndex < 0 || next[activeIndex].Reference == target {
		return ChainReference{}, ErrInvalidPromotion
	}
	previous := next[activeIndex].Reference
	next[activeIndex].Status = ChainStatusSuperseded
	active := next[targetIndex]
	active.Reference.Version = nextChainVersion(next)
	active.Status = ChainStatusActive
	active.CreatedAt = at
	next = append(next, active)
	if r.store != nil {
		record := ChainGovernanceRecord{Operation: ChainGovernanceRollback, Chain: active, PreviousActive: &previous, RecordedAt: at, Reason: "governed rollback"}
		if err := r.store.Append(context.Background(), record); err != nil {
			return ChainReference{}, err
		}
	}
	r.versions[chainID] = next
	return active.Reference, nil
}

func nextChainVersion(versions []ChainVersion) uint64 {
	var highest uint64
	for _, version := range versions {
		if version.Reference.Version > highest {
			highest = version.Reference.Version
		}
	}
	if highest == ^uint64(0) {
		return 0
	}
	return highest + 1
}

func scopeWithin(candidate, active string) bool {
	candidate, active = strings.TrimSpace(candidate), strings.TrimSpace(active)
	if candidate == "" || active == "" {
		return false
	}
	return candidate == active || strings.HasPrefix(candidate, active+"/")
}
