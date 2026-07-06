package topology

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// =========================
// YAML STRUCTS
// =========================

type yamlTopology struct {
	Locked bool              `yaml:"locked"`
	Zones  map[string]yamlZone `yaml:"zones"`
}

type yamlZone struct {
	Floors map[string]yamlFloor `yaml:"floors"`
}

type yamlFloor struct {
	Rooms map[string]yamlRoom `yaml:"rooms"`
}

type yamlRoom struct {
	Connect []string `yaml:"connect"`
}

// =========================
// LOAD
// =========================

func Load(path string, system *Topology) error {

	system.Nodes = map[string]*Node{}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var y yamlTopology

	if err := yaml.Unmarshal(data, &y); err != nil {
		return err
	}

	system.Locked = y.Locked

	if len(y.Zones) == 0 {
		return fmt.Errorf("topology: no zones defined")
	}

	// =========================
	// BUILD TREE
	// =========================

	for zoneID, zone := range y.Zones {

		zoneNode := &Node{
			ID:   zoneID,
			Name: zoneID,
			Type: NodeZone,
		}

		system.Nodes[zoneID] = zoneNode

		for floorID, floor := range zone.Floors {

			floorIDFull := zoneID + "." + floorID

			floorNode := &Node{
				ID:     floorIDFull,
				Name:   floorID,
				Type:   NodeFloor,
				Parent: zoneNode,
			}

			system.Nodes[floorIDFull] = floorNode
			zoneNode.Children = append(zoneNode.Children, floorNode)

			for roomID, room := range floor.Rooms {

				roomIDFull := floorIDFull + "." + roomID

				roomNode := &Node{
					ID:      roomIDFull,
					Name:    roomID,
					Type:    NodeRoom,
					Parent:  floorNode,
					Connect: room.Connect,
				}

				system.Nodes[roomIDFull] = roomNode
				floorNode.Children = append(floorNode.Children, roomNode)
			}
		}
	}

	// =========================
	// BUILD GRAPH
	// =========================

	for zoneID, zone := range y.Zones {
		for floorID, floor := range zone.Floors {

			for roomID, room := range floor.Rooms {

				fromID := zoneID + "." + floorID + "." + roomID

				fromNode, ok := system.Nodes[fromID]
				if !ok {
					return fmt.Errorf("missing node: %s", fromID)
				}

				for _, targetID := range room.Connect {

					if targetID == fromID {
						return fmt.Errorf("self connect: %s", fromID)
					}

					toNode, ok := system.Nodes[targetID]
					if !ok {
						return fmt.Errorf("invalid connection: %s -> %s", fromID, targetID)
					}

					link(fromNode, toNode)
				}
			}
		}
	}

	// =========================
	// WARNINGS
	// =========================

	for id, node := range system.Nodes {

		if node.Type == NodeRoom && len(node.Neighbors) == 0 {
			fmt.Printf("⚠ isolated room: %s\n", id)
		}
	}

	// =========================
	// DEBUG GRAPH
	// =========================

	fmt.Println("----- TOPOLOGY GRAPH -----")

	for id, node := range system.Nodes {

		fmt.Printf(
			"node=%s type=%s parent=%v\n",
	     id,
	     node.Type,
	     func() string {
		     if node.Parent != nil {
			     return node.Parent.ID
		     }
		     return "none"
	     }(),
		)

		if len(node.Connect) > 0 {
			fmt.Printf("  yaml connect: %v\n", node.Connect)
		}

		if len(node.Neighbors) > 0 {

			fmt.Printf("  neighbors: ")

			for _, n := range node.Neighbors {
				fmt.Printf("%s ", n.ID)
			}

			fmt.Println()
		}

		fmt.Println()
	}

	fmt.Println("--------------------------")

	// =========================
	// VALIDATION
	// =========================

	if err := system.Validate(); err != nil {
		return err
	}

	// =========================
	// GRAPH BUILD
	// =========================

	system.BuildGraph()

	fmt.Printf(
		"topology loaded: nodes=%d locked=%v\n",
		len(system.Nodes),
		   system.Locked,
	)

	return nil
}

// =========================
// LINK
// =========================

func link(a, b *Node) {

	if !contains(a.Neighbors, b) {
		a.Neighbors = append(a.Neighbors, b)
	}

	if !contains(b.Neighbors, a) {
		b.Neighbors = append(b.Neighbors, a)
	}
}

func contains(list []*Node, target *Node) bool {
	for _, n := range list {
		if n.ID == target.ID {
			return true
		}
	}
	return false
}

// =========================
// HELPERS
// =========================

func getFloor(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

func Save(path string, topo *Topology) error {

	data, err := yaml.Marshal(topo)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}
