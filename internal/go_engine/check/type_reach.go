package check

import (
	"go/types"
	"math/bits"
	"sync"
)

// typeReach is the packages each type name's closure reaches, a set of bits by package (packageBits), memoized for one
// build's graph (typeGraph) as dependence sets ask it: the type name's own package, and what every type name its node
// points to reaches, its edges and the type names of each type it observes, read whole, so the set and the closure
// digest follow the same nodes. A strongly connected component of the graph reaches one set, gathered once by
// Tarjan's walk, so a dependence set asked of a thousand packages renders and walks each node once, where a walk of
// each package's own reach walked the shared types again for every one (#qbvtkfm). A dependence set is asked of the
// host's memo by builds side by side, so the whole of one ask is under lock, and the memo's sets are never changed
// once stored.
type typeReach struct {
	lock sync.Mutex

	// indexOf is each package's bit, by path, and paths the paths by bit.
	indexOf map[string]int
	paths   []string

	// reached is each type name's set, stored once its component is closed.
	reached map[*types.TypeName]packageBits

	// index, lowest, onStack, gathered and stack are one Tarjan's walk's state (connect), empty between walks.
	index    map[*types.TypeName]int
	lowest   map[*types.TypeName]int
	onStack  map[*types.TypeName]bool
	gathered map[*types.TypeName]packageBits
	stack    []*types.TypeName
}

// packageBits is a set of packages, one bit each, by typeReach's indexOf.
type packageBits []uint64

// reachOf is the packages every one of roots reaches, each root's closure walked once for the build (typeReach), with
// graph's nodes rendered under sizes, the build's.
func (reach *typeReach) reachOf(graph *typeGraph, roots []*types.TypeName, sizes types.Sizes) packageBits {
	reach.lock.Lock()
	defer reach.lock.Unlock()
	if reach.reached == nil {
		reach.indexOf, reach.reached = map[string]int{}, map[*types.TypeName]packageBits{}
	}
	var united packageBits
	for _, root := range roots {
		if _, known := reach.reached[root]; !known {
			reach.connect(graph, root, sizes)
		}
		united = unitedBits(united, reach.reached[root])
	}
	return united
}

// connect stores the set of root and of every type name it reaches that has none yet, by Tarjan's walk: each node's
// own package with what its successors reach, those of its own component gathered when the component closes, every
// member of which then has the component's set. The walk's state is the memo's, cleared after each walk, so the walks
// of a thousand packages' roots allocate it once. The caller holds the lock.
func (reach *typeReach) connect(graph *typeGraph, root *types.TypeName, sizes types.Sizes) {
	if reach.index == nil {
		reach.index, reach.lowest = map[*types.TypeName]int{}, map[*types.TypeName]int{}
		reach.onStack, reach.gathered = map[*types.TypeName]bool{}, map[*types.TypeName]packageBits{}
	}
	defer func() {
		clear(reach.index)
		clear(reach.lowest)
		clear(reach.onStack)
		clear(reach.gathered)
		reach.stack = reach.stack[:0]
	}()
	var visit func(current *types.TypeName)
	visit = func(current *types.TypeName) {
		reach.index[current], reach.lowest[current] = len(reach.index), len(reach.index)
		reach.stack = append(reach.stack, current)
		reach.onStack[current] = true
		own := reach.withPackage(nil, current.Pkg())
		// own is this node's alone until it is gathered, so each successor's set is added into it in place.
		addInto := func(known packageBits) {
			if len(known) > len(own) {
				own = append(own, make(packageBits, len(known)-len(own))...)
			}
			for word, value := range known {
				own[word] |= value
			}
		}
		node := graph.renderedNode(current, sizes)
		for _, successor := range withObserved(append([]*types.TypeName(nil), node.edges...), node.observedTypes) {
			if known, isKnown := reach.reached[successor]; isKnown {
				addInto(known)
				continue
			}
			if _, seen := reach.index[successor]; !seen {
				visit(successor)
				reach.lowest[current] = min(reach.lowest[current], reach.lowest[successor])
				if known, isKnown := reach.reached[successor]; isKnown {
					addInto(known)
				}
				continue
			}
			if reach.onStack[successor] {
				reach.lowest[current] = min(reach.lowest[current], reach.index[successor])
			}
		}
		reach.gathered[current] = own
		if reach.lowest[current] != reach.index[current] {
			return
		}
		// A component of one, the most common, has its own set already.
		if top := reach.stack[len(reach.stack)-1]; top == current {
			reach.stack = reach.stack[:len(reach.stack)-1]
			reach.onStack[current] = false
			reach.reached[current] = own
			return
		}
		var component []*types.TypeName
		var united packageBits
		for {
			member := reach.stack[len(reach.stack)-1]
			reach.stack = reach.stack[:len(reach.stack)-1]
			reach.onStack[member] = false
			component = append(component, member)
			united = unitedBits(united, reach.gathered[member])
			if member == current {
				break
			}
		}
		for _, member := range component {
			reach.reached[member] = united
		}
	}
	visit(root)
}

// withPackage is set with reached's bit, a set of its own; the universe, of no package, has none. The caller holds the
// lock.
func (reach *typeReach) withPackage(set packageBits, reached *types.Package) packageBits {
	if reached == nil {
		return set
	}
	bit, known := reach.indexOf[reached.Path()]
	if !known {
		bit = len(reach.paths)
		reach.indexOf[reached.Path()] = bit
		reach.paths = append(reach.paths, reached.Path())
	}
	grown := make(packageBits, max(len(set), bit/64+1))
	copy(grown, set)
	grown[bit/64] |= 1 << (bit % 64)
	return grown
}

// pathsOf are the paths of set's packages.
func (reach *typeReach) pathsOf(set packageBits) []string {
	reach.lock.Lock()
	defer reach.lock.Unlock()
	var paths []string
	for word, value := range set {
		for remaining := value; remaining != 0; remaining &= remaining - 1 {
			paths = append(paths, reach.paths[word*64+bits.TrailingZeros64(remaining)])
		}
	}
	return paths
}

// unitedBits is the union of left and right, a set of its own, or left itself when right adds nothing.
func unitedBits(left packageBits, right packageBits) packageBits {
	if len(right) == 0 {
		return left
	}
	united := make(packageBits, max(len(left), len(right)))
	copy(united, left)
	for word, value := range right {
		united[word] |= value
	}
	return united
}
