package control_flow_graph

// PathAnalysis answers graph-shape questions shared by rules that consume an
// ESLint-style code path. Thrown exits are deliberately excluded from final
// paths, matching ESLint analyses that treat thrown segments separately.
//
// All per-block state is indexed by Block.Index, so ordinary queries do not
// allocate or hash block pointers.
type PathAnalysis[E any] struct {
	graph        *Graph[E]
	blocks       []pathBlockState
	cyclePaths   *cyclePaths[E]
	shortestExit int
	finalPaths   uint8
	hasFinal     bool
	hasExit      bool
}

type pathBlockState struct {
	shortest int32
	flags    uint8
	paths    uint8
}

const (
	pathFinal uint8 = 1 << iota
	pathThrown
	pathExit
	pathCyclic
	pathOnEveryFinal
	pathCounting
	pathCountKnown
)

// AnalyzePaths computes reachability-derived path information for graph.
func AnalyzePaths[E any](graph *Graph[E]) *PathAnalysis[E] {
	if graph == nil || len(graph.Blocks) == 0 {
		return &PathAnalysis[E]{}
	}

	blockCount := len(graph.Blocks)
	analysis := &PathAnalysis[E]{
		graph:  graph,
		blocks: make([]pathBlockState, blockCount),
	}
	for _, block := range graph.ThrownBlocks {
		if block != nil && block.Reachable {
			analysis.blocks[block.Index()].flags |= pathThrown | pathExit
			analysis.hasExit = true
		}
	}
	for _, block := range graph.FinalBlocks {
		if block != nil && block.Reachable {
			analysis.blocks[block.Index()].flags |= pathExit
			analysis.hasExit = true
			if analysis.blocks[block.Index()].flags&pathThrown != 0 {
				continue
			}
			analysis.blocks[block.Index()].flags |= pathFinal
			analysis.hasFinal = true
		}
	}

	analysis.cyclePaths = newCyclePaths(graph, analysis.blocks)
	analysis.cyclePaths.countToEnd(0)
	analysis.finalPaths = analysis.countFinalPaths(0)
	analysis.findShortestPaths(graph)
	analysis.findFinalPathDominators(graph)
	return analysis
}

// IsCyclic reports whether block belongs to a reachable graph cycle.
func (analysis *PathAnalysis[E]) IsCyclic(block *Block[E]) bool {
	if analysis == nil || block == nil || block.Index() >= len(analysis.blocks) {
		return false
	}
	if analysis.cyclePaths != nil {
		analysis.cyclePaths.countToEnd(block.Index())
	}
	return analysis.blocks[block.Index()].flags&pathCyclic != 0
}

// IsOnEveryFinalPath reports whether every non-throwing path from the graph
// entry to a final block passes through block. It is true when the graph has no
// non-throwing final path, matching the vacuous path-count comparison used by
// ESLint consumers.
func (analysis *PathAnalysis[E]) IsOnEveryFinalPath(block *Block[E]) bool {
	return analysis != nil &&
		block != nil &&
		block.Index() < len(analysis.blocks) &&
		analysis.blocks[block.Index()].flags&pathOnEveryFinal != 0
}

// IsExit reports whether block is a reachable final segment, including an
// uncaught thrown segment. ESLint includes thrown segments in codePath's
// finalSegments even though path counting deliberately assigns them zero
// paths.
func (analysis *PathAnalysis[E]) IsExit(block *Block[E]) bool {
	return analysis != nil &&
		block != nil &&
		block.Index() < len(analysis.blocks) &&
		analysis.blocks[block.Index()].flags&pathExit != 0
}

// ShortestPathFromStart returns the number of blocks on the shortest path from
// the graph entry to block. The second result is false for an unreachable or
// unknown block.
func (analysis *PathAnalysis[E]) ShortestPathFromStart(block *Block[E]) (int, bool) {
	if analysis == nil || block == nil || block.Index() >= len(analysis.blocks) {
		return 0, false
	}
	distance := analysis.blocks[block.Index()].shortest
	return int(distance), distance != 0
}

