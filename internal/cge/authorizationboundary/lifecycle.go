package authorizationboundary

type LifecycleDecision struct {
	Allowed    bool
	ReasonCode string
}

func EvaluateAuthorizationLifecycle(previous, next AuthorizationAssessmentStatus) LifecycleDecision {
	if previous == "" || next == "" || previous == next || previous == AssessmentObsolete || previous == AssessmentInvalidated {
		return LifecycleDecision{ReasonCode: "authorization.transition_not_allowed"}
	}
	if next == AssessmentObsolete || next == AssessmentInvalidated || next == AssessmentEligible || next == AssessmentDenied || next == AssessmentConfirmationRequired || next == AssessmentDeferred || next == AssessmentActive {
		return LifecycleDecision{Allowed: true, ReasonCode: "authorization.transition_allowed"}
	}
	return LifecycleDecision{ReasonCode: "authorization.transition_not_allowed"}
}
