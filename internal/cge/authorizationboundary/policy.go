package authorizationboundary

type Policy struct {
	MaxCandidates  int
	MaxRules       int
	MaxGrants      int
	MaxScopes      int
	MaxReasonCodes int

	MinPolicyCoveragePermille  int
	MinGrantCoveragePermille   int
	MinEligibilityPermille     int
	MinPreferredMarginPermille int

	RequireExplicitAllowRule       bool
	DenyOverridesAllow             bool
	UnknownMeansDenied             bool
	RequireValidGrantWhenRequested bool

	PreserveDeniedCandidates bool
	PreserveRejectedGrants   bool
}

func DefaultPolicy() Policy {
	return Policy{MaxCandidates: 32, MaxRules: 256, MaxGrants: 256, MaxScopes: 64, MaxReasonCodes: 64, MinPolicyCoveragePermille: 1000, MinGrantCoveragePermille: 1000, MinEligibilityPermille: 1000, MinPreferredMarginPermille: 75, RequireExplicitAllowRule: true, DenyOverridesAllow: true, UnknownMeansDenied: true, RequireValidGrantWhenRequested: true, PreserveDeniedCandidates: true, PreserveRejectedGrants: true}
}

func (p Policy) Validate() error {
	if p.MaxCandidates <= 0 || p.MaxRules <= 0 || p.MaxGrants <= 0 || p.MaxScopes <= 0 || p.MaxReasonCodes <= 0 || p.MinPolicyCoveragePermille < 0 || p.MinPolicyCoveragePermille > 1000 || p.MinGrantCoveragePermille < 0 || p.MinGrantCoveragePermille > 1000 || p.MinEligibilityPermille < 0 || p.MinEligibilityPermille > 1000 || p.MinPreferredMarginPermille < 0 || p.MinPreferredMarginPermille > 1000 || !p.RequireExplicitAllowRule || !p.DenyOverridesAllow || !p.UnknownMeansDenied || !p.RequireValidGrantWhenRequested {
		return ErrInvalidPolicy
	}
	return nil
}

func DefaultPolicySet() AuthorizationPolicySet {
	set := AuthorizationPolicySet{ID: "authorization-policy-set-default", Version: "authorization-policy-v1", DefaultEffect: EffectDeny, Revision: 1}
	set.Fingerprint = AuthorizationPolicySetFingerprint(set)
	return set
}

func (p Policy) Fingerprint() string { return policyFingerprint(p) }

func ValidatePolicySet(set AuthorizationPolicySet) error {
	if set.ID == "" || set.Version == "" || len(set.ID) > maxAuthorizationText || len(set.Version) > maxAuthorizationText || set.DefaultEffect != EffectDeny || set.Revision == 0 || set.Fingerprint == "" || policySetFingerprint(set) != set.Fingerprint {
		return ErrInvalidPolicySet
	}
	if len(set.Rules) > 256 {
		return ErrRuleLimitReached
	}
	seen := map[string]struct{}{}
	for _, rule := range set.Rules {
		if _, ok := seen[rule.ID]; ok {
			return ErrInvalidRule
		}
		seen[rule.ID] = struct{}{}
		if err := ValidatePolicyRule(rule); err != nil {
			return err
		}
	}
	return nil
}

func (s AuthorizationPolicySet) Clone() AuthorizationPolicySet {
	out := s
	out.Rules = make([]AuthorizationRule, len(s.Rules))
	for i, rule := range s.Rules {
		out.Rules[i] = rule.Clone()
	}
	return out
}

func sortedPolicyRules(values []AuthorizationRule) []AuthorizationRule {
	out := make([]AuthorizationRule, len(values))
	for i, value := range values {
		out[i] = value.Clone()
	}
	sortRules(out)
	return out
}
