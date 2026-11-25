package control_flow_graph

// cyclePaths mirrors the traversal and caching order used by ESLint's
// countPathsFromStart/countPathsToEnd helpers. That order is observable: once
// a shared successor is cached, a later switch clause may no longer walk the
// back edge that marked an earlier clause as cyclic.
type cyclePaths[E any] struct {
	graph  *Graph[E]
	blocks []pathBlockState
	state  []cycleBlockState
	path   []int
}

func (paths *cyclePaths[E]) countToEnd(index int) bool {
	return paths.count(index, -1)
}

func (paths *cyclePaths[E]) count(index int, barrierStart int) bool {
	if active := paths.state[index].activeAt; active >= 0 {
		// ESLint records the segment after its loop context as the start of the
		// cycle, then marks only the later segments in pathArray. Excluding the
		// repeated header here gives the same observable result: a for-loop test
		// is not cyclic, while its body and incrementor are.
		cycleStart := int(active) + 1
		cycleEnd := len(paths.path)
		if barrierStart > int(active) {
			cycleEnd = barrierStart
		}
		for _, member := range paths.path[cycleStart:cycleEnd] {
			paths.blocks[member].flags |= pathCyclic
		}
		return false
	}
	if paths.state[index].flags&cycleToKnown != 0 {
		return paths.state[index].flags&cycleToEnd != 0
	}

	paths.state[index].activeAt = int32(len(paths.path))
	paths.path = append(paths.path, index)
	hasPath := false
	// An uncaught throw is not a path to a normal exit. A yield is different:
	// it also has a reachable resumption successor, which must still be walked
	// to discover a loop back edge.
	block := paths.graph.Blocks[index]
	traverse := paths.state[index].flags&cycleThrown == 0
	if !traverse {
		for _, successor := range block.Successors {
			if successor != nil && successor.Reachable {
				traverse = true
				break
			}
		}
	}
	if traverse {
		hasPath = paths.blocks[index].flags&pathFinal != 0
		hasSuccessor := false
		for successorIndex, successor := range block.Successors {
			if successor == nil || !successor.Reachable {
				continue
			}
			hasSuccessor = true
			nextBarrier := barrierStart
			if successorIndex < len(block.cycleBarrier) && block.cycleBarrier[successorIndex] {
				nextBarrier = len(paths.path)
			}
			if paths.count(successor.Index(), nextBarrier) {
				hasPath = true
			}
		}
		if !hasSuccessor && paths.state[index].flags&cycleThrown == 0 {
			hasPath = true
		}
	}
	paths.path = paths.path[:len(paths.path)-1]
	paths.state[index].activeAt = -1

	if hasPath || !paths.graph.Blocks[index].Reachable {
		paths.state[index].flags |= cycleToKnown
		if hasPath {
			paths.state[index].flags |= cycleToEnd
		} else {
			paths.state[index].flags &^= cycleToEnd
		}
	} else {
		paths.state[index].flags &^= cycleToKnown
	}
	return hasPath
}
