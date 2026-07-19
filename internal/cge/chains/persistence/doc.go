// Package persistence provides explicit, local, versioned persistence for a
// complete CGE chain registry snapshot.
//
// It is deliberately separate from chains and registry: the registry remains
// an in-memory owner, while this package serializes defensive snapshots and
// restores a new owner only when explicitly requested. The format contains
// detached observation references and local audit history, never raw event
// payloads. There is no automatic save/load, rotation, backup, scheduler,
// append-only global journal, or runtime integration.
package persistence
