package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/control_flow_graph"
)

// outdatedReadLattice is the forward analysis over atomicReadState.
//
// # Union is the meet, and it is upstream's own
//
// `SegmentInfo#initialize` folds every previous segment's two sets together with
// `Set.prototype.add`, and `isOutdated` returns true if any current segment holds the variable. Both
// are disjunction, so a binding outdated on one incoming path is outdated at the join. That is the
// conservative direction for a race warning: a write reachable by a path where the read was stale is
// a write that can be stale.
type outdatedReadLattice struct{}

// Bottom is the identity for union, so both sets are empty.
func (outdatedReadLattice) Bottom() atomicReadState {
	return atomicReadState{fresh: map[*ast.Symbol]bool{}, outdated: map[*ast.Symbol]bool{}}
}

// Entry says nothing has been read and nothing is stale where the function begins. It coincides
// numerically with Bottom and means a different thing, which is why it is written separately.
func (outdatedReadLattice) Entry() atomicReadState {
	return atomicReadState{fresh: map[*ast.Symbol]bool{}, outdated: map[*ast.Symbol]bool{}}
}

func (outdatedReadLattice) Meet(left, right atomicReadState) atomicReadState {
	// Neither argument is mutated: Solve holds one as a settled boundary value and compares against
	// it to decide whether anything moved.
	merged := atomicReadState{
		fresh:    make(map[*ast.Symbol]bool, len(left.fresh)+len(right.fresh)),
		outdated: make(map[*ast.Symbol]bool, len(left.outdated)+len(right.outdated)),
	}
	for symbol := range left.fresh {
		merged.fresh[symbol] = true
	}
	for symbol := range right.fresh {
		merged.fresh[symbol] = true
	}
	for symbol := range left.outdated {
		merged.outdated[symbol] = true
	}
	for symbol := range right.outdated {
		merged.outdated[symbol] = true
	}
	return merged
}

func (outdatedReadLattice) Transfer(
	block *control_flow_graph.Block[atomicEvent],
	incoming atomicReadState,
) atomicReadState {
	state := incoming.clone()
	applyAtomicTransfer(block, state, nil)
	return state
}

func (outdatedReadLattice) Equal(left, right atomicReadState) bool {
	return sameAtomicSymbolSet(left.fresh, right.fresh) &&
		sameAtomicSymbolSet(left.outdated, right.outdated)
}
