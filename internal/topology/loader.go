package topology

import (
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"synora/internal/configfile"
	"synora/pkg/contract"
)

const TopologyConfigVersion = 1

type yamlTopology struct {
	Locked bool                `yaml:"locked"`
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

func Load(path string, system *Topology) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	loaded, err := LoadBytes(data)
	if err != nil {
		return err
	}
	if system == nil {
		return contract.NewAPIError(contract.ErrorValidationFailed, "topology destination is required")
	}
	*system = *loaded
	return nil
}

// LoadBytes validates a complete replacement in memory. It never writes the
// caller's payload and therefore can safely be used before Save.
func LoadBytes(data []byte) (*Topology, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "invalid topology document: %v", err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "topology must be an object")
	}
	root := document.Content[0]
	if yamlMappingHasKey(root, "nodes") {
		var config contract.TopologyConfig
		if err := root.Decode(&config); err != nil {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "invalid flat topology: %v", err)
		}
		return FromConfig(config)
	}
	var legacy yamlTopology
	if err := root.Decode(&legacy); err != nil {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "invalid legacy topology: %v", err)
	}
	if !yamlMappingHasKey(root, "zones") {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "topology nodes or zones are required")
	}
	return fromLegacy(legacy)
}

func FromConfig(config contract.TopologyConfig) (*Topology, error) {
	if config.Version != 0 && config.Version != TopologyConfigVersion {
		return nil, contract.NewAPIError(contract.ErrorValidationFailed, "unsupported topology version %d", config.Version)
	}
	topo := &Topology{
		Nodes: make(map[string]*Node, len(config.Nodes)), Locked: config.Locked,
		RootID: strings.TrimSpace(config.RootID), HouseID: strings.TrimSpace(config.HouseID),
	}
	parents := make(map[string]string, len(config.Nodes))
	for _, item := range config.Nodes {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "topology node id is required")
		}
		if _, duplicate := topo.Nodes[id]; duplicate {
			return nil, contract.NewAPIError(contract.ErrorDuplicateID, "duplicate topology node id %q", id)
		}
		nodeType := NodeType(strings.ToLower(strings.TrimSpace(item.Type)))
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		topo.Nodes[id] = &Node{ID: id, Name: name, Type: nodeType, Metadata: cloneAnyMap(item.Metadata)}
		parents[id] = strings.TrimSpace(item.Parent)
	}
	for id, parentID := range parents {
		if parentID == "" {
			continue
		}
		parent, ok := topo.Nodes[parentID]
		if !ok {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "parent %q for node %q does not exist", parentID, id)
		}
		node := topo.Nodes[id]
		if parent == node {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "topology node %q cannot be its own parent", id)
		}
		node.Parent = parent
		parent.Children = append(parent.Children, node)
	}
	seenLinks := map[string]struct{}{}
	links := append(append([]contract.TopologyLink(nil), config.Links...), config.Edges...)
	for _, edge := range links {
		fromID := strings.TrimSpace(edge.From)
		toID := strings.TrimSpace(edge.To)
		from, fromOK := topo.Nodes[fromID]
		to, toOK := topo.Nodes[toID]
		if !fromOK || !toOK {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "topology link %q -> %q references a missing node", fromID, toID)
		}
		if from == to {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "topology node %q connects to itself", fromID)
		}
		key := linkKey(fromID, toID)
		if _, duplicate := seenLinks[key]; duplicate {
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "duplicate topology link %q", key)
		}
		seenLinks[key] = struct{}{}
		from.Connect = append(from.Connect, to.ID)
		to.Connect = append(to.Connect, from.ID)
	}
	topo.BuildGraph()
	if err := topo.Validate(); err != nil {
		return nil, err
	}
	return topo, nil
}

