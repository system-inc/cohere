package return_nil_ness

import (
	"fmt"
	"go/types"
	"slices"
	"strings"

	"honnef.co/go/tools/analysis/dfa"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation/forward_flow"
)

// callees is what judging a function's body asks of each function it calls statically: the nilness of its results.
// The push walk answers from the facts and judges a function of its own package as it is met; the resolver's walk
// answers from what it computed, on demand.
type callees interface {
	returns(function *intermediate_representation.Function) []ValueNilness
}

// judgment judges one function's body, asking callees of each function it calls.
type judgment struct {
	callees callees
}

// defaultNilnessForSignature is the nilness of each result of signature when nothing is known of them.
func defaultNilnessForSignature(signature *types.Signature) []ValueNilness {
	count := signature.Results().Len()
	if count == 0 {
		return nil
	}
	defaults := make([]ValueNilness, count)
	for index := range count {
		defaults[index] = defaultNilness(signature.Results().At(index).Type())
	}
	return defaults
}

// defaultNilness is the nilness of a value of valueType when nothing is known of it: {MaybeNil MaybeNil} when it may be
// nil, MaybePointerLike handling type parameters with type sets too, and {NeverNil NeverNil} otherwise.
func defaultNilness(valueType types.Type) ValueNilness {
	if typeutil.MaybePointerLike(valueType) {
		return ValueNilness{MaybeNil, MaybeNil}
	}
	return ValueNilness{NeverNil, NeverNil}
}

// judge is the nilness of each of function's results from a forward analysis over its blocks, merged over every block
// that returns, each normalized, so a result that is no pointer is never nil and one the analysis knows nothing of may
// be nil.
func (judging *judgment) judge(function *intermediate_representation.Function) []ValueNilness {
	values := numbering{}

	// The default state of the values that are no instruction is set on entry, since state.get cannot supply it
	// where control flow merges: a merge of lattice values knows nothing of a value's kind.
	entry := state{cloned: true, numbering: values}
	for _, parameter := range function.Parameters {
		if typeutil.MaybePointerLike(parameter.Type()) {
			entry.set(parameter, ValueNilness{Inner: MaybeNil, Outer: MaybeNil})
		}
		// Nilness is never tracked for value types, so they need not be present in the entry state either.
	}
	if strings.HasPrefix(function.Synthetic, "bound method wrapper") {
		// A bound method's bound receiver might be nil.
		for _, freeVariable := range function.FreeVariables {
			entry.set(freeVariable, ValueNilness{Outer: MaybeNil})
		}
	} else {
		// A closure's variables closed over are allocations, which cannot be nil.
		for _, freeVariable := range function.FreeVariables {
			entry.set(freeVariable, ValueNilness{Outer: NeverNil})
		}
	}
	var operands []*intermediate_representation.Value
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			operands = instruction.Operands(operands[:0])
			for _, operand := range operands {
				constant, isConstant := (*operand).(*intermediate_representation.Constant)
				if isConstant && typeutil.MaybePointerLike(constant.Type()) {
					// The only constant that is pointer like is nil.
					entry.set(constant, ValueNilness{Inner: AlwaysNil, Outer: AlwaysNil})
				}
			}
		}
	}
	analysis := forward_flow.Forward[dfa.DenseMapLattice[ValueNilness, lattice]](
		function,
		map[int][]ValueNilness{0: entry.values},
		func(fromIndex int, toIndex int, in []ValueNilness) []ValueNilness {
			from := function.Blocks[fromIndex]
			to := function.Blocks[toIndex]
			current := state{numbering: values, values: in}
			judging.transfer(from, to, &current)
			judging.transferPhis(to, slices.Index(to.Predecessors, from), &current)
			return current.values
		},
	)

	returned := make([]ValueNilness, function.Signature.Results().Len())
	for block := range function.Returns() {
		control, isReturn := block.Control().(*intermediate_representation.Return)
		if !isReturn {
			// Invariant: Returns yields only the blocks whose control instruction is a return.
			panic(fmt.Sprintf("return_nil_ness: %s's block %d, yielded by Returns, ends in no return", function,
				block.Index))
		}
		current := state{numbering: values, values: analysis.In(block.Index)}
		judging.transfer(block, nil, &current)
		for index, result := range control.Results {
			returned[index] = lattice{}.Merge(returned[index], current.get(result))
		}
	}

	for index := range returned {
		resultType := function.Signature.Results().At(index).Type()
		if !typeutil.MaybePointerLike(resultType) {
			returned[index] = ValueNilness{NeverNil, NeverNil}
			continue
		}
		returned[index] = normalize(returned[index], resultType)
	}
	return returned
}

// interesting is whether returned, the results judge found for a function of signature, says more than that each may
// be nil, which is when the push marks the function with a fact. Returned that is not is the defaults of signature.
func interesting(signature *types.Signature, returned []ValueNilness) bool {
	for index, value := range returned {
		if typeutil.MaybePointerLike(signature.Results().At(index).Type()) &&
			value != (ValueNilness{MaybeNil, MaybeNil}) {
			return true
		}
	}
	return false
}

// transferPhis sets each phi at the head of block to the nilness of its edge from the predecessor at index
// predecessor, all at once: every edge is read before any phi is set, so a phi whose edge is another phi of the block
// reads that phi's value from before the edge, as a phi means.
//
// This is the one step not kept from staticcheck, whose processPhis sets the phis one after another, so such a phi
// reads the new value of a phi set before it and the old value of one set after (see the package's doc). In regexp2's
// reduceAtomic, whose loop carries atomic and child, go/ir orders them [atomic, child] and atomic reads the old child,
// never nil, where ours orders them [child, atomic] and a literal port read the child just loaded, which may be nil.
func (judging *judgment) transferPhis(
	block *intermediate_representation.BasicBlock,
	predecessor int,
	current *state,
) {
	var phis []*intermediate_representation.Phi
	var incoming []ValueNilness
	for _, instruction := range block.Instructions {
		phi, isPhi := instruction.(*intermediate_representation.Phi)
		if !isPhi {
			break
		}
		phis = append(phis, phi)
		incoming = append(incoming, current.get(phi.Edges[predecessor]))
	}
	for index, phi := range phis {
		current.set(phi, incoming[index])
	}
}
