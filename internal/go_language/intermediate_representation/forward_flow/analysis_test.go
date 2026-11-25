package forward_flow

import (
	"math/rand"
	"testing"
)

// The cases of upstream's dfa/dense forward_test.go, ported for the solver they test (honnef.co/go/tools@v0.8.1). Each
// holds the fact on entry to every node, which is all this port keeps; upstream's checks of the fact on each edge are
// left out with its Edge, which return_nil_ness never reads.

// requireIns fails test for each node whose fact on entry is not the one wanted.
func requireIns(test *testing.T, analysis *Analysis[nodeSet], wanted map[int]nodeSet) {
	test.Helper()
	for node, want := range wanted {
		if got := analysis.In(node); got != want {
			test.Errorf("In(%d): got %v, want %v", node, got, want)
		}
	}
}

// addsItsSource is the transfer function of the shape tests: each edge adds the node it leaves to the set.
func addsItsSource(from int, _ int, fact nodeSet) nodeSet {
	return fact | 1<<uint(from)
}

// A chain of four nodes: each one's fact holds every node before it.
func TestForwardCarriesAFactAlongAChain(test *testing.T) {
	test.Parallel()
	graph := &simpleGraph{nodeCount: 4, edges: [][]int{0: {1}, 1: {2}, 2: {3}, 3: {}}}
	analysis := Forward[nodeSetUnion](graph, nil, addsItsSource)
	requireIns(test, analysis, map[int]nodeSet{0: 0, 1: setOf(0), 2: setOf(0, 1), 3: setOf(0, 1, 2)})
}

// A diamond: the join merges the facts of both arms.
func TestForwardMergesADiamondsArms(test *testing.T) {
	test.Parallel()
	graph := &simpleGraph{nodeCount: 4, edges: [][]int{0: {1, 2}, 1: {3}, 2: {3}, 3: {}}}
	analysis := Forward[nodeSetUnion](graph, nil, addsItsSource)
	requireIns(test, analysis, map[int]nodeSet{0: 0, 1: setOf(0), 2: setOf(0), 3: setOf(0, 1, 2)})
}

// A cycle, 0 -> 1 -> 2 -> 1: the fact on the edge back reaches the loop's head, so both nodes of the cycle hold all
// three.
func TestForwardReachesAFixedPointAroundACycle(test *testing.T) {
	test.Parallel()
	graph := &simpleGraph{nodeCount: 3, edges: [][]int{0: {1}, 1: {2}, 2: {1}}}
	analysis := Forward[nodeSetUnion](graph, nil, addsItsSource)
	requireIns(test, analysis, map[int]nodeSet{0: 0, 1: setOf(0, 1, 2), 2: setOf(0, 1, 2)})
}

// A loop whose head has 32 arms, each an edge back to the head adding its own bit. Visiting the head as soon as each
// edge back changes it would run the loop's body a quadratic number of times; deferring it to the next sweep coalesces
// the updates, so the head's edges are transferred in at most two whole sweeps.
func TestForwardCoalescesTheEdgesBackIntoTheNextSweep(test *testing.T) {
	test.Parallel()
	const arms = 32
	const entry = 0
	const header = 1
	const firstArm = 2
	edges := make([][]int, 2+arms)
	edges[entry] = []int{header}
	for arm := range arms {
		edges[header] = append(edges[header], firstArm+arm)
		edges[firstArm+arm] = []int{header}
	}
	graph := &simpleGraph{nodeCount: len(edges), edges: edges}

	transfers := 0
	headerEdgeTransfers := 0
	transfer := func(from int, _ int, in nodeSet) nodeSet {
		transfers++
		if from == header {
			headerEdgeTransfers++
		}
		if from >= firstArm {
			return in | setOf(from-firstArm)
		}
		return in
	}

	analysis := Forward[nodeSetUnion](graph, nil, transfer)
	if want := nodeSet((uint64(1) << arms) - 1); analysis.In(header) != want {
		test.Fatalf("the header's fact: got %064b, want %064b", analysis.In(header), want)
	}
	// The header has one edge out for each arm, so this bounds its transfers to two whole sweeps.
	if limit := 2 * arms; headerEdgeTransfers > limit {
		test.Errorf("the header's edges were transferred %d times, want at most %d", headerEdgeTransfers, limit)
	}
	if limit := 4*arms + 1; transfers > limit {
		test.Errorf("got %d transfers in all, want at most %d", transfers, limit)
	}
}

// A spine of 64 nodes, each with an edge to the next and one to a bottleneck, which heads a tail of 62 more, the node
// numbers shuffled so their order is no help. Visited in reverse postorder, the bottleneck is visited once all the
// spine is, so every edge is transferred exactly once; a naive order would run the tail again for each spine node.
func TestForwardTransfersEachEdgeOnceInReversePostorder(test *testing.T) {
	test.Parallel()
	const spine = 64
	const nodeCount = 2 * spine

	// A fixed seed, as upstream's, so the shuffle is the same every run.
	permutation := rand.New(rand.NewSource(42)).Perm(nodeCount)
	logical := make([]int, nodeCount)
	for place, node := range permutation {
		logical[node] = place
	}

	edges := make([][]int, nodeCount)
	edgeCount := 0
	addEdge := func(from int, to int) {
		edges[permutation[from]] = append(edges[permutation[from]], permutation[to])
		edgeCount++
	}
	for node := range spine {
		if node+1 != spine {
			addEdge(node, node+1)
		}
		addEdge(node, spine)
	}
	for node := spine; node < nodeCount-2; node++ {
		addEdge(node, node+1)
	}
	graph := &simpleGraph{nodeCount: nodeCount, edges: edges}

	transfers := 0
	transfer := func(from int, _ int, in nodeSet) nodeSet {
		transfers++
		if logical[from] < spine {
			return in | 1<<uint64(logical[from])
		}
		return in
	}

	Forward[nodeSetUnion](graph, nil, transfer)
	if transfers != edgeCount {
		test.Errorf("got %d transfers, want %d, one for each edge", transfers, edgeCount)
	}
}
