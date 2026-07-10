package contract

type Node struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Connect  []string `json:"connect,omitempty"`
	Children []Node   `json:"children,omitempty"`
}

type Topology struct {
	Tree   []Node         `json:"tree,omitempty"`
	Nodes  []TopologyNode `json:"nodes,omitempty"`
	Links  []TopologyLink `json:"links,omitempty"`
	Locked bool           `json:"locked"`
}

type NodeView struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	DynamicScore float64    `json:"dynamic_score"`
	Connect      []string   `json:"connect,omitempty"`
	Children     []NodeView `json:"children,omitempty"`
}

type TopologySnapshot struct {
	Nodes  []TopologyNode `json:"nodes"`
	Links  []TopologyLink `json:"links,omitempty"`
	Locked bool           `json:"locked"`
}

type TopologyNode struct {
	ID        string         `json:"id" yaml:"id"`
	Name      string         `json:"name,omitempty" yaml:"name,omitempty"`
	Type      string         `json:"type" yaml:"type"`
	Parent    string         `json:"parent,omitempty" yaml:"parent,omitempty"`
	Neighbors []string       `json:"neighbors,omitempty" yaml:"-"`
	Metadata  map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type TopologyLink struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
}

// TopologyConfig is the canonical complete-replacement representation persisted
// by Core. The legacy zones/floors/rooms YAML is accepted only when loading.
type TopologyConfig struct {
	Version int            `json:"version,omitempty" yaml:"version,omitempty"`
	Locked  bool           `json:"locked" yaml:"locked"`
	RootID  string         `json:"root_id,omitempty" yaml:"root_id,omitempty"`
	HouseID string         `json:"house_id,omitempty" yaml:"house_id,omitempty"`
	Nodes   []TopologyNode `json:"nodes" yaml:"nodes"`
	Links   []TopologyLink `json:"links" yaml:"links"`
	Edges   []TopologyLink `json:"edges,omitempty" yaml:"edges,omitempty"`
}
