// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines algorithms related to dominance.

// Dominator tree construction ----------------------------------------
//
// We use the algorithm described in Lengauer & Tarjan. 1979.  A fast
// algorithm for finding dominators in a flowgraph.
// https://doi.acm.org/10.1145/357062.357071
//
// We also apply the optimizations to SLT described in Georgiadis et
// al, Finding Dominators in Practice, JGAA 2006,
// https://jgaa.info/accepted/2006/GeorgiadisTarjanWerneck2006.10.1.pdf
// to avoid the need for buckets of size > 1.

import (
	"slices"
	"sort"
)

// ImmediateDominator returns the block that immediately dominates b:
// its parent in the dominator tree, if any.
// Neither the entry node (b.Index==0) nor recover node
// (b==b.Parent().Recover()) have a parent.
func (block *BasicBlock) ImmediateDominator() *BasicBlock { return block.dom.idom }

// Dominees returns the list of blocks that b immediately dominates:
// its children in the dominator tree.
func (block *BasicBlock) Dominees() []*BasicBlock { return block.dom.children }

// Dominates reports whether b dominates `basicBlock`.
func (block *BasicBlock) Dominates(basicBlock *BasicBlock) bool {
	return block.dom.pre <= basicBlock.dom.pre && basicBlock.dom.post <= block.dom.post
}

// DominatorPreorder returns a new slice containing the blocks of f
// in a preorder traversal of the dominator tree.
func (function *Function) DominatorPreorder() []*BasicBlock {
	slice := slices.Clone(function.Blocks)
	sort.Slice(slice, func(left, right int) bool {
		return slice[left].dom.pre < slice[right].dom.pre
	})
	return slice
}

// DominatorPostorder returns a new slice containing the blocks of f
// in a postorder traversal of the dominator tree.
// (This is not the same as a postdominance order.)
func (function *Function) DominatorPostorder() []*BasicBlock {
	slice := slices.Clone(function.Blocks)
	sort.Slice(slice, func(left, right int) bool {
		return slice[left].dom.post < slice[right].dom.post
	})
	return slice
}

// domInfo contains a BasicBlock's dominance information.
type domInfo struct {
	idom      *BasicBlock   // immediate dominator (parent in domtree)
	children  []*BasicBlock // nodes immediately dominated by this one
	pre, post int32         // pre- and post-order numbering within domtree
}

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

// buildDominatorTree computes the dominator tree of f using the LT algorithm.
// Precondition: all blocks are reachable (e.g. optimizeBlocks has been run).
//
// The core's dominator tree goes here (#0scefye). This is go/ir's own until static_single_assignment's
// tree, built in go/ir's Successors order, lands on main and is proven equal to it, block for block,
// Recover's second root included; then this function and the domInfo it fills give way to it.
func buildDominatorTree(function *Function) {
	// The step numbers refer to the original LT paper; the
	// reordering is due to Georgiadis.

	// Clear any previous domInfo.
	for _, block := range function.Blocks {
		block.dom = domInfo{}
	}

	blockCount := len(function.Blocks)
	// Allocate space for 5 contiguous [n]*BasicBlock arrays:
	// sdom, parent, ancestor, preorder, buckets.
	space := make([]*BasicBlock, 5*blockCount)
	lt := ltState{
		sdom:     space[0:blockCount],
		parent:   space[blockCount : 2*blockCount],
		ancestor: space[2*blockCount : 3*blockCount],
	}

	// Step 1.  Number vertices by depth-first preorder.
	preorder := space[3*blockCount : 4*blockCount]
	root := function.Blocks[0]
	prenum := lt.dfs(root, 0, preorder)
	recover := function.Recover
	if recover != nil {
		lt.dfs(recover, prenum, preorder)
	}

	buckets := space[4*blockCount : 5*blockCount]
	copy(buckets, preorder)

	// In reverse preorder...
	for index := int32(blockCount) - 1; index > 0; index-- {
		vertex := preorder[index]

		// Step 3. Implicitly define the immediate dominator of each node.
		for bucket := buckets[index]; bucket != vertex; bucket = buckets[bucket.dom.pre] {
			minimumVertex := lt.eval(bucket)
			if lt.sdom[minimumVertex.Index].dom.pre < index {
				bucket.dom.idom = minimumVertex
			} else {
				bucket.dom.idom = vertex
			}
		}

		// Step 2. Compute the semidominators of all nodes.
		lt.sdom[vertex.Index] = lt.parent[vertex.Index]
		for _, predecessor := range vertex.Predecessors {
			minimumVertex := lt.eval(predecessor)
			if lt.sdom[minimumVertex.Index].dom.pre < lt.sdom[vertex.Index].dom.pre {
				lt.sdom[vertex.Index] = lt.sdom[minimumVertex.Index]
			}
		}

		lt.link(lt.parent[vertex.Index], vertex)

		if lt.parent[vertex.Index] == lt.sdom[vertex.Index] {
			vertex.dom.idom = lt.parent[vertex.Index]
		} else {
			buckets[index] = buckets[lt.sdom[vertex.Index].dom.pre]
			buckets[lt.sdom[vertex.Index].dom.pre] = vertex
		}
	}

	// The final 'Step 3' is now outside the loop.
	for bucket := buckets[0]; bucket != root; bucket = buckets[bucket.dom.pre] {
		bucket.dom.idom = root
	}

	// Step 4. Explicitly define the immediate dominator of each
	// node, in preorder.
	for _, block := range preorder[1:] {
		if block == root || block == recover {
			block.dom.idom = nil
		} else {
			if block.dom.idom != lt.sdom[block.Index] {
				block.dom.idom = block.dom.idom.dom.idom
			}
			// Calculate Children relation as inverse of ImmediateDominator.
			block.dom.idom.dom.children = append(block.dom.idom.dom.children, block)
		}
	}

	pre, post := numberDominatorTree(root, 0, 0)
	if recover != nil {
		numberDominatorTree(recover, pre, post)
	}
}

// numberDomTree sets the pre- and post-order numbers of a depth-first
// traversal of the dominator tree rooted at `block`.  These are used to
// answer dominance queries in constant time.
func numberDominatorTree(block *BasicBlock, pre, post int32) (int32, int32) {
	block.dom.pre = pre
	pre++
	for _, child := range block.dom.children {
		pre, post = numberDominatorTree(child, pre, post)
	}
	block.dom.post = post
	post++
	return pre, post
}
