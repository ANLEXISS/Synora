// Package durableworkflow provides an isolated, in-memory coordinated state
// and an optional file-backed append-only journal for the experimental
// cognitive layers. It validates cross-layer lineage, derives freshness, and
// replays complete workflow transactions. It does not invoke capabilities,
// issue authority, or integrate with the production runtime.
package durableworkflow
