// Post-dominance, and the set of blocks that must execute on the way out of a function.
//
// This is a port of React's `Dominator.ts` and `ComputeUnconditionalBlocks.ts`, transcribed by oxc
// at `oxc_react_compiler/src/react_compiler_hir/dominator.rs`. Cooper, Harvey and Kennedy, "A
// Simple, Fast Dominance Algorithm", run over the REVERSED graph with one synthetic exit.
//
// # Why this is here rather than borrowed from internal/utils/controlflow
//
// `controlflow` has both pieces and neither is reachable. `AnalyzeDominators` is generic over
// `controlflow.Graph[E]` and `controlflow.Block[E]`, whose `index`, `final` and `thrown` fields are
// unexported and set only by `controlflow.Build`, so a caller holding a `hir.Function` cannot
// construct one. `PathAnalysis.IsOnEveryFinalPath` computes semantically the right thing over the
// right kind of graph, and is likewise only obtainable from a graph this package does not produce.
// `ssa.go`'s header states the general form of this: nothing in that package applies to this graph
// without a conversion that does not exist.
//
// It is also NOT the same question `ssa_verify.go`'s `computeDominance` answers, and the difference
// is the whole point rather than an implementation detail. That one is forward dominance: does
// every path from the entry REACH this block. This is post-dominance: does every path from the
// entry PASS THROUGH this block on its way to a return.
//
// Measured on React's own rule, which is why the distinction is recorded rather than asserted:
//
//	function Component(props) {
//	  const [x, setX] = useState(0);
//	  if (props.c) { return null; }
//	  setX(1);          // clean upstream
//	  return x;
//	}
//
// The entry block dominates the block holding `setX(1)`, so a forward-dominance test marks it
// unconditional and reports. It does not post-dominate the entry, because the early return leaves
// by another path, and upstream is silent. Reusing `computeDominance` here would have produced a
// false positive that no fixture built from the imported corpus contains, since every upstream
// invalid case puts the call on a path that both tests agree about.
package hir

// UnconditionalBlocks returns the blocks that execute on every non-throwing path through function.
//
// This is upstream's `computeUnconditionalBlocks`: walk the post-dominator chain from the entry
// block to the synthetic exit, collecting every real block on the way. A block on that chain lies
// on every path from the entry to a return, so an instruction in it runs unconditionally.
//
// # Throwing paths are excluded, deliberately, and it is observable
//
// The synthetic exit is fed by `Return` terminals only, never by `Throw`. Upstream passes
// `includeThrowsAsExitNode: false` from this caller and true from others, and the choice decides a
// real fixture:
//
//	for (const _ of props) {
//	  if (props.cond) { break; } else { throw new Error('bye!'); }
//	}
//	setState(true);     // REPORTS upstream
//
// If a throw counted as an exit, the throwing arm would be a second way out and the code after the
// loop would stop post-dominating the entry. Because it does not count, the only way to leave is
// through the `break`, so the call after the loop is unconditional and reports. Measured on React's
// rule, which reports it, and it is the reason this parameter is fixed here rather than exposed:
// no caller in this tree wants the other answer, and a flag nothing sets is a flag nothing tests.
//
// A block that cannot reach any return at all is not on the chain and is therefore never
// unconditional, which is the right answer: an infinite loop's body runs, but nothing after it does.
//
// The result is nil for a nil function and for one with no blocks. Membership is the only supported
// question; the ordering of the underlying walk is not exposed because it carries no meaning.
func UnconditionalBlocks(function *Function) map[BlockId]bool {
	if function == nil || len(function.Blocks) == 0 {
		return nil
	}

	tree := computePostDominance(function)
	unconditional := make(map[BlockId]bool, len(function.Blocks))

	// Upstream asserts against re-entering a block, calling it a non-terminating loop. A cycle
	// cannot occur in a post-dominator tree, which is a tree by construction, but the walk is
	// bounded anyway rather than trusting that: this runs over lowered user code, and a panic in a
	// linter is worse than a conservative answer.
	current := function.Entry
	for step := 0; step <= len(function.Blocks); step++ {
		if current == postDominatorExit || unconditional[current] {
			break
		}
		unconditional[current] = true
		next, ok := tree.immediate[current]
		if !ok {
			break
		}
		current = next
	}
	return unconditional
}

// postDominatorExit is the synthetic block every return flows into.
//
// `InvalidBlock` is zero and lowering never mints block zero, so zero is free to mean the exit here
// and cannot collide with a real id.
const postDominatorExit BlockId = InvalidBlock

// postDominanceTree holds the immediate post-dominator of each block.
//
// A block absent from `immediate` reaches no return, so it post-dominates nothing and nothing
// reports it. That is a real state rather than an error: a function whose every path throws has one.
type postDominanceTree struct {
	immediate map[BlockId]BlockId
}