// ShortestExitPathFromStart returns the number of blocks on the shortest path
// from the graph entry to any reachable final segment, thrown exits included.
func (analysis *PathAnalysis[E]) ShortestExitPathFromStart() (int, bool) {
	if analysis == nil || !analysis.hasExit {
		return 0, false
	}
	return analysis.shortestExit, true
}

// HasSingleFinalPath reports whether exactly one non-thrown path reaches a
// final block. The count saturates at two because consumers asking about a
// detached ESLint code-path segment only need to distinguish zero, one, and
// multiple paths.
func (analysis *PathAnalysis[E]) HasSingleFinalPath() bool {
	return analysis != nil && analysis.finalPaths == 1
}

func (analysis *PathAnalysis[E]) countFinalPaths(index int) uint8 {
	state := &analysis.blocks[index]
	if state.flags&pathCounting != 0 {
		return 0
	}
	if state.flags&pathCountKnown != 0 {
		return state.paths
	}
	state.flags |= pathCounting
	count := uint8(0)
	if state.flags&pathThrown == 0 {
		if state.flags&pathFinal != 0 {
			count = 1
		} else {
			for _, successor := range analysis.graph.Blocks[index].Successors {
				if successor == nil || !successor.Reachable {
					continue
				}
				count += analysis.countFinalPaths(successor.Index())
				if count > 1 {
					count = 2
					break
				}
			}
		}
	}
	state.flags &^= pathCounting
	state.flags |= pathCountKnown
	state.paths = count
	return count
}

func (analysis *PathAnalysis[E]) findShortestPaths(graph *Graph[E]) {
	var queue []int
	if analysis.cyclePaths != nil && cap(analysis.cyclePaths.path) >= len(graph.Blocks) {
		queue = analysis.cyclePaths.path[:1]
	} else {
		queue = make([]int, 1, len(graph.Blocks))
	}
	queue[0] = 0
	analysis.blocks[0].shortest = 1
	for head := 0; head < len(queue); head++ {
		index := queue[head]
		distance := analysis.blocks[index].shortest + 1
		for _, successor := range graph.Blocks[index].Successors {
			if successor == nil || !successor.Reachable {
				continue
			}
			successorIndex := successor.Index()
			if analysis.blocks[successorIndex].shortest != 0 {
				continue
			}
			analysis.blocks[successorIndex].shortest = distance
			queue = append(queue, successorIndex)
		}
	}
	if analysis.cyclePaths != nil {
		analysis.cyclePaths.path = queue[:0]
	}

	for index := range analysis.blocks {
		distance := analysis.blocks[index].shortest
		if distance == 0 {
			continue
		}
		if analysis.blocks[index].flags&pathExit != 0 &&
			(analysis.shortestExit == 0 || int(distance) < analysis.shortestExit) {
			analysis.shortestExit = int(distance)
		}
	}
}

type cycleBlockState struct {
	activeAt int32
	flags    uint8
}

const (
	cycleThrown uint8 = 1 << iota
	cycleToKnown
	cycleToEnd
)

func newCyclePaths[E any](graph *Graph[E], blocks []pathBlockState) *cyclePaths[E] {
	blockCount := len(graph.Blocks)
	paths := &cyclePaths[E]{
		graph:  graph,
		blocks: blocks,
		state:  make([]cycleBlockState, blockCount),
		path:   make([]int, 0, blockCount),
	}
	for index := range paths.state {
		paths.state[index].activeAt = -1
	}
	for _, block := range graph.ThrownBlocks {
		if block != nil && block.Reachable {
			paths.state[block.Index()].flags |= cycleThrown
		}
	}
	return paths
}

