package cge

import "context"

// AuthorityMode returns the configured governed-decision publication mode.
func (e *ShadowEngine) AuthorityMode() AuthorityMode {
	if e == nil || e.authority == nil {
		return AuthorityModeShadow
	}
	return e.authority.Mode()
}

// PublishDecision is the Core↔CGE decision boundary. It only persists a
// descriptive record in shadow/advisory mode. Authoritative mode remains
// fail-closed until an explicit execution planner is supplied.
func (e *ShadowEngine) PublishDecision(ctx context.Context, decision DecisionEnvelope, snapshot OperationalSnapshot) (DecisionPublication, error) {
	if e == nil || e.authority == nil {
		return DecisionPublication{Status: DecisionPublicationDenied}, ErrInvalidAuthorityMode
	}
	return e.authority.PublishDecision(ctx, decision, snapshot)
}

// Decisions returns defensive descriptive decision records for diagnostics.
func (e *ShadowEngine) Decisions(ctx context.Context) ([]DecisionRecord, error) {
	if e == nil || e.authority == nil {
		return nil, ErrDecisionStore
	}
	return e.authority.Decisions(ctx)
}
