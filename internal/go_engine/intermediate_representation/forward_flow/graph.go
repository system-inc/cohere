package forward_flow

import (
	"iter"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Graph is a control flow graph whose nodes are numbered from zero, as a function's blocks are by their indices: the
// part of the original's graph.Graph that Forward reads, once the original's compaction has mapped a function's blocks
// to themselves. A function is one; the tests hand Forward graphs of their own.
type Graph interface {
	// NumNodes is how many nodes the graph holds, numbered from zero.
	NumNodes() int
	// Out yields the nodes node has an edge to, the same order every time.
	Out(node int) iter.Seq[int]
}

var _ Graph = (*intermediate_representation.Function)(nil)
