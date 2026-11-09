package static_single_assignment

// Dominance is the immediate-dominator array over a function's block slice, by position in it, and the
// dominator tree it makes.
type Dominance struct {
	// position maps a block id to its index in the order ComputeDominance made: a reverse postorder of
	// the real edges from the entry, then the blocks they don't reach.
	position map[BlockId]int
	// blocks is position's inverse, the id of the block at each index.
	blocks []BlockId
	// immediate[i] is the index of block i's immediate dominator; the entry is its own, and a block the
	// real edges don't reach has none (-1), dominated by nothing but itself.
	immediate []int
	// children holds every block's children in the tree, grouped by parent: block i's are
	// children[childrenStart[i]:childrenStart[i+1]], in go/ir's order (ComputeDominance says why). One
	// array for all of them rather than a slice per block, so the tree is two allocations, not one per
	// block.
	children      []BlockId
	childrenStart []int
}

// Dominates reports whether every path from the entry to block passes through dominator.
//
// A block dominates itself, the standard convention.
func (dominance *Dominance) Dominates(dominator, block BlockId) bool {
	target, ok := dominance.position[dominator]
	if !ok {
		return false
	}
	index, ok := dominance.position[block]
	if !ok {
		return false
	}
	for {
		if index == target {
			return true
		}
		if index == 0 || dominance.immediate[index] == -1 {
			return false
		}
		index = dominance.immediate[index]
	}
}

// ImmediateDominator is the block's parent in the dominator tree: the dominator every other one of its
// strict dominators also dominates. It is go/ir's BasicBlock.Idom.
//
// False for the entry, which has no parent, for a block the real edges don't reach, which is in no tree,
// and for a block the function does not hold.
func (dominance *Dominance) ImmediateDominator(block BlockId) (BlockId, bool) {
	index, ok := dominance.position[block]
	if !ok || index == 0 || dominance.immediate[index] == -1 {
		return 0, false
	}
	return dominance.blocks[dominance.immediate[index]], true
}

// Children are the blocks the block immediately dominates, its children in the dominator tree, in the
// order go/ir's BasicBlock.Dominees gives them: the depth-first preorder of the real edges from the entry.
//
// None for a block the real edges don't reach, which is in no tree, and for a block the function does
// not hold. The slice shares the tree's array, capped so an append copies rather than overwriting a
// sibling's children, and is the caller's to read, not to change.
func (dominance *Dominance) Children(block BlockId) []BlockId {
	index, ok := dominance.position[block]
	if !ok || dominance.childrenStart == nil {
		return nil
	}
	start, end := dominance.childrenStart[index], dominance.childrenStart[index+1]
	if start == end {
		return nil
	}
	return dominance.children[start:end:end]
}

// Preorder is the dominator tree walked from the entry, each block before its children and the children
// in Children's order: go/ir's Function.DomPreorder, as a new slice.
//
// It holds only the blocks the real edges reach. go/ir's walk ends with its Recover block's tree, a
// second root this module has no notion of; here a block the real edges don't reach is in no tree and
// in no walk.
func (dominance *Dominance) Preorder() []BlockId {
	// No tree was built when the real edges reach nothing, not even the entry: a function without one.
	if dominance.childrenStart == nil {
		return nil
	}
	order := make([]BlockId, 0, len(dominance.children)+1)
	// The stack holds blocks still to visit, each parent's children pushed last first so the first child
	// pops next.
	stack := []int{0}
	for len(stack) > 0 {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		order = append(order, dominance.blocks[index])
		start, end := dominance.childrenStart[index], dominance.childrenStart[index+1]
		for child := end - 1; child >= start; child-- {
			stack = append(stack, dominance.position[dominance.children[child]])
		}
	}
	return order
}

// collectChildren builds the tree's children from the immediate dominators, appending each block the
// real edges reach to its parent's children in preorder: the ids of those blocks in the depth-first
// preorder of the real edges, the entry first. ComputeDominance calls it only when the edges reach the
// entry, so preorder holds one block at least.
//
// Mutates: dominance its children and childrenStart are built here, once, after the fixed point settles
func (dominance *Dominance) collectChildren(preorder []BlockId) {
	dominance.childrenStart = make([]int, len(dominance.blocks)+1)
	parents := make([]int, len(preorder))
	for order, id := range preorder[1:] {
		parent := dominance.immediate[dominance.position[id]]
		if parent == -1 {
			// Invariant: every block the real edges reach has a reached predecessor ahead of it in reverse
			// postorder, so the fixed point gives every one but the entry an immediate dominator.
			panic("static_single_assignment: a block the entry reaches has no immediate dominator")
		}
		parents[order] = parent
		dominance.childrenStart[parent+1]++
	}
	for index := 1; index < len(dominance.childrenStart); index++ {
		dominance.childrenStart[index] += dominance.childrenStart[index-1]
	}
	dominance.children = make([]BlockId, len(preorder)-1)
	// filled is how many children each parent has been given so far, which places the next one.
	filled := make([]int, len(dominance.blocks))
	for order, id := range preorder[1:] {
		parent := parents[order]
		dominance.children[dominance.childrenStart[parent]+filled[parent]] = id
		filled[parent]++
	}
}
