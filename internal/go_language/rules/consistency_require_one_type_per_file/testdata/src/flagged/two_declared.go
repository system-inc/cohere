package flagged

// Two declared types with methods: Graph's sit here, and Node's in node_walk.go still count, since a method anywhere in
// the package gives Node behavior a reader looks for in its own file.
type Graph struct {
	nodes []*Node
}

func (graph *Graph) Size() int {
	return len(graph.nodes)
}

type Node struct { // want "already serves `Graph`, and `Node` is a second type with methods"
	name string
}
