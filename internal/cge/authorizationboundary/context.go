package authorizationboundary

import (
	"sort"
	"strings"
	"time"
)

const maxAuthorizationText = 256

func ValidateAuthorizationContext(context AuthorizationContext) error {
	if context.ID == "" || context.DomainID == "" || !validPurpose(context.PurposeCode) || len(context.ID) > maxAuthorizationText || len(context.DomainID) > maxAuthorizationText || len(context.RequestActorClass) > maxAuthorizationText || len(context.RequestOrigin) > maxAuthorizationText {
		return ErrInvalidAuthorizationContext
	}
	if context.RequestedAt.IsZero() || context.ValidUntil.IsZero() || context.ValidUntil.Before(context.RequestedAt) {
		return ErrInvalidAuthorizationContext
	}
	if !validScopes(context.RequestedScope) {
		return ErrInvalidScope
	}
	if context.Fingerprint == "" || AuthorizationContextFingerprint(context) != context.Fingerprint {
		return ErrFingerprintMismatch
	}
	return nil
}

func (c AuthorizationContext) Clone() AuthorizationContext {
	out := c
	out.RequestedScope = append([]AuthorizationScope(nil), c.RequestedScope...)
	return out
}

func (s AuthorizationScope) Clone() AuthorizationScope { return s }

func validScopes(values []AuthorizationScope) bool {
	seen := map[AuthorizationScope]struct{}{}
	for _, value := range values {
		if value.Kind == "" || value.Ref == "" || len(value.Kind) > maxAuthorizationText || len(value.Ref) > maxAuthorizationText || forbiddenAuthorizationText(value.Kind) || forbiddenAuthorizationText(value.Ref) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func scopesContain(values []AuthorizationScope, wanted AuthorizationScope) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func allScopesContained(values, required []AuthorizationScope) bool {
	for _, wanted := range required {
		if !scopesContain(values, wanted) {
			return false
		}
	}
	return true
}

func anyScopeContained(values, excluded []AuthorizationScope) bool {
	for _, value := range excluded {
		if scopesContain(values, value) {
			return true
		}
	}
	return false
}

func canonicalScopes(values []AuthorizationScope) []AuthorizationScope {
	out := append([]AuthorizationScope(nil), values...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

func canonicalStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func canonicalPurposes(values []AuthorizationPurposeCode) []AuthorizationPurposeCode {
	out := append([]AuthorizationPurposeCode(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func canonicalKinds(values []string) []string { return canonicalStrings(values) }

func uniqueSortedStrings(values []string) []string {
	out := canonicalStrings(values)
	result := out[:0]
	for _, value := range out {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func sortedUniqueStrings(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" || len(value) > maxAuthorizationText || forbiddenAuthorizationText(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func forbiddenAuthorizationText(value string) bool {
	lowered := strings.ToLower(value)
	for _, term := range []string{"execution_token", "command_payload", "emergency_override", "force_allow", "automatic_consent", "execute", "invoke", "dispatch", "reserve", "sensor_command", "action_command"} {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}

func timePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
