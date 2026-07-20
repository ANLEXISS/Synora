package capabilitymapping

import (
	"sort"
	"strings"

	"synora/internal/cge/advisoryrequests"
)

func ValidateInventory(inventory CapabilityInventory, catalog CapabilityCatalog) error {
	if err := ValidateCatalog(catalog); err != nil {
		return err
	}
	if inventory.ID == "" || inventory.DomainID == "" || inventory.Revision == 0 || forbiddenCapabilityText(string(inventory.ID)) || forbiddenCapabilityText(inventory.DomainID) {
		return ErrInvalidInventory
	}
	if inventory.CatalogFingerprint == "" || inventory.CatalogFingerprint != catalog.Fingerprint {
		return ErrInventoryCatalogMismatch
	}
	if inventory.Fingerprint == "" || inventoryFingerprint(inventory) != inventory.Fingerprint {
		return ErrFingerprintMismatch
	}
	seen := map[CapabilityInstanceID]struct{}{}
	for i := 1; i < len(inventory.Instances); i++ {
		if inventory.Instances[i-1].ID >= inventory.Instances[i].ID {
			return ErrInvalidInventory
		}
	}
	for _, instance := range inventory.Instances {
		if _, ok := seen[instance.ID]; ok {
			return ErrCapabilityIDCollision
		}
		seen[instance.ID] = struct{}{}
		if err := validateInstance(instance, catalog); err != nil {
			return err
		}
	}
	return nil
}

func validateInstance(instance CapabilityInstance, catalog CapabilityCatalog) error {
	if instance.ID == "" || instance.Kind == "" || instance.DomainID == "" || instance.ProviderID == "" || instance.Revision == 0 {
		return ErrInvalidCapabilityInstance
	}
	if forbiddenCapabilityText(string(instance.ID)) || forbiddenCapabilityText(instance.DomainID) || forbiddenCapabilityText(instance.ProviderID) {
		return ErrInvalidCapabilityInstance
	}
	definition, ok := definitionFor(catalog, instance.Kind)
	if !ok {
		return ErrUnknownCapabilityKind
	}
	if instance.DefinitionFingerprint == "" || instance.DefinitionFingerprint != definitionFingerprint(definition) {
		return ErrFingerprintMismatch
	}
	if !validCapabilityStatus(instance.Status) {
		return ErrInvalidCapabilityStatus
	}
	if err := validateQuality(instance.Quality); err != nil {
		return err
	}
	if !validClass(string(instance.CostClass), classCost) || !validClass(string(instance.LatencyClass), classLatency) || !validClass(string(instance.SensitivityClass), classSensitivity) {
		return ErrInvalidCapabilityInstance
	}
	if !validScopes(instance.SupportedScopes) {
		return ErrInvalidCapabilityScope
	}
	if !validConstraints(instance.Constraints) {
		return ErrInvalidCapabilityConstraint
	}
	if instance.Fingerprint == "" || instanceFingerprint(instance) != instance.Fingerprint {
		return ErrFingerprintMismatch
	}
	return nil
}

func validateQuality(q CapabilityQuality) error {
	for _, value := range []int{q.ReliabilityPermille, q.CompletenessPermille, q.FreshnessPermille} {
		if value < 0 || value > 1000 {
			return ErrInvalidCapabilityQuality
		}
	}
	if q.SourceCount < 0 {
		return ErrInvalidCapabilityQuality
	}
	return nil
}

func validScopes(values []CapabilityScope) bool {
	copy := append([]CapabilityScope(nil), values...)
	sortScopes(copy)
	for i, scope := range copy {
		if scope.Kind == "" || scope.Ref == "" || len(scope.Kind) > 64 || len(scope.Ref) > 128 || forbiddenCapabilityText(scope.Kind) || forbiddenCapabilityText(scope.Ref) {
			return false
		}
		if i > 0 && copy[i-1] == scope {
			return false
		}
	}
	return true
}