// findFinalPathDominators adds one synthetic exit reached by every final block
// and computes immediate dominators with the Cooper-Harvey-Kennedy algorithm.
// The real blocks on the synthetic exit's dominator chain are exactly those on
// every non-throwing final path.
func (analysis *PathAnalysis[E]) findFinalPathDominators(graph *Graph[E]) {
	blockCount := len(graph.Blocks)
	if !analysis.hasFinal {
		for index, block := range graph.Blocks {
			if block.Reachable {
				analysis.blocks[index].flags |= pathOnEveryFinal
			}
		}
		return
	}

	exit := blockCount
	predCounts := make([]int, blockCount+2)
	for index, block := range graph.Blocks {
		if !block.Reachable {
			continue
		}
		for _, successor := range block.Successors {
			if successor != nil && successor.Reachable {
				predCounts[successor.Index()+1]++
			}
		}
		if analysis.blocks[index].flags&pathFinal != 0 {
			predCounts[exit+1]++
		}
	}
	for index := 1; index < len(predCounts); index++ {
		predCounts[index] += predCounts[index-1]
	}
	totalPredecessors := predCounts[len(predCounts)-1]
	nodeCount := blockCount + 1
	work := make([]int, totalPredecessors+nodeCount*4)
	predecessors := work[:totalPredecessors]
	predCursor := work[totalPredecessors : totalPredecessors+nodeCount]
	postOrderStorage := work[totalPredecessors+nodeCount : totalPredecessors+nodeCount*2]
	rpoPosition := work[totalPredecessors+nodeCount*2 : totalPredecessors+nodeCount*3]
	immediate := work[totalPredecessors+nodeCount*3:]
	copy(predCursor, predCounts[:nodeCount])
	for index, block := range graph.Blocks {
		if !block.Reachable {
			continue
		}
		for _, successor := range block.Successors {
			if successor == nil || !successor.Reachable {
				continue
			}
			successorIndex := successor.Index()
			predecessors[predCursor[successorIndex]] = index
			predCursor[successorIndex]++
		}
		if analysis.blocks[index].flags&pathFinal != 0 {
			predecessors[predCursor[exit]] = index
			predCursor[exit]++
		}
	}

	for index := range rpoPosition {
		rpoPosition[index] = -1
		immediate[index] = -1
	}
	postOrder := postOrderStorage[:0]
	var visit func(int)
	visit = func(index int) {
		rpoPosition[index] = 0
		if index < blockCount {
			for _, successor := range graph.Blocks[index].Successors {
				if successor != nil && successor.Reachable && rpoPosition[successor.Index()] == -1 {
					visit(successor.Index())
				}
			}
			if analysis.blocks[index].flags&pathFinal != 0 && rpoPosition[exit] == -1 {
				visit(exit)
			}
		}
		postOrder = append(postOrder, index)
	}
	visit(0)

	for left, right := 0, len(postOrder)-1; left < right; left, right = left+1, right-1 {
		postOrder[left], postOrder[right] = postOrder[right], postOrder[left]
	}
	for index, block := range postOrder {
		rpoPosition[block] = index
	}
	immediate[0] = 0
	intersect := func(left int, right int) int {
		// Each finger climbs the dominator tree from its own block until the two meet.
		leftFinger := left
		rightFinger := right
		for leftFinger != rightFinger {
			for rpoPosition[leftFinger] > rpoPosition[rightFinger] {
				leftFinger = immediate[leftFinger]
			}
			for rpoPosition[rightFinger] > rpoPosition[leftFinger] {
				rightFinger = immediate[rightFinger]
			}
		}
		return leftFinger
	}

	changed := true
	for changed {
		changed = false
		for _, index := range postOrder[1:] {
			start := predCounts[index]
			end := predCounts[index+1]
			newImmediate := -1
			for _, predecessor := range predecessors[start:end] {
				if immediate[predecessor] == -1 {
					continue
				}
				if newImmediate == -1 {
					newImmediate = predecessor
				} else {
					newImmediate = intersect(predecessor, newImmediate)
				}
			}
			if immediate[index] != newImmediate {
				immediate[index] = newImmediate
				changed = true
			}
		}
	}

	for index := immediate[exit]; index >= 0; index = immediate[index] {
		analysis.blocks[index].flags |= pathOnEveryFinal
		if index == 0 {
			break
		}
	}
}
