// Package nil_ness is x/tools' nilness pass, a nil dereference, a nil panic or a comparison with nil whose outcome
// the function's own control flow already decides, ported onto staticcheck's intermediate representation, which
// staticcheck's checks already build for every package, so the run no longer builds x/tools' single assignment form
// for this pass.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/nilness/nilness.go, whose logic it
// keeps and whose messages it reports word for word. Where staticcheck's form is built differently from x/tools', the
// code says how, and how the translation still finds what the original finds. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages.
//
// The two differ only where adjudicated, each case in testdata/src/differences and held by
// TestPortDiffersFromOriginalOnlyWhereItIsRight, and the port misses nothing the original finds. The port finds more,
// rightly: staticcheck lifts a variable up to the first use that takes its address or a closure captures it, so a
// comparison before that use sees the variable's known value where x/tools sees memory; it merges the same constant
// from every edge into that constant, so a variable only ever assigned nil is known nil; it reads the value behind a
// pointer in the caller of a method with a value receiver, where x/tools reads it in a wrapper out of sight; and
// buildir's functions include the closures of package-level initializers, which buildssa's leave out.
package nil_ness

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Name is the original's, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it read
// the same as before the port took the original's place.
const Name = "nilness"

// Doc is the rule's documentation.
const Doc = "Flags a nil dereference, a panic with nil, and a comparison with nil the control flow already " +
	"decides: x/tools' nilness, ported onto staticcheck's intermediate representation."

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Check judges one function, visiting its reachable blocks in dominance order with the nilness facts that dominate
// each.
func Check(context *rule.Context, function *ir.Function) {
	// Skip cgo-generated functions annotated with //go:cgo_unsafe_args such as _cgo_cmalloc since they behave in
	// magical ways not captured by the single assignment form, leading to false positives.
	if hasCgoUnsafeArguments(function) {
		return
	}
	// Visit the entry block. No need to visit function.Recover.
	if function.Blocks != nil {
		walk := &walk{context: context, seen: make([]bool, len(function.Blocks))}
		walk.visit(function.Blocks[0], make([]fact, 0, 20)) // 20 is plenty
	}
}

// nilnessOf reports whether value is definitely nil, definitely not nil, or unknown given the dominating stack of
// facts.
func nilnessOf(stack []fact, value ir.Value) nilState {
	switch value := value.(type) {
	// Unwrap ChangeInterface and Slice values recursively, to detect if underlying values have any facts recorded or
	// are otherwise known with regard to nilness.
	//
	// This work must be in addition to expanding facts about ChangeInterfaces during inference/fact gathering
	// because this covers cases where the nilness of a value is intrinsic, rather than based on inferred facts, such
	// as a zero value interface variable. That said, this work alone would only inform us when facts are about
	// underlying values, rather than outer values, when the analysis is transitive in both directions.
	case *ir.ChangeInterface:
		if underlying := nilnessOf(stack, value.X); underlying != unknown {
			return underlying
		}
	case *ir.MakeInterface:
		// A MakeInterface is non-nil unless its operand is a type parameter.
		parameter, isParameter := types.Unalias(value.X.Type()).(*types.TypeParam)
		if !isParameter {
			return isNonNil
		}

		// A MakeInterface of a type parameter is non-nil if the type parameter cannot be instantiated as an
		// interface type (#66835). The original reads the constraint's normal terms through x/tools' internal
		// typeparams package; staticcheck's type set reads them through x/exp's, the same algorithm, and holds no
		// terms where either fails.
		if len(typeutil.NewTypeSet(parameter).Terms) > 0 {
			return isNonNil
		}

		// If the type parameter can be instantiated as an interface (and thus also as a concrete type), we can't
		// determine the nilness.

	case *ir.Slice:
		if underlying := nilnessOf(stack, value.X); underlying != unknown {
			return underlying
		}
	case *ir.SliceToArrayPointer:
		underlying := nilnessOf(stack, value.X)
		if sliceToArrayPointerLength(value) > 0 {
			if underlying == isNil {
				// We know that *(*[1]byte)(nil) is going to panic because of the conversion. So return unknown to
				// the caller, prevent useless nil dereference reporting due to * operator.
				return unknown
			}
			// Otherwise, the conversion will yield a non-nil pointer to array. Note that the instruction can still
			// panic if array length greater than slice length. If the value is used by another instruction, that
			// instruction can assume the panic did not happen when that instruction is reached.
			return isNonNil
		}
		// In case array length is zero, the conversion result depends on nilness of the slice.
		if underlying != unknown {
			return underlying
		}
	}

	// Is value intrinsically nil or non-nil?
	switch value := value.(type) {
	case *ir.Alloc,
		*ir.FieldAddr,
		*ir.FreeVar,
		*ir.Function,
		*ir.Global,
		*ir.IndexAddr,
		*ir.MakeChan,
		*ir.MakeClosure,
		*ir.MakeMap,
		*ir.MakeSlice:
		return isNonNil

	case *ir.Const:
		if isNilConstant(value) {
			return isNil // nil or zero value of a pointer-like type
		}
		return unknown // non-pointer
	}

	// Search dominating control-flow facts.
	for _, known := range stack {
		if known.value == value {
			return known.nilness
		}
	}
	return unknown
}

