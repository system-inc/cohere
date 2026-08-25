// Graph maintenance: the passes lowering runs to bring a freshly built function into the state
// every other pass assumes.
//
// Three invariants are established here and every consumer may rely on them:
//
//  1. Function.Blocks is in reverse postorder, and unreachable blocks have been removed.
//  2. Every block's Predecessors is exactly the set of blocks with a real edge into it.
//  3. Every instruction and terminal has a nonzero, monotonically increasing EvaluationOrder.
//
// They are re-established by calling Finalize, which any pass that restructures the graph must do.
package hir

// Finalize brings a freshly lowered function into the state every pass assumes.
//
// Order matters: reverse postorder first, because it drops unreachable blocks and the predecessor
// edges must not name a block that is gone; evaluation order last, because it walks the blocks in
// their final order.
func Finalize(function *Function) {
	ReversePostorder(function)
	MarkPredecessors(function)
	MarkEvaluationOrder(function)
	for _, nested := range function.Functions {
		Finalize(nested)
	}
}

// ReversePostorder reorders Function.Blocks into reverse postorder and drops unreachable blocks.
//
// # Why unreachable blocks are dropped here and kept in controlflow
//
// `controlflow` keeps them, because a rule may want to ask what would have run after a `return`.
// This drops them, because a value defined only in a block control never reaches is a value no
// analysis should see: single-assignment construction over an unreachable definition produces a phi
// operand from a predecessor that cannot execute, and every effect derived from it is fiction.
//
// A pass wanting the unreachable code should read the AST, or the other graph. This one is for
// reasoning about what runs.
//
// The traversal follows real edges only, plus fallthroughs, because a construct's fallthrough is
// reachable through its arms and a walk that omitted it would drop live blocks whenever an arm's
// terminal names the fallthrough only structurally.
func ReversePostorder(function *Function) {
	postorder := make([]*BasicBlock, 0, len(function.Blocks))
	visited := make(map[BlockId]bool, len(function.Blocks))

	// Iterative rather than recursive: a deeply nested function would otherwise be bounded by the
	// goroutine stack, and lowering is called on whatever the source contains.
	type frame struct {
		block      *BasicBlock
		successors []BlockId
		next       int
	}
	entry, ok := function.Block(function.Entry)
	if !ok {
		return
	}
	stack := []*frame{{block: entry, successors: successorList(entry)}}
	visited[entry.Id] = true

	for len(stack) > 0 {
		top := stack[len(stack)-1]
		if top.next == len(top.successors) {
			postorder = append(postorder, top.block)
			stack = stack[:len(stack)-1]
			continue
		}
		successorId := top.successors[top.next]
		top.next++
		if visited[successorId] {
			continue
		}
		successor, ok := function.Block(successorId)
		if !ok {
			continue
		}
		visited[successorId] = true
		stack = append(stack, &frame{block: successor, successors: successorList(successor)})
	}

	blocks := make([]*BasicBlock, 0, len(postorder))
	for index := len(postorder) - 1; index >= 0; index-- {
		blocks = append(blocks, postorder[index])
	}
	function.Blocks = blocks

	for id := range function.blocksById {
		if !visited[id] {
			delete(function.blocksById, id)
		}
	}
}

func successorList(block *BasicBlock) []BlockId {
	var successors []BlockId
	EachSuccessorAndFallthrough(block.Terminal, func(id BlockId) {
		successors = append(successors, id)
	})
	return successors
}

// MarkPredecessors recomputes every block's Predecessors from the real edges.
//
// Fallthroughs are NOT edges and do not produce a predecessor. See EachSuccessor.
//
// This is the field single-assignment construction needs: Braun's algorithm reads a value at a
// block join by asking each predecessor what it holds, and a missing or spurious predecessor is a
// wrong phi rather than a crash.
func MarkPredecessors(function *Function) {
	for _, block := range function.Blocks {
		block.Predecessors = block.Predecessors[:0]
	}
	for _, block := range function.Blocks {
		seen := map[BlockId]bool{}
		EachSuccessor(block.Terminal, func(id BlockId) {
			if seen[id] {
				return
			}
			seen[id] = true
			successor, ok := function.Block(id)
			if !ok {
				return
			}
			successor.Predecessors = append(successor.Predecessors, block.Id)
		})
	}
}

// MarkEvaluationOrder assigns each instruction and terminal its position in evaluation order.
//
// Numbering follows Function.Blocks, which is reverse postorder, so a forward analysis sees
// increasing numbers along any acyclic path. Across a back edge the number decreases, which is the
// correct and expected signal that a loop was traversed.
//
// Starts at 1: zero means unassigned, and a pass that reads an order of zero has found a block the
// finalizer did not reach.
func MarkEvaluationOrder(function *Function) {
	order := EvaluationOrder(1)
	for _, block := range function.Blocks {
		for _, instructionId := range block.Instructions {
			function.Instructions[instructionId].Order = order
			order++
		}
		setTerminalOrder(block.Terminal, order)
		order++
	}
}

func setTerminalOrder(terminal Terminal, order EvaluationOrder) {
	switch t := terminal.(type) {
	case *Return:
		t.Order = order
	case *Throw:
		t.Order = order
	case *Unreachable:
		t.Order = order
	case *Unsupported:
		t.Order = order
	case *Goto:
		t.Order = order
	case *If:
		t.Order = order
	case *Branch:
		t.Order = order
	case *Switch:
		t.Order = order
	case *While:
		t.Order = order
	case *DoWhile:
		t.Order = order
	case *For:
		t.Order = order
	case *ForOf:
		t.Order = order
	case *ForIn:
		t.Order = order
	case *Logical:
		t.Order = order
	case *Ternary:
		t.Order = order
	case *Optional:
		t.Order = order
	case *Sequence:
		t.Order = order
	case *Label:
		t.Order = order
	case *Try:
		t.Order = order
	case *MaybeThrow:
		t.Order = order
	case *Scope:
		t.Order = order
	}
}
