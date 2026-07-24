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

// ProtectRaw always treats value as a raw Core identifier. It is
// pseudonymisation, not encryption: no secret is involved and the result is
// not reversible by this package.
func ProtectRaw(kind Kind, value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(namespace + "\x00" + string(kind) + "\x00" + value))
	return formatPrefix + string(kind) + ":" + hex.EncodeToString(digest[:])
}

// Protect returns a deterministic, domain-separated pseudonym suitable for
// CGE durable state when value may already be an internal reference. Empty
// values remain empty. A token is retained only when it is valid for the
// requested domain; a token from another domain is pseudonymised again into
// the requested domain.
func Protect(kind Kind, value string) string {
	if value == "" || IsProtectedFor(kind, value) {
		return value
	}
	return ProtectRaw(kind, value)
}

// IsProtected reports whether value is a complete v1 CGE durable identifier.
func IsProtected(value string) bool {
	_, ok := protectedKind(value)
	return ok
}

// IsProtectedFor reports whether value is a complete v1 CGE durable
// identifier in exactly the requested domain.
func IsProtectedFor(kind Kind, value string) bool {
	parsed, ok := protectedKind(value)
	return ok && parsed == kind
}

func protectedKind(value string) (Kind, bool) {
	if !strings.HasPrefix(value, formatPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(value, formatPrefix)
	separator := strings.LastIndexByte(rest, ':')
	if separator <= 0 || separator == len(rest)-1 {
		return "", false
	}
	kind := Kind(rest[:separator])
	if !validKind(kind) || len(rest[separator+1:]) != hexLength {
		return "", false
	}
	decoded, err := hex.DecodeString(rest[separator+1:])
	if err != nil || len(decoded) != sha256.Size {
		return "", false
	}
	return kind, true
}

func validKind(kind Kind) bool {
	switch kind {
	case KindObservation, KindEntity, KindDevice, KindClip, KindTrack, KindActivation, KindSequence:
		return true
	default:
		return false
	}
}