// isNilConstant is whether candidate is the nil of a type that has one, as x/tools' Const.IsNil decides it.
// staticcheck's IsNil leaves out unsafe.Pointer, whose zero constant both forms build with no value, and x/tools'
// counts it.
func isNilConstant(candidate *ir.Const) bool {
	if candidate.Value != nil {
		return false
	}
	if basic, isBasic := candidate.Type().Underlying().(*types.Basic); isBasic && basic.Kind() == types.UnsafePointer {
		return true
	}
	return candidate.IsNil()
}

// sliceToArrayPointerLength is the length of the array conversion points to. The original asserts the pointer to an
// array and panics on anything else; the conversion always yields one, so the port reads zero instead of crashing
// should that ever not hold.
func sliceToArrayPointerLength(conversion *ir.SliceToArrayPointer) int64 {
	pointer, isPointer := conversion.Type().(*types.Pointer)
	if !isPointer {
		return 0
	}
	array, isArray := pointer.Elem().Underlying().(*types.Array)
	if !isArray {
		return 0
	}
	return array.Len()
}

// If block ends with an equality comparison, equality returns the operation and its true (equal) and false (not
// equal) successors.
func equality(block *ir.BasicBlock) (*ir.BinOp, *ir.BasicBlock, *ir.BasicBlock) {
	if branch, isIf := block.Control().(*ir.If); isIf {
		if comparison, isComparison := branch.Cond.(*ir.BinOp); isComparison {
			switch comparison.Op {
			case token.EQL:
				return comparison, block.Succs[0], block.Succs[1]
			case token.NEQ:
				return comparison, block.Succs[1], block.Succs[0]
			}
		}
	}
	return nil, nil, nil
}

// expandFacts takes a single fact and returns the set of facts that can be known about it or any of its related
// values. Some operations, like ChangeInterface, have transitive nilness, such that if you know the underlying value
// is nil, you also know the value itself is nil, and vice versa. This operation allows callers to match on any of the
// related values in analyses, rather than just the one form of the value that happened to appear in a comparison.
//
// This work must be in addition to unwrapping values within nilnessOf because while this work helps give facts about
// transitively known values based on inferred facts, the recursive check within nilnessOf covers cases where nilness
// facts are intrinsic to the underlying value, such as a zero value interface variables.
//
// ChangeInterface is the only expansion currently supported, but others, like Slice, could be added.
func expandFacts(known fact) []fact {
	expanded := []fact{known}
	for current := known; ; {
		change, isChange := current.value.(*ir.ChangeInterface)
		if !isChange {
			return expanded
		}
		current = fact{change.X, current.nilness}
		expanded = append(expanded, current)
	}
}

