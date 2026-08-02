package situationfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"synora/internal/cge/episodes"
)

type Snapshot struct {
	Revision uint64

	FactSets     []FactSet
	EpisodeIndex map[episodes.EpisodeID]int

	SchemaFingerprint string
	PolicyFingerprint string
	Digest            string
}

func (s Snapshot) Clone() Snapshot {
	out := s
	out.FactSets = make([]FactSet, len(s.FactSets))
	for i, set := range s.FactSets {
		out.FactSets[i] = set.Clone()
	}
	out.EpisodeIndex = make(map[episodes.EpisodeID]int, len(s.EpisodeIndex))
	for id, index := range s.EpisodeIndex {
		out.EpisodeIndex[id] = index
	}
	return out
}

func (s Snapshot) Validate(schema FactSchema, policy Policy) error {
	if err := schema.Validate(); err != nil || policy.Validate() != nil {
		return ErrInvalidFactSet
	}
	last := episodes.EpisodeID("")
	seen := map[episodes.EpisodeID]struct{}{}
	for i, set := range s.FactSets {
		if err := set.Validate(schema, policy); err != nil {
			return err
		}
		if i > 0 && set.EpisodeID <= last {
			return ErrInvalidFactSet
		}
		if _, ok := seen[set.EpisodeID]; ok {
			return ErrInvalidFactSet
		}
		seen[set.EpisodeID] = struct{}{}
		last = set.EpisodeID
		if index, ok := s.EpisodeIndex[set.EpisodeID]; !ok || index != i {
			return ErrInvalidFactSet
		}
	}
	if len(seen) != len(s.EpisodeIndex) {
		return ErrInvalidFactSet
	}
	if s.Digest != "" && s.Digest != RegistryDigest(s) {
		return ErrFingerprintMismatch
	}
	return nil
}

func RegistryDigest(snapshot Snapshot) string {
	copy := snapshot
	copy.Digest = ""
	copy.FactSets = append([]FactSet(nil), snapshot.FactSets...)
	if !factSetsSorted(copy.FactSets) {
		sort.Slice(copy.FactSets, func(i, j int) bool { return copy.FactSets[i].EpisodeID < copy.FactSets[j].EpisodeID })
	}
	copy.EpisodeIndex = map[episodes.EpisodeID]int{}
	for i, set := range copy.FactSets {
		copy.EpisodeIndex[set.EpisodeID] = i
	}
	return digestJSON("situation-facts-registry-v1:", copy)
}

func digestJSON(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	digest := sha256.Sum256(payload)
	return prefix + hex.EncodeToString(digest[:])
}
