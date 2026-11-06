package allowed

// One type with its methods, in the file named for it.
type Graph struct {
	edges map[string][]string
}

func NewGraph() *Graph {
	return &Graph{edges: map[string][]string{}}
}

func (graph *Graph) Connect(from string, to string) {
	graph.edges[from] = append(graph.edges[from], to)
}
