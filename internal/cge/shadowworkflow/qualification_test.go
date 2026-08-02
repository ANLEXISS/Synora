package shadowworkflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/advisoryrequests"
	"synora/internal/cge/authorizationboundary"
	"synora/internal/cge/capabilitymapping"
	"synora/internal/cge/durableworkflow"
)

type qualificationClock struct {
	mu  sync.RWMutex
	now time.Time
}

func newQualificationClock() *qualificationClock {
	return &qualificationClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *qualificationClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *qualificationClock) Advance(value time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(value)
	c.mu.Unlock()
}

type qualificationCapabilityProvider struct {
	catalog   capabilitymapping.CapabilityCatalog
	inventory capabilitymapping.CapabilityInventory
	err       error
	available bool
}

func newQualificationCapabilityProvider() *qualificationCapabilityProvider {
	catalog := capabilitymapping.Catalog()
	instances := make([]capabilitymapping.CapabilityInstance, 0, len(catalog.Definitions))
	for _, definition := range catalog.Definitions {
		instance := capabilitymapping.CapabilityInstance{
			ID: capabilitymapping.CapabilityInstanceID("capability-instance-" + string(definition.Kind)), Kind: definition.Kind,
			DomainID: "synthetic-domain", ProviderID: "provider-alpha",
			Status:    capabilitymapping.CapabilityStatusAvailable,
			Quality:   capabilitymapping.CapabilityQuality{ReliabilityPermille: 1000, CompletenessPermille: 1000, FreshnessPermille: 1000, Calibrated: true, SourceCount: 1},
			CostClass: capabilitymapping.CapabilityCostLow, LatencyClass: capabilitymapping.CapabilityLatencyShort, SensitivityClass: capabilitymapping.CapabilitySensitivityLow,
			Revision: 1, DefinitionFingerprint: capabilitymapping.CapabilityDefinitionFingerprint(definition),
		}
		instance.Fingerprint = capabilitymapping.CapabilityInstanceFingerprint(instance)
		instances = append(instances, instance)
	}
	inventory := capabilitymapping.CapabilityInventory{ID: "synthetic-inventory", DomainID: "synthetic-domain", Revision: 1, Instances: instances, CatalogFingerprint: catalog.Fingerprint}
	inventory.Fingerprint = capabilitymapping.CapabilityInventoryFingerprint(inventory)
	return &qualificationCapabilityProvider{catalog: catalog, inventory: inventory, available: true}
}

func (p *qualificationCapabilityProvider) SnapshotFor(context.Context, advisoryrequests.AdvisoryEvidenceRequest) (capabilitymapping.CapabilityCatalog, capabilitymapping.CapabilityInventory, bool, error) {
	return p.catalog, p.inventory, p.available, p.err
}

type qualificationAuthorizationProvider struct {
	mu          sync.RWMutex
	allow       bool
	available   bool
	grantMode   qualificationGrantMode
	err         error
	panicNow    bool
	block       bool
	hardBlock   <-chan struct{}
	entered     chan struct{}
	enteredOnce sync.Once
}

type qualificationGrantMode string

const (
	qualificationGrantNone         qualificationGrantMode = "none"
	qualificationGrantConfirmation qualificationGrantMode = "confirmation"
	qualificationGrantValid        qualificationGrantMode = "valid"
	qualificationGrantExpired      qualificationGrantMode = "expired"
	qualificationGrantRevoked      qualificationGrantMode = "revoked"
)

