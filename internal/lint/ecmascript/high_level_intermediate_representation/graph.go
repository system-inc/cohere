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
//
// The passes themselves are static_single_assignment's, which this IR shares with Adamic's flow graph.
// What lives here is ssaGraph, this IR's side of that module's Graph: how it reads and writes a
// Function, a BasicBlock and a Place.
package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

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
// Upstream's `getReversePostorderedBlocks`, a structural fallthrough visited first so loop bodies and
// conditional arms precede their continuation, and a fallthrough nothing real reaches kept as an empty
// Unreachable block. static_single_assignment.ReversePostorder carries the reasoning.
func ReversePostorder(function *Function) {
	if function == nil {
		return
	}
	static_single_assignment.ReversePostorder(ssaGraph{}, function)
}

// MarkPredecessors recomputes every block's Predecessors from the real edges.
//
// Fallthroughs are not edges and do not produce a predecessor. See EachSuccessor.
//
// This is the field single-assignment construction needs: Braun's algorithm reads a value at a
// block join by asking each predecessor what it holds, and a missing or spurious predecessor is a
// wrong phi rather than a crash.
func MarkPredecessors(function *Function) {
	static_single_assignment.MarkPredecessors(ssaGraph{}, function)
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
	static_single_assignment.MarkEvaluationOrder(ssaGraph{}, function)
}

func setTerminalOrder(terminal Terminal, order static_single_assignment.EvaluationOrder) {
	switch typed := terminal.(type) {
	case *Return:
		typed.Order = order
	case *Throw:
		typed.Order = order
	case *Unreachable:
		typed.Order = order
	case *Unsupported:
		typed.Order = order
	case *Goto:
		typed.Order = order
	case *If:
		typed.Order = order
	case *Branch:
		typed.Order = order
	case *Switch:
		typed.Order = order
	case *While:
		typed.Order = order
	case *DoWhile:
		typed.Order = order
	case *For:
		typed.Order = order
	case *ForOf:
		typed.Order = order
	case *ForIn:
		typed.Order = order
	case *Logical:
		typed.Order = order
	case *Ternary:
		typed.Order = order
	case *Optional:
		typed.Order = order
	case *Sequence:
		typed.Order = order
	case *Label:
		typed.Order = order
	case *Try:
		typed.Order = order
	case *MaybeThrow:
		typed.Order = order
	case *Scope:
		typed.Order = order
	}
}

// ssaRole is the module's two roles from this IR's three: a receiver is read, as every pass here has
// always treated it.
func ssaRole(role PlaceRole) static_single_assignment.Role {
	if role == PlaceRoleDefine {
		return static_single_assignment.Define
	}
	return static_single_assignment.Use
}
