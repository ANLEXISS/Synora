// Package deviation evaluates descriptive behavioral deviation against a
// caller-provided routine baseline. It is intentionally pure: it does not
// access a registry, durable coordinator, journal, ShadowEngine, or clock.
//
// Scores are bounded indices in [0, 1000]. They are not probabilities,
// confidence values, alerts, threat levels, or security decisions.
package deviation
