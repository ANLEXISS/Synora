package episodes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func EpisodeFingerprint(episode Episode) string {
	copy := episode.Clone()
	copy.ClosedAt = cloneTimePtr(copy.ClosedAt)
	payload, _ := json.Marshal(copy)
	digest := sha256.Sum256(payload)
	return "episode-v1:sha256:" + hex.EncodeToString(digest[:])
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func RegistryDigest(snapshot Snapshot) string {
	canonical := snapshot.Clone()
	canonical.Episodes = sortedEpisodes(canonical.Episodes)
	type eventEntry struct {
		EventID   string
		EpisodeID EpisodeID
	}
	entries := make([]eventEntry, 0, len(canonical.EventIndex))
	for eventID, episodeID := range canonical.EventIndex {
		entries = append(entries, eventEntry{eventID, episodeID})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].EventID < entries[j].EventID })
	payload, _ := json.Marshal(struct {
		Revision          uint64
		PolicyFingerprint string
		Episodes          []EpisodeSnapshot
		EventIndex        []eventEntry
	}{canonical.Revision, canonical.PolicyFingerprint, canonical.Episodes, entries})
	digest := sha256.Sum256(payload)
	return "episode-registry-v1:sha256:" + hex.EncodeToString(digest[:])
}

func ValidateDigest(snapshot Snapshot, expected string) error {
	if RegistryDigest(snapshot) != expected {
		return fmt.Errorf("%w: digest", ErrInvalidSnapshot)
	}
	return nil
}