func fromLegacy(config yamlTopology) (*Topology, error) {
	topo := &Topology{Nodes: map[string]*Node{}, Locked: config.Locked}
	zoneIDs := sortedMapKeys(config.Zones)
	for _, zoneID := range zoneIDs {
		zone := config.Zones[zoneID]
		zoneNode := &Node{ID: zoneID, Name: zoneID, Type: NodeZone}
		topo.Nodes[zoneID] = zoneNode
		for _, floorID := range sortedMapKeys(zone.Floors) {
			floor := zone.Floors[floorID]
			fullFloorID := zoneID + "." + floorID
			floorNode := &Node{ID: fullFloorID, Name: floorID, Type: NodeFloor, Parent: zoneNode}
			topo.Nodes[fullFloorID] = floorNode
			zoneNode.Children = append(zoneNode.Children, floorNode)
			for _, roomID := range sortedMapKeys(floor.Rooms) {
				room := floor.Rooms[roomID]
				fullRoomID := fullFloorID + "." + roomID
				roomNode := &Node{
					ID: fullRoomID, Name: roomID, Type: NodeRoom, Parent: floorNode,
					Connect: normalizeIDs(room.Connect),
				}
				topo.Nodes[fullRoomID] = roomNode
				floorNode.Children = append(floorNode.Children, roomNode)
			}
		}
	}
	topo.BuildGraph()
	if err := topo.Validate(); err != nil {
		return nil, err
	}
	return topo, nil
}

func (t *Topology) ConfigView() contract.TopologyConfig {
	if t == nil {
		return contract.TopologyConfig{Version: TopologyConfigVersion, Nodes: []contract.TopologyNode{}, Links: []contract.TopologyLink{}}
	}
	ids := make([]string, 0, len(t.Nodes))
	for id := range t.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	config := contract.TopologyConfig{
		Version: TopologyConfigVersion, Locked: t.Locked, RootID: t.RootID, HouseID: t.HouseID,
		Nodes: make([]contract.TopologyNode, 0, len(ids)), Links: []contract.TopologyLink{},
	}
	seen := map[string]struct{}{}
	for _, id := range ids {
		node := t.Nodes[id]
		if node == nil {
			continue
		}
		parent := ""
		if node.Parent != nil {
			parent = node.Parent.ID
		}
		config.Nodes = append(config.Nodes, contract.TopologyNode{
			ID: node.ID, Name: node.Name, Type: string(node.Type), Parent: parent,
			Neighbors: append([]string(nil), node.Connect...), Metadata: cloneAnyMap(node.Metadata),
		})
		for _, target := range node.Connect {
			key := linkKey(node.ID, target)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			from, to := orderedPair(node.ID, target)
			config.Links = append(config.Links, contract.TopologyLink{From: from, To: to})
		}
	}
	sort.Slice(config.Links, func(i, j int) bool {
		if config.Links[i].From == config.Links[j].From {
			return config.Links[i].To < config.Links[j].To
		}
		return config.Links[i].From < config.Links[j].From
	})
	return config
}

func (t *Topology) NodeIDs() map[string]bool {
	if t == nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(t.Nodes))
	for id := range t.Nodes {
		out[id] = true
	}
	return out
}

func (t *Topology) Clone() *Topology {
	if t == nil {
		return &Topology{Nodes: map[string]*Node{}}
	}
	copy, err := FromConfig(t.ConfigView())
	if err != nil {
		return &Topology{Nodes: map[string]*Node{}}
	}
	return copy
}

func Save(path string, topo *Topology) error {
	if topo == nil {
		return contract.NewAPIError(contract.ErrorValidationFailed, "topology is required")
	}
	prepared, err := FromConfig(topo.ConfigView())
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(prepared.ConfigView())
	if err != nil {
		return err
	}
	return configfile.WriteAtomicWithBackup(path, data, 0o640)
}

func link(a, b *Node) {
	if a == nil || b == nil || a == b {
		return
	}
	if !contains(a.Neighbors, b) {
		a.Neighbors = append(a.Neighbors, b)
	}
	if !contains(b.Neighbors, a) {
		b.Neighbors = append(b.Neighbors, a)
	}
}

func contains(list []*Node, target *Node) bool {
	for _, node := range list {
		if node != nil && target != nil && node.ID == target.ID {
			return true
		}
	}
	return false
}

func linkKey(a, b string) string {
	left, right := orderedPair(a, b)
	return left + "\x00" + right
}

func orderedPair(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func normalizeIDs(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
