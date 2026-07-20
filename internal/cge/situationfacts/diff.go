package situationfacts

import (
	"sort"
)

func Diff(before, after FactSet) (FactSetDiff, error) {
	return diffWithValidation(before, after, true)
}

func diffTrusted(before, after FactSet) (FactSetDiff, error) {
	return diffWithValidation(before, after, false)
}

func diffWithValidation(before, after FactSet, verifyFingerprints bool) (FactSetDiff, error) {
	if before.EpisodeID == "" || after.EpisodeID == "" || before.EpisodeID != after.EpisodeID || before.EpisodeRevision > after.EpisodeRevision {
		return FactSetDiff{}, ErrInvalidDiff
	}
	if verifyFingerprints && (before.Fingerprint == "" || after.Fingerprint == "" || FactSetFingerprint(before) != before.Fingerprint || FactSetFingerprint(after) != after.Fingerprint) {
		return FactSetDiff{}, ErrFingerprintMismatch
	}
	if before.Fingerprint == after.Fingerprint && before.Fingerprint != "" {
		return FactSetDiff{EpisodeID: before.EpisodeID, BeforeEpisodeRevision: before.EpisodeRevision, AfterEpisodeRevision: after.EpisodeRevision, BeforeFingerprint: before.Fingerprint, AfterFingerprint: after.Fingerprint}, nil
	}
	if before.EpisodeRevision == after.EpisodeRevision {
		return FactSetDiff{}, ErrInvalidDiff
	}
	result := FactSetDiff{EpisodeID: before.EpisodeID, BeforeEpisodeRevision: before.EpisodeRevision, AfterEpisodeRevision: after.EpisodeRevision, BeforeFingerprint: before.Fingerprint, AfterFingerprint: after.Fingerprint}
	if factsSorted(before.Facts) && factsSorted(after.Facts) && conflictsSorted(before.Conflicts) && conflictsSorted(after.Conflicts) {
		appendSortedFactDiff(&result, before.Facts, after.Facts)
		appendSortedConflictDiff(&result, before.Conflicts, after.Conflicts)
		return result, nil
	}
	left, right := factsByKey(before.Facts), factsByKey(after.Facts)
	keys := make([]FactKey, 0, len(left)+len(right))
	seen := make(map[FactKey]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range right {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, key := range keys {
		beforeFacts, afterFacts := left[key], right[key]
		if len(beforeFacts) == 1 && len(afterFacts) == 1 {
			if beforeFacts[0].ID != afterFacts[0].ID {
				result.Changed = append(result.Changed, FactChange{Key: key, Before: beforeFacts[0].Clone(), After: afterFacts[0].Clone()})
			}
			continue
		}
		ids := make(map[FactID]Fact, len(beforeFacts))
		for _, fact := range beforeFacts {
			ids[fact.ID] = fact
		}
		for _, fact := range afterFacts {
			if _, ok := ids[fact.ID]; ok {
				delete(ids, fact.ID)
			} else {
				result.Added = append(result.Added, fact.Clone())
			}
		}
		for _, fact := range ids {
			result.Removed = append(result.Removed, retractedFact(fact))
		}
	}
	sortFacts(result.Added)
	sortFacts(result.Removed)
	sort.Slice(result.Changed, func(i, j int) bool { return result.Changed[i].Key < result.Changed[j].Key })
	conflictsByID := func(values []ConflictSet) map[string]ConflictSet {
		out := map[string]ConflictSet{}
		for _, value := range values {
			out[value.ID] = value
		}
		return out
	}
	leftConflicts, rightConflicts := conflictsByID(before.Conflicts), conflictsByID(after.Conflicts)
	for id, conflict := range rightConflicts {
		if _, ok := leftConflicts[id]; !ok {
			result.ConflictsAdded = append(result.ConflictsAdded, conflict)
		}
	}
	for id, conflict := range leftConflicts {
		if _, ok := rightConflicts[id]; !ok {
			result.ConflictsRemoved = append(result.ConflictsRemoved, conflict)
		}
	}
	sort.Slice(result.ConflictsAdded, func(i, j int) bool { return result.ConflictsAdded[i].ID < result.ConflictsAdded[j].ID })
	sort.Slice(result.ConflictsRemoved, func(i, j int) bool { return result.ConflictsRemoved[i].ID < result.ConflictsRemoved[j].ID })
	return result, nil
}

func appendSortedFactDiff(result *FactSetDiff, before, after []Fact) {
	for leftIndex, rightIndex := 0, 0; leftIndex < len(before) || rightIndex < len(after); {
		var key FactKey
		if rightIndex == len(after) || leftIndex < len(before) && before[leftIndex].Key < after[rightIndex].Key {
			key = before[leftIndex].Key
		} else if leftIndex == len(before) || after[rightIndex].Key < before[leftIndex].Key {
			key = after[rightIndex].Key
		} else {
			key = before[leftIndex].Key
		}
		leftStart, rightStart := leftIndex, rightIndex
		for leftIndex < len(before) && before[leftIndex].Key == key {
			leftIndex++
		}
		for rightIndex < len(after) && after[rightIndex].Key == key {
			rightIndex++
		}
		leftGroup, rightGroup := before[leftStart:leftIndex], after[rightStart:rightIndex]
		if len(leftGroup) == 1 && len(rightGroup) == 1 {
			if leftGroup[0].ID != rightGroup[0].ID {
				result.Changed = append(result.Changed, FactChange{Key: key, Before: leftGroup[0].Clone(), After: rightGroup[0].Clone()})
			}
			continue
		}
		beforeIDs := make(map[FactID]struct{}, len(leftGroup))
		for _, fact := range leftGroup {
			beforeIDs[fact.ID] = struct{}{}
		}
		for _, fact := range rightGroup {
			if _, ok := beforeIDs[fact.ID]; !ok {
				result.Added = append(result.Added, fact.Clone())
			}
			delete(beforeIDs, fact.ID)
		}
		for _, fact := range leftGroup {
			if _, ok := beforeIDs[fact.ID]; ok {
				result.Removed = append(result.Removed, retractedFact(fact))
				delete(beforeIDs, fact.ID)
			}
		}
	}
	sortFacts(result.Added)
	sortFacts(result.Removed)
	sort.Slice(result.Changed, func(i, j int) bool { return result.Changed[i].Key < result.Changed[j].Key })
}

func appendSortedConflictDiff(result *FactSetDiff, before, after []ConflictSet) {
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(before) || rightIndex < len(after) {
		if rightIndex == len(after) || leftIndex < len(before) && before[leftIndex].ID < after[rightIndex].ID {
			result.ConflictsRemoved = append(result.ConflictsRemoved, before[leftIndex])
			leftIndex++
			continue
		}
		if leftIndex == len(before) || after[rightIndex].ID < before[leftIndex].ID {
			result.ConflictsAdded = append(result.ConflictsAdded, after[rightIndex])
			rightIndex++
			continue
		}
		leftIndex++
		rightIndex++
	}
}

func retractedFact(fact Fact) Fact {
	fact = fact.Clone()
	fact.Status = StatusRetracted
	fact.ID = factIDFor(fact)
	return fact
}

func factsByKey(values []Fact) map[FactKey][]Fact {
	out := make(map[FactKey][]Fact, len(values))
	for _, value := range values {
		// The returned facts are read-only during comparison. Cloning happens
		// only for values that cross the public Diff result boundary.
		out[value.Key] = append(out[value.Key], value)
	}
	for key := range out {
		sort.Slice(out[key], func(i, j int) bool { return out[key][i].ID < out[key][j].ID })
	}
	return out
}
