package forward_flow

import (
	"iter"
	"slices"
)

// simpleGraph is a graph of nodeCount nodes whose edges out of each node are listed, upstream's test graph.
type simpleGraph struct {
	nodeCount int
	edges     [][]int
}

func (graph *simpleGraph) NumNodes() int { return graph.nodeCount }

func (graph *simpleGraph) Out(node int) iter.Seq[int] { return slices.Values(graph.edges[node]) }
