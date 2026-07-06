package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"synora/internal/engine/contracts"
)

func (g *GraphMemory) SaveGraph() error {

	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.graph == nil {
		return nil
	}

	err := os.MkdirAll(
		"data",
		0755,
	)

	if err != nil {
		return err
	}

	path := filepath.Join(
		"data",
		"behaviour.json",
	)

	data, err := json.MarshalIndent(
		g.graph,
		"",
		"  ",
	)

	if err != nil {
		return err
	}

	return os.WriteFile(
		path,
		data,
		0644,
	)
}

func (g *GraphMemory) LoadGraph() error {

	g.mu.Lock()
	defer g.mu.Unlock()

	path := filepath.Join(
		"data",
		"behaviour.json",
	)

	data, err := os.ReadFile(
		path,
	)

	if err != nil {
		return err
	}

	var graph contracts.BehaviorGraph

	err = json.Unmarshal(
		data,
		&graph,
	)

	if err != nil {
		return err
	}

	g.graph = &graph

	g.lastNodes =
		make(
			map[string]*contracts.BehaviorNode,
		)

	rebuildLastNodes(
		g.lastNodes,
		graph.Roots,
	)

	return nil
}

func rebuildLastNodes(
	lastNodes map[string]*contracts.BehaviorNode,
	nodes []*contracts.BehaviorNode,
) {

	for _, node := range nodes {

		key :=
			string(node.SubjectType) +
				":" +
				node.SubjectID

		existing, ok :=
			lastNodes[key]

		if !ok ||
			node.LastSeen.After(
				existing.LastSeen,
			) {

			lastNodes[key] =
				node
		}

		rebuildLastNodes(
			lastNodes,
			node.Children,
		)
	}
}

func findMostRecentNode(
	nodes []*contracts.BehaviorNode,
) *contracts.BehaviorNode {

	var newest *contracts.BehaviorNode

	for _, node := range nodes {

		if newest == nil ||
			node.LastSeen.After(
				newest.LastSeen,
			) {

			newest = node
		}

		childNewest :=
			findMostRecentNode(
				node.Children,
			)

		if childNewest != nil {

			if newest == nil ||
				childNewest.LastSeen.After(
					newest.LastSeen,
				) {

				newest =
					childNewest
			}
		}
	}

	return newest
}

func (g *GraphMemory) GraphExists() bool {

	path := filepath.Join(
		"data",
		"behaviour.json",
	)

	_, err :=
		os.Stat(path)

	return err == nil
}

func (g *GraphMemory) TouchGraph() {

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.graph == nil {
		return
	}

	g.graph.LastUpdate =
		time.Now()

	g.graph.Version++
}