package allowed

// A file of Graph's methods alone may be named for their theme after Graph's own name.
func (graph *Graph) Neighbors(from string) []string {
	return graph.edges[from]
}

func (graph Graph) Size() int {
	return len(graph.edges)
}
