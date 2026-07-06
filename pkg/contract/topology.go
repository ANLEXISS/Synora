package contract

type Node struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Connect  []string `json:"connect,omitempty"`
	Children []Node   `json:"children,omitempty"`
}

type Topology struct {
	Tree []Node `json:"tree"`
}

type NodeView struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	DynamicScore float64    `json:"dynamic_score"`
	Connect      []string   `json:"connect,omitempty"`
	Children     []NodeView `json:"children,omitempty"`
}

type TopologySnapshot struct {
	Nodes []TopologyNode `json:"nodes"`
}

type TopologyNode struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Parent    string   `json:"parent,omitempty"`
	Neighbors []string `json:"neighbors,omitempty"`
}