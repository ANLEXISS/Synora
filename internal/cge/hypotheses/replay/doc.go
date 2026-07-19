// Package replay reconstructs the in-memory hypothesis registry from the
// shared cognitive journal. It deliberately ignores chain-domain records and
// never writes journal or snapshot state. A hypothesis.superseded record is
// applied as one registry transaction: the predecessor becomes terminal and
// its open successor is published together, preserving lineage determinism.
package replay
