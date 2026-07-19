// Package replay reconstructs the in-memory routine registry from routine
// records in the global CGE journal. Chain generations are intentionally not
// part of this package: routines always replay the complete global journal.
package replay
