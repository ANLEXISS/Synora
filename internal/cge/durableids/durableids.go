// Package durableids owns the single redaction boundary for identifiers that
// cross from Core into CGE durable state.
package durableids

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Kind separates equal source values used by different CGE domains.
type Kind string

const (
	KindObservation Kind = "observation"
	KindEntity      Kind = "entity"
	KindDevice      Kind = "device"
	KindClip        Kind = "clip"
	KindTrack       Kind = "track"
	KindActivation  Kind = "activation"
	KindSequence    Kind = "sequence"
)

const (
	formatPrefix = "cgeid-v1:"
	namespace    = "synora.cge.durable-id.v1"
	hexLength    = sha256.Size * 2
)

// Protect returns a deterministic, domain-separated pseudonym suitable for
// CGE durable state. It is pseudonymisation, not encryption: no secret is
// involved and the result is not reversible by this package.
//
// Empty values remain empty. A value already in the protected format is
// returned unchanged, which makes the boundary idempotent.
func Protect(kind Kind, value string) string {
	if value == "" || IsProtected(value) {
		return value
	}
	digest := sha256.Sum256([]byte(namespace + "\x00" + string(kind) + "\x00" + value))
	return formatPrefix + string(kind) + ":" + hex.EncodeToString(digest[:])
}

// IsProtected reports whether value is a complete v1 CGE durable identifier.
func IsProtected(value string) bool {
	if !strings.HasPrefix(value, formatPrefix) {
		return false
	}
	rest := strings.TrimPrefix(value, formatPrefix)
	separator := strings.LastIndexByte(rest, ':')
	if separator <= 0 || separator == len(rest)-1 {
		return false
	}
	if !validKind(Kind(rest[:separator])) || len(rest[separator+1:]) != hexLength {
		return false
	}
	decoded, err := hex.DecodeString(rest[separator+1:])
	return err == nil && len(decoded) == sha256.Size
}

func validKind(kind Kind) bool {
	switch kind {
	case KindObservation, KindEntity, KindDevice, KindClip, KindTrack, KindActivation, KindSequence:
		return true
	default:
		return false
	}
}