func validConstraints(values []CapabilityConstraint) bool {
	copy := make([]CapabilityConstraint, len(values))
	for i, value := range values {
		copy[i] = value.Clone()
	}
	sortConstraints(copy)
	for i, constraint := range copy {
		if constraint.Code == "" || len(constraint.Code) > 128 || forbiddenCapabilityText(constraint.Code) || !validConstraintOperator(constraint.Operator) {
			return false
		}
		if constraint.Value.String != "" && forbiddenCapabilityText(constraint.Value.String) {
			return false
		}
		if constraint.Value.Bool == nil && constraint.Value.String == "" && (constraint.Operator == ConstraintEquals || constraint.Operator == ConstraintNotEquals || constraint.Operator == ConstraintContains || constraint.Operator == ConstraintMinimum || constraint.Operator == ConstraintMaximum) {
			return false
		}
		if constraint.Value.NumberPermille < 0 || constraint.Value.NumberPermille > 1000 {
			return false
		}
		if i > 0 && sameConstraint(copy[i-1], constraint) {
			return false
		}
	}
	return true
}

func sameConstraint(a, b CapabilityConstraint) bool {
	return a.Code == b.Code && a.Operator == b.Operator && a.Hard == b.Hard && a.Value.String == b.Value.String && a.Value.NumberPermille == b.Value.NumberPermille && boolValue(a.Value.Bool) == boolValue(b.Value.Bool)
}
func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
func validConstraintOperator(value ConstraintOperator) bool {
	return value == ConstraintEquals || value == ConstraintNotEquals || value == ConstraintContains || value == ConstraintMinimum || value == ConstraintMaximum || value == ConstraintPresent || value == ConstraintAbsent
}
func validCapabilityStatus(value CapabilityStatus) bool {
	return value == CapabilityStatusAvailable || value == CapabilityStatusDegraded || value == CapabilityStatusUnavailable || value == CapabilityStatusUnknown || value == CapabilityStatusRetired || value == CapabilityStatusInvalidated
}

func definitionFor(catalog CapabilityCatalog, kind CapabilityKind) (CapabilityDefinition, bool) {
	for _, definition := range catalog.Definitions {
		if definition.Kind == kind {
			return definition.Clone(), true
		}
	}
	return CapabilityDefinition{}, false
}

func validateRequest(request advisoryrequests.AdvisoryEvidenceRequest, policy Policy) error {
	if request.ID == "" || request.Key == "" || request.EpisodeID == "" || request.CandidateID == "" || request.Generation == 0 || request.SourceAssessmentFingerprint == "" || request.SourceCandidateFingerprint == "" {
		return ErrInvalidRequest
	}
	if request.Status == advisoryrequests.StatusSatisfied || request.Status == advisoryrequests.StatusExpired || request.Status == advisoryrequests.StatusCancelled || request.Status == advisoryrequests.StatusInvalidated {
		return ErrRequestTerminal
	}
	if request.Status != advisoryrequests.StatusProposed && request.Status != advisoryrequests.StatusAcknowledged && request.Status != advisoryrequests.StatusDeferred && request.Status != advisoryrequests.StatusSuppressed {
		return ErrInvalidRequest
	}
	if request.Status == advisoryrequests.StatusSuppressed && !policy.AnalyzeSuppressedRequests {
		return ErrInvalidRequest
	}
	if request.Kind == "" || request.Dimension == "" || request.Fingerprint == "" || advisoryrequests.AdvisoryRequestFingerprint(request) != request.Fingerprint {
		return ErrFingerprintMismatch
	}
	if request.Key != advisoryrequests.AdvisoryRequestKeyFor(request) || request.ID != advisoryrequests.AdvisoryRequestIDFor(request.Key, request.Generation) {
		return ErrInvalidRequest
	}
	if request.Flags.NotACommand != true || request.Flags.NotAProbability != true || request.Flags.NoSecurityMeaning != true || request.Flags.RequiresExternalMapping != true || request.Flags.RequiresExternalAuthorization != true {
		return ErrInvalidRequest
	}
	if requestKindToCapabilityKind[string(request.Kind)] == "" || requestKindToDimension[string(request.Kind)] != string(request.Dimension) {
		return ErrInvalidRequest
	}
	return nil
}

func normalizeInventory(inventory CapabilityInventory) CapabilityInventory {
	out := inventory.Clone()
	sort.Slice(out.Instances, func(i, j int) bool { return out.Instances[i].ID < out.Instances[j].ID })
	for i := range out.Instances {
		sortScopes(out.Instances[i].SupportedScopes)
		sortConstraints(out.Instances[i].Constraints)
	}
	return out
}

func normalizeString(value string) string { return strings.TrimSpace(value) }
