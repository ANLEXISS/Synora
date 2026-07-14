package main

import (
	"fmt"
	"strings"

	"synora/internal/engine"
	"synora/pkg/contract"
)

func (a *coreApp) applyActionPolicy(event *contract.Event, result *engine.Result) {
	if a == nil || a.policy == nil || result == nil || result.Decision == nil || result.DangerAssessment == nil {
		return
	}
	decision := result.Decision
	assessment := result.DangerAssessment
	decision.RecommendedActionsFromCGE = nil
	for _, item := range assessment.RecommendedSystemActions {
		if item.Type != "" {
			decision.RecommendedActionsFromCGE = append(decision.RecommendedActionsFromCGE, item.Type)
		}
	}
	policyActions := a.policy.Evaluate(contract.DangerLevel(assessment.RiskLevel), event, decision, a.state.SystemState().Security)
	decision.PolicyActions = policyActions
	decision.RecommendedActionsFromPolicy = nil
	decision.FinalActionPlan = nil
	decision.ActionDecisionReason = fmt.Sprintf("CGE danger %s evaluated, then action policy %s was applied", assessment.RiskLevel, assessment.RiskLevel)
	for _, action := range policyActions {
		if action.Command != "" {
			decision.RecommendedActionsFromPolicy = append(decision.RecommendedActionsFromPolicy, action.Command)
		}
		if action.Blocked {
			decision.BlockedActions = appendUniqueString(decision.BlockedActions, action.Command+":"+action.BlockedReason)
			continue
		}
		decision.FinalActionPlan = append(decision.FinalActionPlan, contract.ActionPlanItem{ID: action.ID, Command: action.Command, Target: action.Target, Source: "policy", Priority: action.Priority, Reason: action.Reason})
	}
	if len(decision.FinalActionPlan) > 0 {
		decision.ActionDecision = "recommended"
	}
}

func hasPolicyPlan(decision *contract.Decision) bool {
	return decision != nil && len(decision.FinalActionPlan) > 0
}

func appendAutomationPlan(decision *contract.Decision, requests []contract.ActionRequest) {
	if decision == nil {
		return
	}
	for _, request := range requests {
		command := strings.TrimSpace(request.Type)
		if command == "" {
			command = strings.TrimSpace(request.Action.Type)
		}
		if command == "" {
			continue
		}
		decision.FinalActionPlan = append(decision.FinalActionPlan, contract.ActionPlanItem{ID: request.ActionID, Command: command, Target: request.Target, Source: "automation", Priority: 0, Reason: "matching automation"})
	}
	if len(requests) > 0 {
		decision.ActionDecision = "requested"
	}
}
