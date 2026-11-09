package intermediate_representation

// ltState holds the working state for Lengauer-Tarjan algorithm
// (during which domInfo.pre is repurposed for CFG DFS preorder number).
type ltState struct {
	// Each slice is indexed by b.Index.
	sdom     []*BasicBlock // b's semidominator
	parent   []*BasicBlock // b's parent in DFS traversal of CFG
	ancestor []*BasicBlock // b's ancestor with least sdom
}

// dfs implements the depth-first search part of the LT algorithm.
func (state *ltState) dfs(block *BasicBlock, index int32, preorder []*BasicBlock) int32 {
	preorder[index] = block
	block.dom.pre = index // For now: DFS preorder of spanning tree of CFG
	index++
	state.sdom[block.Index] = block
	state.link(nil, block)
	for _, successor := range block.Successors {
		if state.sdom[successor.Index] == nil {
			state.parent[successor.Index] = block
			index = state.dfs(successor, index, preorder)
		}
	}
	return index
}

// eval implements the EVAL part of the LT algorithm.
func (state *ltState) eval(vertex *BasicBlock) *BasicBlock {
	// TODO(adonovan): opt: do path compression per simple LT.
	minimumVertex := vertex
	for ; state.ancestor[vertex.Index] != nil; vertex = state.ancestor[vertex.Index] {
		if state.sdom[vertex.Index].dom.pre < state.sdom[minimumVertex.Index].dom.pre {
			minimumVertex = vertex
		}
	}
	return minimumVertex
}

// link implements the LINK part of the LT algorithm.
func (state *ltState) link(parent, vertex *BasicBlock) {
	state.ancestor[vertex.Index] = parent
}
