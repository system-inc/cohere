package allowed

// go/build reads a file's name up to its first dot, and so does the rule: this file is graph.
func (graph *Graph) Clear() {
	graph.edges = map[string][]string{}
}
