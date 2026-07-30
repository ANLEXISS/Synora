package cge

import "context"

// DecisionPublicationSink transports only a descriptive envelope. It is not
// an execution or action-dispatch interface.
type DecisionPublicationSink interface {
	PublishDecision(context.Context, DecisionEnvelope) error
}

// AuthorityMode returns the configured governed-decision publication mode.
func (e *ShadowEngine) AuthorityMode() AuthorityMode {
	if e == nil || e.authority == nil {
		return AuthorityModeShadow
	}
	return e.authority.Mode()
}

func (e *ShadowEngine) ExecutionMode() CGEExecutionMode {
	if e == nil || e.authority == nil {
		return CGEExecutionDisabled
	}
	return e.authority.executionMode
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

// ExecutionPlans exposes only validated, detached dry-run diagnostics.
func (e *ShadowEngine) ExecutionPlans(ctx context.Context) ([]ExecutionPlan, error) {
	if e == nil || e.authority == nil {
		return nil, ErrExecutionPlanStore
	}
	return e.authority.ExecutionPlans(ctx)
}

func (e *ShadowEngine) ExecutionPlanComparisons(ctx context.Context) ([]ExecutionPlanComparison, error) {
	if e == nil || e.authority == nil {
		return nil, ErrExecutionPlanStore
	}
	return e.authority.ExecutionPlanComparisons(ctx)
}

// RecordActionResult forwards only the closed CGE feedback contract to the
// authority boundary. Legacy action-result payloads are not reinterpreted as
// CGE execution evidence.
func (e *ShadowEngine) RecordActionResult(ctx context.Context, result ActionResult) error {
	if e == nil || e.authority == nil {
		return ErrDecisionStore
	}
	return e.authority.RecordActionResult(ctx, result)
}
