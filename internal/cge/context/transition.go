package context

import (
	"fmt"
	"sort"
	"time"
)

type DistanceStatus string

const (
	DistanceReachable   DistanceStatus = "reachable"
	DistanceUnreachable DistanceStatus = "unreachable"
	DistanceUnknown     DistanceStatus = "unknown"
)

type TransitionFact struct {
	Code string `json:"code"`
}
type TransitionAssessment struct {
	PreviousObservationID   string           `json:"previous_observation_id"`
	CurrentObservationID    string           `json:"current_observation_id"`
	Elapsed                 time.Duration    `json:"elapsed"`
	SameNode                bool             `json:"same_node"`
	SameZone                bool             `json:"same_zone"`
	Adjacent                bool             `json:"adjacent"`
	GraphDistance           int              `json:"graph_distance"`
	DistanceStatus          DistanceStatus   `json:"distance_status"`
	EntryTransition         bool             `json:"entry_transition"`
	ExitTransition          bool             `json:"exit_transition"`
	ExteriorTransition      bool             `json:"exterior_transition"`
	OccupancyChanged        bool             `json:"occupancy_changed"`
	HouseModeChanged        bool             `json:"house_mode_changed"`
	TemporalBucketChanged   bool             `json:"temporal_bucket_changed"`
	TopologyRevisionChanged bool             `json:"topology_revision_changed"`
	Facts                   []TransitionFact `json:"facts"`
}

// ShortestPath returns the deterministic unweighted distance between two
// nodes. Unknown endpoints and unreachable endpoints have distinct statuses.
func ShortestPath(topology TopologySnapshot, from, to string) (int, DistanceStatus, error) {
	if err := topology.Validate(); err != nil {
		return -1, DistanceUnknown, err
	}
	distance, status := shortestPath(topology, from, to)
	return distance, status, nil
}

func EvaluateTransition(previous, current Frame, topology TopologySnapshot) (TransitionAssessment, error) {
	if err := previous.Validate(); err != nil {
		return TransitionAssessment{}, err
	}
	if err := current.Validate(); err != nil {
		return TransitionAssessment{}, err
	}
	if current.ObservedAt.Before(previous.ObservedAt) {
		return TransitionAssessment{}, fmt.Errorf("transition timestamps are not ordered")
	}
	assessment := TransitionAssessment{PreviousObservationID: previous.ObservationID, CurrentObservationID: current.ObservationID, Elapsed: current.ObservedAt.Sub(previous.ObservedAt), SameNode: previous.NodeID != "" && previous.NodeID == current.NodeID, SameZone: previous.ZoneID != "" && previous.ZoneID == current.ZoneID, OccupancyChanged: previous.Occupancy != current.Occupancy, HouseModeChanged: previous.HouseMode != current.HouseMode, TemporalBucketChanged: previous.Time.MinuteOfDay/15 != current.Time.MinuteOfDay/15, TopologyRevisionChanged: previous.TopologyRevision != current.TopologyRevision, GraphDistance: -1, DistanceStatus: DistanceUnknown}
	if assessment.TopologyRevisionChanged {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.topology_revision_changed"})
	} else {
		distance, status := shortestPath(topology, previous.NodeID, current.NodeID)
		assessment.GraphDistance = distance
		assessment.DistanceStatus = status
		assessment.Adjacent = status == DistanceReachable && distance == 1
		if status == DistanceUnreachable {
			assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.unreachable"})
		}
		if status == DistanceReachable {
			assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.graph_distance"})
		}
	}
	assessment.EntryTransition = previous.EntryPoint != current.EntryPoint && current.EntryPoint
	assessment.ExitTransition = previous.EntryPoint != current.EntryPoint && previous.EntryPoint
	assessment.ExteriorTransition = previous.Exterior != current.Exterior
	if assessment.SameNode {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.same_node"})
	}
	if assessment.SameZone {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.same_zone"})
	}
	if assessment.Adjacent {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.adjacent"})
	}
	if assessment.EntryTransition {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.entry"})
	}
	if assessment.ExitTransition {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.exit"})
	}
	if assessment.ExteriorTransition {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.exterior"})
	}
	if assessment.OccupancyChanged {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.occupancy_changed"})
	}
	if assessment.HouseModeChanged {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.house_mode_changed"})
	}
	if assessment.TemporalBucketChanged {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.temporal_bucket_changed"})
	}
	if previous.Quality != QualityComplete || current.Quality != QualityComplete {
		assessment.Facts = append(assessment.Facts, TransitionFact{Code: "transition.context_partial"})
	}
	return assessment, nil
}

func shortestPath(topology TopologySnapshot, from, to string) (int, DistanceStatus) {
	if err := topology.Validate(); err != nil {
		return -1, DistanceUnknown
	}
	nodes := map[string]struct{}{}
	for _, n := range topology.Nodes {
		nodes[n.ID] = struct{}{}
	}
	if _, ok := nodes[from]; !ok {
		return -1, DistanceUnknown
	}
	if _, ok := nodes[to]; !ok {
		return -1, DistanceUnknown
	}
	if from == to {
		return 0, DistanceReachable
	}
	neighbors := map[string][]string{}
	for _, e := range topology.Edges {
		neighbors[e.From] = append(neighbors[e.From], e.To)
		if !e.Directed {
			neighbors[e.To] = append(neighbors[e.To], e.From)
		}
	}
	for id := range neighbors {
		sort.Strings(neighbors[id])
	}
	type item struct {
		id       string
		distance int
	}
	queue := []item{{from, 0}}
	visited := map[string]bool{from: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range neighbors[current.id] {
			if visited[next] {
				continue
			}
			if next == to {
				return current.distance + 1, DistanceReachable
			}
			visited[next] = true
			queue = append(queue, item{next, current.distance + 1})
		}
	}
	return -1, DistanceUnreachable
}
