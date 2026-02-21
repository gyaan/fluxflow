package dag

// Graph holds nodes and their parent→children adjacency list.
// It is immutable once built; hot-reload creates a new Graph and swaps atomically.
type Graph struct {
	nodes    map[string]Node   // id → Node
	children map[string][]Node // parent id → ordered children
	roots    []*ScenarioNode   // entry points
}

// NewGraph allocates an empty Graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:    make(map[string]Node),
		children: make(map[string][]Node),
	}
}

// AddNode registers a node by its ID.
func (g *Graph) AddNode(n Node) {
	g.nodes[n.ID()] = n
	if sn, ok := n.(*ScenarioNode); ok {
		g.roots = append(g.roots, sn)
	}
}

// AddEdge records that parent has child as a direct successor.
func (g *Graph) AddEdge(parentID string, child Node) {
	g.children[parentID] = append(g.children[parentID], child)
}

// Node returns a node by ID (nil if not found).
func (g *Graph) Node(id string) Node {
	return g.nodes[id]
}

// Children returns the direct successors of a node.
func (g *Graph) Children(id string) []Node {
	return g.children[id]
}

// Roots returns all ScenarioNodes (DFS entry points).
func (g *Graph) Roots() []*ScenarioNode {
	return g.roots
}

// NodeCount returns the total number of registered nodes.
func (g *Graph) NodeCount() int {
	return len(g.nodes)
}
