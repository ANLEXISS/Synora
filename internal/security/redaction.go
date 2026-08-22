package security

import "regexp"

var (
	supportSecretPattern = regexp.MustCompile(`(?i)(["']?(?:api[_-]?token|token|password|secret|private[_-]?key|authorization|cookie|biometric|face[_-]?profile)["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}\]]+)`)
	supportPathPattern   = regexp.MustCompile(`(?:/(?:etc|var|home|tmp|run|opt|root)/[A-Za-z0-9._~+/:-]+)`)
)

// RedactSupportText is the final boundary for operator-provided logs and
// support excerpts. It is intentionally conservative about what it removes:
// known credential/biometric fields and host filesystem paths.
func RedactSupportText(value string) string {
	value = supportSecretPattern.ReplaceAllString(value, `${1}<redacted>`)
	return supportPathPattern.ReplaceAllString(value, `<path-redacted>`)
}
