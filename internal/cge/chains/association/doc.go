// Package association plans explicit, deterministic observation-to-chain
// associations from defensive chain snapshots.
//
// Planning is pure: it does not own a registry, append a journal, create a
// chain, or resolve an ambiguity. ObservationRef already contains the
// identity, sequence, activation, track, device, and node keys used here;
// SituationKind is the only additional optional association context. Durable
// application is deliberately kept in the durable package.
// The default policy uses integer weights, explicit temporal windows, and a
// minimum score margin. A single score fact cannot cross the attach threshold;
// equal or insufficient candidates produce an ambiguous or create-candidate
// plan. Candidate IDs are SHA-256-derived from the policy version and
// observation ID, with no raw observation data embedded.
package association