func (p *qualificationAuthorizationProvider) InputsFor(ctx context.Context, mapping capabilitymapping.CapabilityMappingAssessment) (authorizationboundary.AuthorizationContext, authorizationboundary.AuthorizationPolicySet, authorizationboundary.ExternalGrantSnapshot, bool, error) {
	p.mu.RLock()
	panicNow, block, hardBlock, providerErr, available, allow, grantMode := p.panicNow, p.block, p.hardBlock, p.err, p.available, p.allow, p.grantMode
	p.mu.RUnlock()
	if panicNow {
		panic("qualification provider panic")
	}
	if block {
		p.enteredOnce.Do(func() {
			if p.entered != nil {
				close(p.entered)
			}
		})
		<-ctx.Done()
		return authorizationboundary.AuthorizationContext{}, authorizationboundary.AuthorizationPolicySet{}, authorizationboundary.ExternalGrantSnapshot{}, false, ctx.Err()
	}
	if hardBlock != nil {
		p.enteredOnce.Do(func() {
			if p.entered != nil {
				close(p.entered)
			}
		})
		<-hardBlock
	}
	if providerErr != nil {
		return authorizationboundary.AuthorizationContext{}, authorizationboundary.AuthorizationPolicySet{}, authorizationboundary.ExternalGrantSnapshot{}, false, providerErr
	}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	kind := capabilitymapping.CapabilityIdentityObservation
	if len(mapping.Candidates) > 0 {
		kind = mapping.Candidates[0].CapabilityKind
	}
	purpose := authorizationboundary.PurposeResolveIdentityAmbiguity
	switch kind {
	case capabilitymapping.CapabilityContextStateObservation:
		purpose = authorizationboundary.PurposeVerifyContextState
	case capabilitymapping.CapabilityIdentityContinuityObservation:
		purpose = authorizationboundary.PurposeVerifyIdentityContinuity
	case capabilitymapping.CapabilitySpatialRelationObservation:
		purpose = authorizationboundary.PurposeVerifySpatialContinuity
	case capabilitymapping.CapabilitySourceConsistencyObservation:
		purpose = authorizationboundary.PurposeVerifySourceConsistency
	case capabilitymapping.CapabilityTemporalRepetitionObservation:
		purpose = authorizationboundary.PurposeObserveTemporalRepetition
	case capabilitymapping.CapabilityPatternAlignmentObservation:
		purpose = authorizationboundary.PurposeVerifyPatternAlignment
	case capabilitymapping.CapabilityEntityMultiplicityObservation:
		purpose = authorizationboundary.PurposeVerifyEntityMultiplicity
	case capabilitymapping.CapabilityInformationCompletenessObservation:
		purpose = authorizationboundary.PurposeImproveInformationCompleteness
	}
	contextValue := authorizationboundary.AuthorizationContext{ID: "synthetic-context", DomainID: "synthetic-domain", PurposeCode: purpose, RequestedAt: at, ValidUntil: at.Add(time.Hour), RequestActorClass: "operator", RequestOrigin: "qualification"}
	contextValue.Fingerprint = authorizationboundary.AuthorizationContextFingerprint(contextValue)
	set := authorizationboundary.DefaultPolicySet()
	if allow || (grantMode != "" && grantMode != qualificationGrantNone) {
		rule := authorizationboundary.AuthorizationRule{ID: "synthetic-allow", Effect: authorizationboundary.EffectAllowEligibility, CapabilityKinds: []capabilitymapping.CapabilityKind{kind}, PurposeCodes: []authorizationboundary.AuthorizationPurposeCode{purpose}, DomainIDs: []string{"synthetic-domain"}, MaximumSensitivityClass: capabilitymapping.CapabilitySensitivityModerate, MaximumCostClass: capabilitymapping.CapabilityCostHigh, MaximumLatencyClass: capabilitymapping.CapabilityLatencyExtended, MinimumQualityPermille: 400, Priority: 1, ReasonCode: "synthetic_allow"}
		if grantMode == qualificationGrantConfirmation {
			rule.ID = "synthetic-confirmation"
			rule.Effect = authorizationboundary.EffectRequireExternalConfirmation
			rule.ReasonCode = "synthetic_confirmation_required"
		}
		if grantMode == qualificationGrantValid || grantMode == qualificationGrantExpired || grantMode == qualificationGrantRevoked {
			rule.RequiredGrantKinds = []authorizationboundary.ExternalGrantKind{authorizationboundary.GrantPrivacyConsent}
		}
		set.Rules = []authorizationboundary.AuthorizationRule{rule}
		set.Fingerprint = authorizationboundary.AuthorizationPolicySetFingerprint(set)
	}
	grants := authorizationboundary.ExternalGrantSnapshot{Revision: 1, Index: map[authorizationboundary.ExternalGrantID]int{}}
	if grantMode == qualificationGrantValid || grantMode == qualificationGrantExpired || grantMode == qualificationGrantRevoked {
		grant := authorizationboundary.ExternalGrant{ID: "synthetic-privacy-grant", Kind: authorizationboundary.GrantPrivacyConsent, SubjectClass: "operator", DomainID: "synthetic-domain", PurposeCodes: []authorizationboundary.AuthorizationPurposeCode{purpose}, CapabilityKinds: []capabilitymapping.CapabilityKind{kind}, ValidFrom: at.Add(-time.Hour), ValidUntil: at.Add(time.Hour), IssuerID: "synthetic-issuer", Revision: 1}
		if grantMode == qualificationGrantExpired {
			grant.ValidUntil = at.Add(-time.Minute)
		}
		if grantMode == qualificationGrantRevoked {
			revokedAt := at.Add(-time.Minute)
			grant.Revoked = true
			grant.RevokedAt = &revokedAt
		}
		grant.Fingerprint = authorizationboundary.ExternalGrantFingerprint(grant)
		grants.Grants = []authorizationboundary.ExternalGrant{grant}
		grants.Index[grant.ID] = 0
	}
	grants.Fingerprint = authorizationboundary.ExternalGrantSnapshotFingerprint(grants)
	return contextValue, set, grants, available, nil
}

