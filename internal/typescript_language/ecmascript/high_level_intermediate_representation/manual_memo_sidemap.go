package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// manualMemoSidemap is what a single forward walk learns about the temporaries in a function.
//
// Upstream calls this `IdentifierSidemap` and builds it in the same loop that does the rewriting,
// relying on a temporary always being defined before its use. That holds pre-SSA in a graph built
// from an expression tree, and it is relied on here for the same reason: a value's defining
// instruction precedes every use of it in evaluation order.
type manualMemoSidemap struct {
	// functions maps a temporary to the function expression stored in it.
	functions map[static_single_assignment.IdentifierId]bool
	// manualMemos maps a temporary holding a loaded `useMemo`/`useCallback` to which one it is.
	manualMemos map[static_single_assignment.IdentifierId]ManualMemoKind
	// react holds temporaries holding the `React` namespace object.
	react map[static_single_assignment.IdentifierId]bool
	// depsLists maps a temporary holding an array literal to its element places.
	depsLists map[static_single_assignment.IdentifierId][]Place
	// deps maps a temporary to the access path it evaluates to, when it is one.
	deps map[static_single_assignment.IdentifierId]ManualMemoDependency
	// optionals is upstream's `findOptionalPlaces`: property values guarded by an Optional
	// terminal. Structural optional lowering deliberately leaves PropertyLoad.Optional false.
	optionals map[static_single_assignment.IdentifierId]bool
	// optionalJoins are the only phi blocks through which a single resolved optional arm may be
	// projected. An enclosing ternary or logical join is a distinct expression.
	optionalJoins map[static_single_assignment.BlockId]bool
	// anchors maps a temporary to the instruction that defined it.
	//
	// Upstream keeps the whole defining instruction on its `ManualMemoCallee`; only the id is
	// needed here, and keeping it for every temporary rather than only callee loads costs one map
	// write per instruction and removes a special case.
	anchors map[static_single_assignment.IdentifierId]InstructionId
}

// calleeAnchor is the instruction that defined a temporary, for anchoring `StartMemoize`.
func (sidemap *manualMemoSidemap) calleeAnchor(identifier static_single_assignment.IdentifierId) InstructionId {
	return sidemap.anchors[identifier]
}
