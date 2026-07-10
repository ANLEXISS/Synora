package topology

type NodeType string

const (
	NodeRoot      NodeType = "root"
	NodeHouse     NodeType = "house"
	NodeZone      NodeType = "zone"
	NodeFloor     NodeType = "floor"
	NodeRoom      NodeType = "room"
	NodeDevice    NodeType = "device"
	NodeUnlocated NodeType = "unlocated"
)

type Node struct {
	ID   string
	Name string
	Type NodeType

	Parent   *Node
	Children []*Node

	Connect   []string       `yaml:"connect,omitempty"`
	Neighbors []*Node        `yaml:"-"`
	Metadata  map[string]any `yaml:"metadata,omitempty"`
}

type Topology struct {
	Nodes   map[string]*Node
	Locked  bool
	RootID  string
	HouseID string
}
