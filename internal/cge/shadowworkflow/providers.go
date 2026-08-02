package shadowworkflow

import (
	"context"

	"synora/internal/cge/advisoryrequests"
	"synora/internal/cge/authorizationboundary"
	"synora/internal/cge/capabilitymapping"
)

type CapabilityInputProvider interface {
	SnapshotFor(context.Context, advisoryrequests.AdvisoryEvidenceRequest) (capabilitymapping.CapabilityCatalog, capabilitymapping.CapabilityInventory, bool, error)
}

type AuthorizationInputProvider interface {
	InputsFor(context.Context, capabilitymapping.CapabilityMappingAssessment) (authorizationboundary.AuthorizationContext, authorizationboundary.AuthorizationPolicySet, authorizationboundary.ExternalGrantSnapshot, bool, error)
}
