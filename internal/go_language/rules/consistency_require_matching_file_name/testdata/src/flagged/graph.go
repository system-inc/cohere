package flagged

// Graph is declared in the file named for it, and walk.go holds more of its methods.
type Graph struct {
	edges map[string][]string
}

func (graph *Graph) Connect(from string, to string) {
	graph.edges[from] = append(graph.edges[from], to)
}
