package allowed

// A themed file of Graph's methods alone, by pointer and by value, serves one type.
func (graph *Graph) Neighbors(from string) []string {
	return graph.edges[from]
}

func (graph Graph) Size() int {
	return len(graph.edges)
}