// isNillable is whether a value of type typ can be nil, as the original decides it for an asserted type.
func isNillable(typ types.Type) bool {
	// The original notes that CoreType, and the case for an interface, look wrong: this should probably use
	// Underlying, and handle a type parameter by computing the union across its normal terms.
	switch typ := coreType(typ).(type) {
	case *types.Pointer,
		*types.Map,
		*types.Signature,
		*types.Chan,
		*types.Interface,
		*types.Slice:
		return true
	case *types.Basic:
		return typ == types.Typ[types.UnsafePointer]
	}
	return false
}

// coreType is the core type of typ, or nil when it has none, as x/tools' internal typeparams.CoreType reads it: the
// underlying type of a type that is no interface, and otherwise the one type its type set's terms share, which
// staticcheck's typeutil.CoreType reads. staticcheck's reads a type set for every type, so the port asks it only of
// an interface, keeping the common case as cheap as the original's.
func coreType(typ types.Type) types.Type {
	underlying := typ.Underlying()
	if _, isInterface := underlying.(*types.Interface); !isInterface {
		return underlying
	}
	return typeutil.CoreType(typ)
}

// isTypeParameter is whether typ is a type parameter.
func isTypeParameter(typ types.Type) bool {
	_, isParameter := types.Unalias(typ).(*types.TypeParam)
	return isParameter
}

// isRangeIndex reports whether the instruction is a slice indexing operation slice[i] within a "for range slice"
// loop. The operation could be explicit, such as slice[i] within (or even after) the loop, or it could be implicit,
// such as "for i, v := range slice {}". (These cannot be reliably distinguished.)
func isRangeIndex(address *ir.IndexAddr) bool {
	// Here we reverse-engineer the lowering of range-over-slice, which staticcheck builds as x/tools does:
	//
	//      n = len(x)
	//      jump loop
	// loop:                                                "rangeindex.loop"
	//      phi = φ(-1, incr) #rangeindex
	//      incr = phi + 1
	//      cond = incr < n
	//      if cond goto body else done
	// body:                                                "rangeindex.body"
	//      instr = &x[incr]
	//      ...
	// done:
	increment, isIncrement := address.Index.(*ir.BinOp)
	if !isIncrement || increment.Op != token.ADD {
		return false
	}
	loop := increment.Block()
	if loop.Comment != "rangeindex.loop" {
		return false
	}
	branch, isIf := loop.Control().(*ir.If)
	if !isIf {
		return false
	}
	// The original asserts the condition a comparison and panics on anything else; the loop's condition always is
	// one, so the port declines instead of crashing should that ever not hold.
	condition, isComparison := branch.Cond.(*ir.BinOp)
	if !isComparison || condition.X != increment || condition.Op != token.LSS {
		return false
	}
	call, isCall := condition.Y.(*ir.Call)
	if !isCall {
		return false
	}
	builtin, isBuiltin := call.Common().Value.(*ir.Builtin)
	return isBuiltin && builtin.Name() == "len" && call.Common().Args[0] == address.X
}

// hasCgoUnsafeArguments is whether function, or a function it is nested in, is declared with //go:cgo_unsafe_args.
func hasCgoUnsafeArguments(function *ir.Function) bool {
	for enclosing := function; enclosing != nil; enclosing = enclosing.Parent() {
		if declaration, isDeclaration := enclosing.Syntax().(*ast.FuncDecl); isDeclaration && declaration != nil &&
			declaration.Doc != nil {
			for _, comment := range declaration.Doc.List {
				if isCgoUnsafeArgumentsDirective(comment.Text) {
					return true
				}
			}
		}
	}
	return false
}

// isCgoUnsafeArgumentsDirective is whether a comment's text is the directive go:cgo_unsafe_args, read as x/tools'
// internal astutil.Directives reads a directive: the tool and the name run to the first space.
func isCgoUnsafeArgumentsDirective(text string) bool {
	const directive = "//go:cgo_unsafe_args"
	rest, isDirective := strings.CutPrefix(text, directive)
	return isDirective && (rest == "" || strings.HasPrefix(rest, " "))
}
