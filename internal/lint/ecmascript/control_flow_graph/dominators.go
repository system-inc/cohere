package control_flow_graph

// This file is a local addition, not vendored. See the change list in builder.go's header.
//
// It exposes two things the graph already computed and threw away: the predecessor edges that
// `findFinalPathDominators` built into a scratch array, and the immediate-dominator array that
// same function wrote and dropped after reading one chain out of it.
//
// # Why this is an addition rather than a rewrite of path_analysis.go
//
// `findFinalPathDominators` is a correct Cooper-Harvey-Kennedy implementation and it is load-
// bearing for `PathAnalysis.IsOnEveryFinalPath`, which `rules_of_hooks` consumes. Editing it to
// hand its array outward would put a second caller's requirements on a function whose current
// caller needs one boolean per block, and the two want different graphs: it dominates over a graph
// with one synthetic exit reached only by non-throwing final blocks, deliberately excluding thrown
// exits, because that is the question ESLint asks. A consumer asking "what dominates this block" in
// general does not want thrown paths excluded.
//
// So the algorithm is written a second time here over the real successor graph, and the two are
// pinned against each other by `TestDominatorsAgreeWithFinalPathDominators`. That test is the
// reason duplicating it is safe rather than a divergence waiting to happen: if either drifts, it
// goes red.

// AnalyzeDominators computes the dominator tree and predecessor edges of graph's reachable
// subgraph.
//
// Unreachable blocks are excluded entirely rather than given a dominator. There is no meaningful
// answer for them: a block control never arrives at is not dominated by anything, and treating the
// entry as its dominator would let a query about dead code come back with a confident true. Every
// accessor here answers false or nil for one, and `IsReachable` is the way to ask before believing
// a false.
func AnalyzeDominators[E any](graph *Graph[E]) *DominatorAnalysis[E] {
	if graph == nil || len(graph.Blocks) == 0 {
		return &DominatorAnalysis[E]{}
	}

	blockCount := len(graph.Blocks)
	analysis := &DominatorAnalysis[E]{
		graph:            graph,
		immediate:        make([]int, blockCount),
		rpoPosition:      make([]int, blockCount),
		predecessorStart: make([]int, blockCount+1),
	}

	// Counting sort over in-degree, so the predecessor groups land in one array.
	//
	// The entry block's own reachability is not read from Blocks[0].Reachable. Every other block's
	// is, but the entry is reachable by construction (`Build` sets it before laying anything out),
	// and reading the field here would make this depend on a field the builder sets rather than on
	// the definition.
	counts := make([]int, blockCount+1)
	for _, block := range graph.Blocks {
		if !block.Reachable {
			continue
		}
		for _, successor := range block.Successors {
			if successor != nil && successor.Reachable {
				counts[successor.Index()+1]++
			}
		}
	}
	for index := 1; index <= blockCount; index++ {
		counts[index] += counts[index-1]
	}
	copy(analysis.predecessorStart, counts)
	analysis.predecessors = make([]*Block[E], counts[blockCount])
	cursor := make([]int, blockCount)
	copy(cursor, counts[:blockCount])
	for _, block := range graph.Blocks {
		if !block.Reachable {
			continue
		}
		for _, successor := range block.Successors {
			if successor == nil || !successor.Reachable {
				continue
			}
			analysis.predecessors[cursor[successor.Index()]] = block
			cursor[successor.Index()]++
		}
	}

	for index := range analysis.immediate {
		analysis.immediate[index] = -1
		analysis.rpoPosition[index] = -1
	}

	// Depth-first postorder over reachable successors, then reversed.
	//
	// Written with an explicit stack rather than recursion. `findFinalPathDominators` recurses, and
	// on a deeply nested source file that is a stack the graph's depth controls; this runs over
	// every code path root in every file in the tree, so the depth is worth not trusting.
	postOrder := make([]*Block[E], 0, blockCount)
	type frame struct {
		block     *Block[E]
		successor int
	}
	visited := make([]bool, blockCount)
	stack := []frame{{block: graph.Blocks[0]}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.successor < len(top.block.Successors) {
			successor := top.block.Successors[top.successor]
			top.successor++
			if successor == nil || !successor.Reachable || visited[successor.Index()] {
				continue
			}
			visited[successor.Index()] = true
			stack = append(stack, frame{block: successor})
			continue
		}
		postOrder = append(postOrder, top.block)
		stack = stack[:len(stack)-1]
	}

	analysis.reversePostOrder = make([]*Block[E], len(postOrder))
	for index, block := range postOrder {
		reversed := len(postOrder) - 1 - index
		analysis.reversePostOrder[reversed] = block
		analysis.rpoPosition[block.Index()] = reversed
	}

	// Cooper-Harvey-Kennedy. The entry is its own immediate dominator, and every other reachable
	// block starts undefined and is refined by intersecting its already-processed predecessors
	// until nothing moves.
	analysis.immediate[0] = 0
	intersect := func(left, right int) int {
		// Each finger climbs the dominator tree from its own block until the two meet.
		leftFinger := left
		rightFinger := right
		for leftFinger != rightFinger {
			for analysis.rpoPosition[leftFinger] > analysis.rpoPosition[rightFinger] {
				leftFinger = analysis.immediate[leftFinger]
			}
			for analysis.rpoPosition[rightFinger] > analysis.rpoPosition[leftFinger] {
				rightFinger = analysis.immediate[rightFinger]
			}
		}
		return leftFinger
	}
	for changed := true; changed; {
		changed = false
		for _, block := range analysis.reversePostOrder[1:] {
			index := block.Index()
			newImmediate := -1
			for _, predecessor := range analysis.Predecessors(block) {
				predecessorIndex := predecessor.Index()
				if analysis.immediate[predecessorIndex] == -1 {
					continue
				}
				if newImmediate == -1 {
					newImmediate = predecessorIndex
				} else {
					newImmediate = intersect(predecessorIndex, newImmediate)
				}
			}
			if newImmediate != -1 && analysis.immediate[index] != newImmediate {
				analysis.immediate[index] = newImmediate
				changed = true
			}
		}
	}

	return analysis
}
