// Package calibrationledger durably records redacted, descriptive comparisons
// between historical decisions and the cognitive shadow projection.
//
// Records are append-only NDJSON envelopes protected by a deterministic
// genesis, strict sequence checks, a record digest, and an envelope hash chain.
// This package has no calibration, feedback, decision, authorization, alert,
// command, or action capability.
package calibrationledger
