package authorizationboundary

import (
	"sort"

	"synora/internal/cge/capabilitymapping"
)

func ValidateGrant(grant ExternalGrant) error {
	if grant.ID == "" || !validGrantKind(grant.Kind) || grant.DomainID == "" || grant.IssuerID == "" || len(grant.ID) > maxAuthorizationText || forbiddenAuthorizationText(string(grant.ID)) || forbiddenAuthorizationText(grant.IssuerID) || len(grant.SubjectClass) > maxAuthorizationText || grant.ValidFrom.IsZero() || grant.ValidUntil.IsZero() || !grant.ValidUntil.After(grant.ValidFrom) || grant.Revision == 0 || grant.Fingerprint == "" || ExternalGrantFingerprint(grant) != grant.Fingerprint {
		return ErrInvalidGrant
	}
	if !sortedUniquePurposes(grant.PurposeCodes) || !sortedUniqueCapabilityKinds(grant.CapabilityKinds) || !validScopes(grant.Scopes) {
		return ErrInvalidGrant
	}
	for _, kind := range grant.CapabilityKinds {
		found := false
		for _, supported := range capabilitymapping.AllCapabilityKinds() {
			if kind == supported {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalidGrant
		}
	}
	return nil
}

func (g ExternalGrant) Clone() ExternalGrant {
	out := g
	out.PurposeCodes = append([]AuthorizationPurposeCode(nil), g.PurposeCodes...)
	out.CapabilityKinds = append([]capabilitymapping.CapabilityKind(nil), g.CapabilityKinds...)
	out.Scopes = append([]AuthorizationScope(nil), g.Scopes...)
	out.RevokedAt = timePointer(g.RevokedAt)
	return out
}

func ValidateGrantSnapshot(snapshot ExternalGrantSnapshot) error {
	if snapshot.Revision == 0 || snapshot.Fingerprint == "" || grantSnapshotFingerprint(snapshot) != snapshot.Fingerprint || len(snapshot.Grants) > 256 {
		return ErrInvalidGrantSnapshot
	}
	seen := map[ExternalGrantID]struct{}{}
	for i, grant := range snapshot.Grants {
		if _, ok := seen[grant.ID]; ok {
			return ErrInvalidGrantSnapshot
		}
		seen[grant.ID] = struct{}{}
		if err := ValidateGrant(grant); err != nil {
			return err
		}
		if snapshot.Index != nil && snapshot.Index[grant.ID] != i {
			return ErrInvalidGrantSnapshot
		}
	}
	return nil
}

func (s ExternalGrantSnapshot) Clone() ExternalGrantSnapshot {
	out := s
	out.Grants = make([]ExternalGrant, len(s.Grants))
	for i, grant := range s.Grants {
		out.Grants[i] = grant.Clone()
	}
	out.Index = make(map[ExternalGrantID]int, len(s.Index))
	for key, value := range s.Index {
		out.Index[key] = value
	}
	return out
}

type grantState string

const (
	grantValid              grantState = "grant.valid"
	grantMissing            grantState = "grant.missing"
	grantExpired            grantState = "grant.expired"
	grantNotYetValid        grantState = "grant.not_yet_valid"
	grantRevoked            grantState = "grant.revoked"
	grantPurposeMismatch    grantState = "grant.purpose_mismatch"
	grantCapabilityMismatch grantState = "grant.capability_mismatch"
	grantScopeMismatch      grantState = "grant.scope_mismatch"
	grantDomainMismatch     grantState = "grant.domain_mismatch"
	grantSubjectMismatch    grantState = "grant.subject_mismatch"
	grantInvalid            grantState = "grant.invalid"
)

func grantMatches(grant ExternalGrant, context AuthorizationContext, candidate capabilitymapping.CapabilityMappingCandidate, purpose AuthorizationPurposeCode, requiredScopes []AuthorizationScope) grantState {
	if grant.Revoked {
		return grantRevoked
	}
	if !context.RequestedAt.Before(grant.ValidUntil) {
		return grantExpired
	}
	if context.RequestedAt.Before(grant.ValidFrom) {
		return grantNotYetValid
	}
	if grant.DomainID != context.DomainID {
		return grantDomainMismatch
	}
	if grant.SubjectClass != "" && grant.SubjectClass != context.RequestActorClass {
		return grantSubjectMismatch
	}
	if len(grant.PurposeCodes) > 0 && !containsPurpose(grant.PurposeCodes, purpose) {
		return grantPurposeMismatch
	}
	if len(grant.CapabilityKinds) > 0 && !containsKind(grant.CapabilityKinds, candidate.CapabilityKind) {
		return grantCapabilityMismatch
	}
	if !allScopesContained(grant.Scopes, requiredScopes) {
		return grantScopeMismatch
	}
	return grantValid
}

func sortGrants(values []ExternalGrant) {
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
}
