package flagged

// A file of Graph's methods alone, named for neither Graph nor a theme of it, is pointed at its first receiver.
func (graph *Graph) Neighbors(from string) []string { // want "Name it `graph.go`, or `graph_<purpose>.go` if it holds"
	return graph.edges[from]
}

func (graph Graph) Size() int {
	return len(graph.edges)
}