func (p *qualificationAuthorizationProvider) setFailure(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
}

func (p *qualificationAuthorizationProvider) setAllow(value bool) {
	p.mu.Lock()
	p.allow = value
	p.mu.Unlock()
}

func (p *qualificationAuthorizationProvider) setBlock(value bool) {
	p.mu.Lock()
	p.block = value
	p.mu.Unlock()
}

type qualificationStore struct {
	mu               sync.RWMutex
	base             durableworkflow.Store
	failAppendBefore bool
	failAppend       bool
	failSync         bool
	failCheckpoint   bool
	appendThenPanic  bool
	appendCalls      int
	syncCalls        int
	checkpointCalls  int
}

func (s *qualificationStore) Append(record durableworkflow.Record) error {
	s.mu.RLock()
	failBefore, appendThenPanic, failAppend := s.failAppendBefore, s.appendThenPanic, s.failAppend
	s.mu.RUnlock()
	s.appendCalls++
	if failBefore {
		return fmt.Errorf("%w: qualification append before write", durableworkflow.ErrCommitNotDurable)
	}
	if err := s.base.Append(record); err != nil {
		return err
	}
	if appendThenPanic {
		s.mu.Lock()
		s.appendThenPanic = false
		s.mu.Unlock()
		panic("qualification append publication interruption")
	}
	if failAppend {
		return fmt.Errorf("%w: qualification append", durableworkflow.ErrCommitNotDurable)
	}
	return nil
}

func (s *qualificationStore) Sync() error {
	s.mu.RLock()
	failSync := s.failSync
	s.mu.RUnlock()
	s.syncCalls++
	if err := s.base.(durableworkflow.SyncStore).Sync(); err != nil {
		return err
	}
	if failSync {
		return fmt.Errorf("%w: qualification fsync", durableworkflow.ErrCommitNotDurable)
	}
	return nil
}

func (s *qualificationStore) Load() (durableworkflow.RecoveryInput, error) {
	return s.base.Load()
}

func (s *qualificationStore) WriteCheckpoint(checkpoint durableworkflow.Checkpoint) error {
	s.mu.RLock()
	failCheckpoint := s.failCheckpoint
	s.mu.RUnlock()
	s.checkpointCalls++
	if failCheckpoint {
		return fmt.Errorf("%w: qualification checkpoint", durableworkflow.ErrCommitNotDurable)
	}
	return s.base.WriteCheckpoint(checkpoint)
}

func (s *qualificationStore) Close() error { return nil }

func (s *qualificationStore) setAppendBefore(value bool) {
	s.mu.Lock()
	s.failAppendBefore = value
	s.mu.Unlock()
}

func (s *qualificationStore) setAppendPanic(value bool) {
	s.mu.Lock()
	s.appendThenPanic = value
	s.mu.Unlock()
}

func (s *qualificationStore) setSyncFailure(value bool) {
	s.mu.Lock()
	s.failSync = value
	s.mu.Unlock()
}

func (s *qualificationStore) setCheckpointFailure(value bool) {
	s.mu.Lock()
	s.failCheckpoint = value
	s.mu.Unlock()
}

func newInjectedRuntime(t *testing.T, cfg Config, clock Clock, store durableworkflow.Store, capabilityProvider CapabilityInputProvider, authorizationProvider AuthorizationInputProvider) *Runtime {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{cfg: cfg, clock: clock, state: StateStarting, metrics: newMetricCounter(), capabilityProvider: capabilityProvider, authorizationProvider: authorizationProvider, done: make(chan struct{})}
	r.breaker.state = circuitClosed
	r.queue = make(chan ShadowWorkflowInput, cfg.QueueCapacity)
	coordinator, err := durableworkflow.Open(store, r.durablePolicy())
	if err != nil {
		t.Fatal(err)
	}
	r.store, r.coordinator, r.accepting, r.state = store, coordinator, true, StateRunning
	r.lastCheckpointAt = clock.Now().UTC()
	workerContext, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.worker(workerContext)
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

func waitForQualification(t *testing.T, runtime *Runtime, condition func(StatusSnapshot) bool) StatusSnapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := runtime.Status()
		if condition(status) {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	return runtime.Status()
}
