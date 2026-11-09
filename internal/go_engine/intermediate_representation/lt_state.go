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
//
// Mutates: preorder gains each block the search reaches, at its preorder number
func (state *ltState) dfs(block *BasicBlock, index int32, preorder []*BasicBlock) int32 {
	preorder[index] = block
	block.dom.pre = index // For now: DFS preorder of spanning tree of CFG
	nextIndex := index + 1
	state.sdom[block.Index] = block
	state.link(nil, block)
	for _, successor := range block.Successors {
		if state.sdom[successor.Index] == nil {
			state.parent[successor.Index] = block
			nextIndex = state.dfs(successor, nextIndex, preorder)
		}
	}
	return nextIndex
}

// eval implements the `EVAL` part of the LT algorithm.
func (state *ltState) eval(vertex *BasicBlock) *BasicBlock {
	// It walks the whole ancestor chain, without the path compression of simple LT.
	minimumVertex := vertex
	for ancestor := vertex; state.ancestor[ancestor.Index] != nil; ancestor = state.ancestor[ancestor.Index] {
		if state.sdom[ancestor.Index].dom.pre < state.sdom[minimumVertex.Index].dom.pre {
			minimumVertex = ancestor
		}
	}
	return minimumVertex
}

// link implements the `LINK` part of the LT algorithm.
func (state *ltState) link(parent, vertex *BasicBlock) {
	state.ancestor[vertex.Index] = parent
}
