package intermediate_representation

import "slices"

// Loop is the set of blocks one back-edge closes: the loop's header, the block the edge leaves from, and every
// block that reaches that block without passing through the header.
type Loop struct{ *BlockSet }

// FindLoops returns function's natural loops, one per back-edge, an edge to a block that dominates the block it
// leaves from. Headers come in dominator preorder and a header's back-edges in predecessor order, so a header
// with two back-edges heads two loops. A function with no blocks, as an external one has, has none. It is
// staticcheck's irutil.FindLoops (honnef.co/go/tools@v0.8.1 go/ir/irutil/loops.go), translated, not copied, onto
// our IR, so a reader moving from go/ir finds the same loops through it.
func FindLoops(function *Function) []Loop {
	if function.Blocks == nil {
		return nil
	}
	tree := function.DominatorPreorder()
	var sets []Loop
	for _, header := range tree {
		for _, predecessor := range header.Predecessors {
			if !header.Dominates(predecessor) {
				continue
			}
			// predecessor is a back-edge to header, the loop's header.
			if predecessor == header {
				set := Loop{NewBlockSet(len(function.Blocks))}
				set.Add(predecessor)
				sets = append(sets, set)
				continue
			}
			set := Loop{NewBlockSet(len(function.Blocks))}
			set.Add(header)
			set.Add(predecessor)
			for _, block := range allPredecessorsBut(predecessor, header, nil) {
				set.Add(block)
			}
			sets = append(sets, set)
		}
	}
	return sets
}

// allPredecessorsBut appends to list every block that reaches block through predecessors without passing through
// but, each once, depth first, and returns the list. It is irutil's allPredsBut, translated as FindLoops is: its
// labelled continue over the list is slices.Contains here, the same linear search.
func allPredecessorsBut(block, but *BasicBlock, list []*BasicBlock) []*BasicBlock {
	gathered := list
	for _, predecessor := range block.Predecessors {
		if predecessor == but || slices.Contains(gathered, predecessor) {
			continue
		}
		gathered = append(gathered, predecessor)
		gathered = allPredecessorsBut(predecessor, but, gathered)
	}
	return gathered
}
