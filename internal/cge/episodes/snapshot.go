package episodes

import (
	"fmt"
	"sort"
)

type Snapshot struct {
	Revision          uint64
	Episodes          []EpisodeSnapshot
	EventIndex        map[string]EpisodeID
	PolicyFingerprint string
}

func (s Snapshot) Clone() Snapshot {
	out := s
	out.Episodes = make([]EpisodeSnapshot, len(s.Episodes))
	for i, episode := range s.Episodes {
		out.Episodes[i] = episode.Clone()
	}
	out.EventIndex = make(map[string]EpisodeID, len(s.EventIndex))
	for eventID, episodeID := range s.EventIndex {
		out.EventIndex[eventID] = episodeID
	}
	return out
}

func (s Snapshot) Validate() error {
	seenEpisodes := make(map[EpisodeID]struct{}, len(s.Episodes))
	seenEvents := make(map[string]EpisodeID, len(s.EventIndex))
	lastID := EpisodeID("")
	for _, episode := range s.Episodes {
		if err := episode.Validate(); err != nil {
			return fmt.Errorf("%w: episode %s: %v", ErrInvalidSnapshot, episode.ID, err)
		}
		if _, ok := seenEpisodes[episode.ID]; ok || lastID != "" && lastID >= episode.ID {
			return fmt.Errorf("%w: episode order", ErrInvalidSnapshot)
		}
		seenEpisodes[episode.ID] = struct{}{}
		lastID = episode.ID
		for _, observation := range episode.Observations {
			if old, ok := seenEvents[observation.EventID]; ok && old != episode.ID {
				return fmt.Errorf("%w: event belongs to two episodes", ErrInvalidSnapshot)
			}
			seenEvents[observation.EventID] = episode.ID
		}
	}
	if len(seenEvents) != len(s.EventIndex) {
		return fmt.Errorf("%w: event index cardinality", ErrInvalidSnapshot)
	}
	for eventID, episodeID := range s.EventIndex {
		if seenEvents[eventID] != episodeID {
			return fmt.Errorf("%w: event index entry", ErrInvalidSnapshot)
		}
	}
	return nil
}

func sortedEpisodes(values []EpisodeSnapshot) []EpisodeSnapshot {
	out := make([]EpisodeSnapshot, len(values))
	for i, value := range values {
		out[i] = value.Clone()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
