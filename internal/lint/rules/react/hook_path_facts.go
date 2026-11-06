package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
)

// hookPathFacts is one code path root's graph and the analysis over it, plus a map from a Hook call
// to the block it was laid out in.
//
// Keyed by the call node's position rather than by the node pointer, for the reason
// `internal/unused` found the hard way: the graph lays a `finally` block out twice and both copies
// carry the same positions, so the first **reachable** block a position lands in is the one that
// answers for it.
type hookPathFacts struct {
	analysis *control_flow_graph.PathAnalysis[hookEvent]
	blocks   map[int]*control_flow_graph.Block[hookEvent]
}

func (facts *hookPathFacts) blockOf(call *ast.Node) (*control_flow_graph.Block[hookEvent], bool) {
	block, found := facts.blocks[call.Pos()]
	return block, found
}
