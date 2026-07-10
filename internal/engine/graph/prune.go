package graph

import (
	"sort"
	"time"
)

func (g *GraphMemory) pruneInspectionLocked() {
	g.pruneSequencesLocked(CGEMaxSequences)
	g.pruneTransitionsLocked(CGEMaxTransitions)
	g.pruneBehaviorsLocked(CGEMaxBehaviors)
	g.pruneRunTrackingLocked(CGEMaxSequences)
	g.pruneRecentEventsLocked(CGEMaxSequences)
}

func (g *GraphMemory) pruneSequencesLocked(limit int) {
	if limit <= 0 || len(g.learnedSequences) <= limit {
		return
	}
	sorted := sortedSequences(g.learnedSequences)
	keepIDs := make(map[string]bool, limit)
	for _, item := range limitSequences(sorted, limit) {
		keepIDs[item.ID] = true
	}
	for key, item := range g.learnedSequences {
		if item == nil || !keepIDs[item.ID] {
			delete(g.learnedSequences, key)
		}
	}
}

func (g *GraphMemory) pruneTransitionsLocked(limit int) {
	if limit <= 0 || len(g.learnedTransitions) <= limit {
		return
	}
	sorted := sortedTransitions(g.learnedTransitions)
	keepIDs := make(map[string]bool, limit)
	for _, item := range limitTransitions(sorted, limit) {
		keepIDs[item.ID] = true
	}
	for key, item := range g.learnedTransitions {
		if item == nil || !keepIDs[item.ID] {
			delete(g.learnedTransitions, key)
		}
	}
}

func (g *GraphMemory) pruneBehaviorsLocked(limit int) {
	if limit <= 0 || len(g.learnedBehaviors) <= limit {
		return
	}
	sorted := sortedBehaviors(g.learnedBehaviors)
	keepIDs := make(map[string]bool, limit)
	for _, item := range limitBehaviors(sorted, limit) {
		keepIDs[item.ID] = true
	}
	for key, item := range g.learnedBehaviors {
		if item == nil || !keepIDs[item.ID] {
			delete(g.learnedBehaviors, key)
		}
	}
}

func (g *GraphMemory) pruneRunTrackingLocked(limit int) {
	if limit <= 0 || len(g.runEvents) <= limit {
		return
	}
	type runItem struct {
		id       string
		lastSeen time.Time
	}
	items := make([]runItem, 0, len(g.runEvents))
	for id, events := range g.runEvents {
		lastSeen := time.Time{}
		if len(events) > 0 {
			lastSeen = events[len(events)-1].Timestamp
		}
		items = append(items, runItem{id: id, lastSeen: lastSeen})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].lastSeen.Equal(items[j].lastSeen) {
			return items[i].lastSeen.After(items[j].lastSeen)
		}
		return items[i].id < items[j].id
	})
	for _, item := range items[limit:] {
		delete(g.runEvents, item.id)
		delete(g.countedRunKeys, item.id)
		delete(g.lastSequenceByRun, item.id)
	}
}

func (g *GraphMemory) pruneRecentEventsLocked(limit int) {
	if limit <= 0 || len(g.recentEvents) <= limit {
		return
	}
	type scopeItem struct {
		id       string
		lastSeen time.Time
	}
	items := make([]scopeItem, 0, len(g.recentEvents))
	for id, events := range g.recentEvents {
		lastSeen := time.Time{}
		if len(events) > 0 {
			lastSeen = events[len(events)-1].Timestamp
		}
		items = append(items, scopeItem{id: id, lastSeen: lastSeen})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].lastSeen.Equal(items[j].lastSeen) {
			return items[i].lastSeen.After(items[j].lastSeen)
		}
		return items[i].id < items[j].id
	})
	for _, item := range items[limit:] {
		delete(g.recentEvents, item.id)
	}
}