// computePostDominance runs Cooper-Harvey-Kennedy over the reversed graph.
//
// The algorithm needs its nodes in reverse postorder OF THE GRAPH IT WALKS, and the graph it walks
// here is the reverse of `Function.Blocks`' order rather than that order itself. So the traversal
// below computes its own ordering from the exit backwards, instead of reusing `Function.Blocks`,
// which is in reverse postorder of the FORWARD graph and is the wrong order for this.
//
// Getting that wrong does not fail loudly. Cooper-Harvey-Kennedy's `intersect` walks two pointers
// toward each other by comparing ordering positions, so a wrong order makes it walk the wrong way
// and either loop or settle on a node that does not dominate either input. The result is a
// plausible tree that is quietly incorrect, which is why the order is derived here rather than
// borrowed.
func computePostDominance(function *Function) *postDominanceTree {
	// Successors in the reversed graph are predecessors in the forward one, and vice versa. The
	// exit is a successor of every returning block.
	reverseSuccessors := make(map[BlockId][]BlockId, len(function.Blocks)+1)
	for _, block := range function.Blocks {
		if _, isReturn := block.Terminal.(*Return); isReturn {
			reverseSuccessors[postDominatorExit] = append(reverseSuccessors[postDominatorExit], block.Id)
		}
		for _, predecessorId := range block.Predecessors {
			reverseSuccessors[block.Id] = append(reverseSuccessors[block.Id], predecessorId)
		}
	}

	order := reversePostorderFrom(postDominatorExit, reverseSuccessors)
	position := make(map[BlockId]int, len(order))
	for index, id := range order {
		position[id] = index
	}

	immediate := make([]int, len(order))
	for index := range immediate {
		immediate[index] = -1
	}
	tree := &postDominanceTree{immediate: make(map[BlockId]BlockId, len(order))}
	if len(order) == 0 {
		return tree
	}
	immediate[0] = 0

	// The two pointers walk toward the root, which sits at position zero, so the larger position is
	// always the one further from it and is the one that steps.
	intersect := func(a, b int) int {
		for a != b {
			for a > b {
				if immediate[a] == -1 || immediate[a] == a {
					return b
				}
				a = immediate[a]
			}
			for b > a {
				if immediate[b] == -1 || immediate[b] == b {
					return a
				}
				b = immediate[b]
			}
		}
		return a
	}

	// Predecessors in the reversed graph, which the fixed point reads.
	reversePredecessors := make(map[BlockId][]BlockId, len(order))
	for from, targets := range reverseSuccessors {
		for _, to := range targets {
			reversePredecessors[to] = append(reversePredecessors[to], from)
		}
	}

	for changed := true; changed; {
		changed = false
		for index := 1; index < len(order); index++ {
			newImmediate := -1
			for _, predecessorId := range reversePredecessors[order[index]] {
				predecessorIndex, known := position[predecessorId]
				if !known || immediate[predecessorIndex] == -1 {
					continue
				}
				if newImmediate == -1 {
					newImmediate = predecessorIndex
				} else {
					newImmediate = intersect(predecessorIndex, newImmediate)
				}
			}
			if newImmediate != -1 && immediate[index] != newImmediate {
				immediate[index] = newImmediate
				changed = true
			}
		}
	}

	for index, id := range order {
		if index == 0 || immediate[index] == -1 {
			continue
		}
		tree.immediate[id] = order[immediate[index]]
	}
	return tree
}

// reversePostorderFrom returns the nodes reachable from root, in reverse postorder.
//
// Iterative rather than recursive for the same reason `ReversePostorder` is: the depth is whatever
// the source contains, and a linter must not be bounded by the goroutine stack.
func reversePostorderFrom(root BlockId, successors map[BlockId][]BlockId) []BlockId {
	visited := make(map[BlockId]bool, len(successors))
	postorder := make([]BlockId, 0, len(successors))

	type frame struct {
		id   BlockId
		next int
	}
	stack := []*frame{{id: root}}
	visited[root] = true

	for len(stack) > 0 {
		top := stack[len(stack)-1]
		children := successors[top.id]
		if top.next < len(children) {
			child := children[top.next]
			top.next++
			if !visited[child] {
				visited[child] = true
				stack = append(stack, &frame{id: child})
			}
			continue
		}
		postorder = append(postorder, top.id)
		stack = stack[:len(stack)-1]
	}

	for left, right := 0, len(postorder)-1; left < right; left, right = left+1, right-1 {
		postorder[left], postorder[right] = postorder[right], postorder[left]
	}
	return postorder
}
