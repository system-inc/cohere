package core

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// atomicReadState is the lattice value: the two sets upstream keeps per code path segment.
//
// # Why both sets travel, when only one is consulted at a report site
//
// `isOutdated` reads only the outdated set, so a first version of this rule kept the fresh set
// block-local and treated it as bookkeeping inside one block. That is wrong wherever a read and the
// suspension that stales it land in different blocks, which is what any forked right-hand side
// produces. Measured on upstream's own cases: `foo = foo + (bar ? baz : await amount)` lays the read
// out in the entry block and the await in one arm of the fork, so at the await the block-local fresh
// set is empty and nothing is ever marked outdated. Five corpus findings were silent that way, and
// the rule's own doc comment had asserted the collapse was safe, which is exactly the "documented a
// limit I did not measure" failure.
//
// Upstream carries both sets forward in `SegmentInfo#initialize`, unioning each from every previous
// segment. This is the same thing said as a lattice.
type atomicReadState struct {
	// fresh holds bindings read since the last suspension on this path. A suspension moves them all
	// into outdated.
	fresh map[*ast.Symbol]bool
	// outdated holds bindings whose last read was before a suspension and which no later read has
	// refreshed. A write to one of these is what this rule reports.
	outdated map[*ast.Symbol]bool
}

func (state atomicReadState) clone() atomicReadState {
	cloned := atomicReadState{
		fresh:    make(map[*ast.Symbol]bool, len(state.fresh)),
		outdated: make(map[*ast.Symbol]bool, len(state.outdated)),
	}
	for symbol := range state.fresh {
		cloned.fresh[symbol] = true
	}
	for symbol := range state.outdated {
		cloned.outdated[symbol] = true
	}
	return cloned
}
