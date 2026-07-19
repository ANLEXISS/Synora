// Package generations manages immutable registry snapshot generations and the
// atomic manifest that names the active generation.
//
// The store layout is root/snapshots/ plus root/manifest.json. A generation
// file is named snapshot-%020d-%016s.json: the decimal part is the journal
// sequence included by the snapshot and the hexadecimal part is the first 16
// lowercase hexadecimal characters of the snapshot payload SHA-256. The
// complete payload digest, byte size, schema, timestamp and chain count are
// retained in Generation and in the checkpoint payload.
//
// A generation is written first, then a durable snapshot.checkpointed journal
// record must be supplied to PublishManifest. The store never appends that
// record itself. This keeps the ordering boundary in durable.Coordinator:
// snapshot and sync, journal checkpoint and sync, then manifest replacement.
// A written PendingGeneration is not active; it becomes checkpointed only
// after Finalize, and active only after PublishManifest. Old generations and
// orphaned snapshots are retained; no cleanup, rotation, repair, compaction,
// truncation, scheduler, or background activity is performed.
//
// If a process stops after the snapshot, the old manifest and journal remain
// usable. If it stops after the checkpoint, the old manifest remains usable and
// replay consumes the checkpoint and later records. If it stops after the
// atomic manifest replacement, the new generation is recoverable. Manifest
// loading validates the snapshot; FromGenerationManifest additionally checks
// the exact checkpoint in the complete journal. FromJournal remains the
// explicit journal-only recovery path.
package generations
