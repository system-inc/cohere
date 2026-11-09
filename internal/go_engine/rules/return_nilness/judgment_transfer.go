package return_nilness

import (
	"fmt"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/exp/typeparams"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// transfer carries current through from's instructions on the edge to to, nil for the walk of a returning block,
// which knows nilness both from what the instructions make and from what they could not have run on: a value loaded
// through, indexed or called was not nil. The analysis is forward only, so what an instruction proves of a value holds
// only after it, though the value had it before.
func (judging *judgment) transfer(
	from *intermediate_representation.BasicBlock,
	to *intermediate_representation.BasicBlock,
	current *state,
) {
	for _, instruction := range from.Instructions {
		// Returning early for an instruction that is a value of a type that is not pointer like is tempting, but an
		// instruction such as a load says something of the value it operates on.

		switch instruction := instruction.(type) {
		case *intermediate_representation.Convert:
			current.set(instruction, current.get(instruction.X))
		case *intermediate_representation.SliceToArrayPointer:
			// Go does not allow (*T)(s) where T is a type parameter whose type set holds array types, but it does allow
			// (T)(s) where T is a type parameter whose type set holds pointers to array types.

			allNonZero := typeutil.All(instruction.Type(), func(term *types.Term) bool {
				pointer, isPointer := term.Type().Underlying().(*types.Pointer)
				if !isPointer {
					// Invariant: a slice converts only to a pointer to an array, or to a type parameter whose every
					// term is one, which the type checker proves.
					panic(fmt.Sprintf("return_nilness: a slice converted to %s, which is no pointer", term.Type()))
				}
				return typeutil.All(pointer.Elem(), func(innerTerm *types.Term) bool {
					array, isArray := innerTerm.Type().Underlying().(*types.Array)
					if !isArray {
						// Invariant: as above, the pointer's element is an array.
						panic(fmt.Sprintf("return_nilness: a slice converted to a pointer to %s, which is no array",
							innerTerm.Type()))
					}
					return array.Len() != 0
				})
			})

			if allNonZero {
				// Converting a nil slice to a pointer to an array whose length is not zero panics.
				current.setOuter(instruction, NeverNil)
				current.setOuter(instruction.X, NeverNil)
			} else {
				current.set(instruction, current.get(instruction.X))
			}
		case *intermediate_representation.SliceToArray:
			// The same as SliceToArrayPointer, without the pointer.

			allNonZero := typeutil.All(instruction.Type(), func(term *types.Term) bool {
				array, isArray := term.Type().Underlying().(*types.Array)
				if !isArray {
					// Invariant: a slice converts only to an array, or to a type parameter whose every term is one.
					panic(fmt.Sprintf("return_nilness: a slice converted to %s, which is no array", term.Type()))
				}
				return array.Len() != 0
			})

			if allNonZero {
				// Converting a nil slice to an array whose length is not zero panics.
				current.setOuter(instruction.X, NeverNil)
			}
		case *intermediate_representation.Slice:
			if typeutil.All(instruction.X.Type(), typeutil.IsType[*types.Array]) {
				// Slicing an array never makes a nil slice.
				current.setOuter(instruction, NeverNil)
				continue
			}

			if hasNonZeroBound(instruction.Low) || hasNonZeroBound(instruction.High) ||
				hasNonZeroBound(instruction.Maximum) {
				// A bound is not zero, so slicing succeeds only on a slicee that is not nil.
				current.setOuter(instruction, NeverNil)
				current.setOuter(instruction.X, NeverNil)
			} else {
				// The new slice is as nil as the slicee.
				current.set(instruction, current.get(instruction.X))
			}

		case *intermediate_representation.If:
			comparison, isComparison := instruction.Condition.(*intermediate_representation.BinaryOperation)
			if !isComparison {
				continue
			}
			var target intermediate_representation.Value
			if isNilConstant(comparison.X) {
				target = comparison.Y
			} else if isNilConstant(comparison.Y) {
				target = comparison.X
			} else {
				continue
			}
			operator := comparison.Operator
			if to != from.Successors[0] {
				// On the false branch the comparison is negated.
				switch operator {
				case token.EQL:
					operator = token.NEQ
				case token.NEQ:
					operator = token.EQL
				default:
					// Invariant: a comparison with nil is == or !=, the only operators the type checker allows on it.
					panic(fmt.Sprintf("return_nilness: internal error: unhandled token %v", operator))
				}
			}
			switch operator {
			case token.EQL:
				current.set(target, ValueNilness{AlwaysNil, AlwaysNil})
			case token.NEQ:
				current.setOuter(target, NeverNil)
			default:
				// Invariant: as above, a comparison with nil is == or !=, and negating one gives the other.
				panic(fmt.Sprintf("return_nilness: internal error: unhandled token %v", operator))
			}

			// Staticcheck leaves for later the comparison of two values that are not nil: the true branch of
			// a value never nil compared equal to one that may be makes the second never nil.
		case *intermediate_representation.ChangeType:
			current.set(instruction, current.get(instruction.X))
		case *intermediate_representation.MultiConvert:
			current.set(instruction, current.get(instruction.X))
		case *intermediate_representation.Load:
			if _, isGlobal := instruction.X.(*intermediate_representation.Global); isGlobal {
				current.setOuter(instruction, MaybeNilGlobal)
			} else {
				current.setOuter(instruction, MaybeNil)
			}
			current.setOuter(instruction.X, NeverNil)
		case *intermediate_representation.FieldAddress:
			current.setOuter(instruction.X, NeverNil)
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.IndexAddress:
			current.setOuter(instruction.X, NeverNil)
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.Allocation:
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.MakeMap:
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.MakeSlice:
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.MakeClosure:
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.MakeChannel:
			current.setOuter(instruction, NeverNil)
		case *intermediate_representation.MapUpdate:
			current.setOuter(instruction.Map, NeverNil)
		case *intermediate_representation.Store:
			current.setOuter(instruction.Address, NeverNil)
		case intermediate_representation.CallInstruction:
			// Calling a nil function, or deferring it or starting it in a goroutine, panics or fails.
			if !instruction.Common().IsInvoke() {
				current.setOuter(instruction.Common().Value, NeverNil)
			}
			call, isCall := instruction.(*intermediate_representation.Call)
			if !isCall {
				// Defer and Go produce no value.
				continue
			}
			if call.Common().Signature().Results().Len() != 1 {
				// A call returning nothing says nothing more, and the results of one returning more than one are
				// handled at their Extract.
				continue
			}

			judging.transferReturnValue(current, call, call, 0)
		case *intermediate_representation.Send:
			current.setOuter(instruction.Channel, NeverNil)
		case *intermediate_representation.Receive:
			current.setOuter(instruction.Channel, NeverNil)
			current.set(instruction, ValueNilness{MaybeNil, MaybeNil})
		case *intermediate_representation.MakeInterface:
			current.set(instruction, ValueNilness{
				Inner: current.get(instruction.X).Outer,
				Outer: NeverNil,
			})
		case *intermediate_representation.ChangeInterface:
			current.set(instruction, current.get(instruction.X))
		case *intermediate_representation.TypeAssert:
			// A comma-ok assertion's result is a tuple, whose Extracts get the nilness of its values, so only the
			// other form is judged here.
			if !instruction.CommaOk {
				// The interface value cannot have been nil, or the assertion would have panicked.
				current.setOuter(instruction.X, NeverNil)

				if types.IsInterface(instruction.Type()) && !typeparams.IsTypeParam(instruction.Type()) {
					// Asserting to another interface fails on a nil assertee, and makes a new interface value.
					current.setOuter(instruction, NeverNil)
					current.setInner(instruction, current.get(instruction.X).Inner)
				} else {
					// The assertion extracted the interface value's inner value.
					current.setOuter(instruction, current.get(instruction.X).Inner)
				}
			}
		case *intermediate_representation.TypeSwitch:
			// Handled at its Extracts.
		case *intermediate_representation.MapLookup:
			if current.get(instruction.X).Outer == AlwaysNil {
				current.set(instruction, ValueNilness{AlwaysNil, AlwaysNil})
			} else {
				current.set(instruction, ValueNilness{MaybeNil, MaybeNil})
			}
		case *intermediate_representation.Field:
			current.set(instruction.X, ValueNilness{NeverNil, NeverNil})
			current.set(instruction, ValueNilness{MaybeNil, MaybeNil})
		case *intermediate_representation.Index:
			current.set(instruction.X, ValueNilness{NeverNil, NeverNil})
			current.set(instruction, ValueNilness{MaybeNil, MaybeNil})

		case *intermediate_representation.Extract:
			judging.transferExtract(current, instruction)
		case *intermediate_representation.Select:
			if instruction.Blocking && len(instruction.States) == 1 {
				// A select with no default and one state finished only if that state's channel was not nil.
				current.setOuter(instruction.States[0].Channel, NeverNil)
			}
		case *intermediate_representation.Jump, *intermediate_representation.BlankStore,
			*intermediate_representation.Phi, *intermediate_representation.Panic, *intermediate_representation.Return,
			*intermediate_representation.RunDefers, *intermediate_representation.Unreachable,
			*intermediate_representation.ConstantSwitch, *intermediate_representation.UnaryOperation,
			*intermediate_representation.BinaryOperation, *intermediate_representation.CompositeValue,
			*intermediate_representation.Range, *intermediate_representation.Next:
		default:
			position := judging.pass.Fset.PositionFor(instruction.Pos(), false)
			// Invariant: every kind of instruction the representation builds has a case above, so a kind it builds
			// that is new to this pass is found here, as staticcheck's pass finds one, rather than judged by chance.
			panic(fmt.Sprintf("return_nilness: internal error: unhandled type %T at %s", instruction, position))
		}
	}
}

// transferExtract sets the nilness of extract, one value of a tuple: of a comma-ok assertion, which says nothing yet
// of its value, of a call's results, or of a type switch's case, which says whether the switched value was nil.
func (judging *judgment) transferExtract(current *state, extract *intermediate_representation.Extract) {
	switch tuple := extract.Tuple.(type) {
	case *intermediate_representation.TypeAssert:
		// Reaching here, the assertion used the comma-ok form, and nothing is known of its result yet.
		if extract.Index == 0 {
			current.set(extract, ValueNilness{MaybeNil, MaybeNil})
		}

		// Staticcheck leaves for later setting the value's nilness on the branches that check ok, since ok may be
		// used in any way, and a relation between the two Extracts is no fact this analysis can hold.

	case *intermediate_representation.Call:
		judging.transferReturnValue(current, extract, tuple, extract.Index)

	case *intermediate_representation.TypeSwitch:
		if extract.Index == 0 {
			// Index 0 is an integer, which says nothing.
			return
		}
		condition := extract.Index - 1
		if condition >= len(tuple.Conditions) {
			// The default branch: with a case for untyped nil, reaching it says the interface value is not nil.
			hasNil := slices.ContainsFunc(tuple.Conditions, func(caseType types.Type) bool {
				basic, isBasic := caseType.(*types.Basic)
				return isBasic && basic.Kind() == types.UntypedNil
			})
			if hasNil {
				current.setOuter(tuple.Tag, NeverNil)
			} else {
				current.setOuter(tuple.Tag, MaybeNil)
			}
			current.setOuter(extract, current.get(tuple.Tag).Inner)
		} else {
			// No Extract stands for the untyped nil case, so running any Extract of a type switch says the switched
			// value was no nil interface value.
			current.setOuter(tuple.Tag, NeverNil)
			caseType := tuple.Conditions[condition]
			if types.IsInterface(caseType) && !typeparams.IsTypeParam(caseType) {
				// Asserting to an interface type always makes an interface value that is not nil.
				current.setInner(extract, current.get(tuple.Tag).Inner)
				current.setOuter(extract, NeverNil)
			} else {
				current.setOuter(extract, current.get(tuple.Tag).Inner)
			}
		}
	default:
		current.set(extract, ValueNilness{MaybeNil, MaybeNil})
	}
}

// transferReturnValue sets the nilness of value, the result at index result of call: from what a builtin is known to
// return, from the judging of a static callee, or, calling what cannot be told, that it may be nil.
func (judging *judgment) transferReturnValue(
	current *state,
	value intermediate_representation.Value,
	call *intermediate_representation.Call,
	result int,
) {
	resultType := call.Common().Signature().Results().At(result).Type()
	if !typeutil.MaybePointerLike(resultType) {
		current.setOuter(value, NeverNil)
		return
	}

	if builtin, isBuiltin := call.Call.Value.(*intermediate_representation.Builtin); isBuiltin {
		switch builtin.Name() {
		case "append":
			// Staticcheck leaves for later that variadic arguments known not to be empty make a slice that is not
			// nil.
			switch appended := current.get(call.Call.Arguments[0]).Outer; appended {
			case MaybeNil, MaybeNilGlobal, NeverNil:
				current.setOuter(value, appended)
			case AlwaysNil:
				current.setOuter(value, MaybeNil)
			}
		case "UnsafeSlice":
			// unsafe.Slice panics on a negative length, or on a nil pointer with a length that is not zero, so a
			// pointer that is not nil cannot become a nil slice, nor the other way.
			current.set(value, current.get(call.Call.Arguments[0]))
		case "UnsafeStringData":
			// Staticcheck leaves for later what a string's length would say here.
			current.setOuter(value, MaybeNil)
		case "UnsafeSliceData":
			// A slice that is not nil but has no capacity still gives a pointer that is not nil.
			current.set(value, current.get(call.Call.Arguments[0]))
		case "UnsafeAdd":
			// Pointer arithmetic can turn a nil pointer into one that is not and the other way. Staticcheck leaves for
			// later that a positive addend never makes a nil pointer.
			current.setOuter(value, MaybeNil)
		case "ssa:deferstack":
			current.setOuter(value, NeverNil)
		case "ssa:wrapnilchk":
			current.setOuter(value, NeverNil)
		case "recover":
			current.setOuter(value, MaybeNil)
		default:
			// Invariant: these are every builtin whose result may be nil; new, make and the rest are built as
			// instructions of their own, or give values that cannot be nil, which returned above.
			panic(fmt.Sprintf("return_nilness: internal error: unhandled builtin %s", builtin.Name()))
		}
		return
	}

	callee := call.Common().StaticCallee()
	if callee == nil {
		// Which function is called is not known.
		current.set(value, ValueNilness{MaybeNil, MaybeNil})
		return
	}
	calleeNilness := judging.returns(callee)
	if len(calleeNilness) > result {
		current.set(value, normalize(calleeNilness[result], resultType))
	} else {
		current.set(value, ValueNilness{MaybeNil, MaybeNil})
	}
}

// hasNonZeroBound is whether bound, a slicing's low, high or maximum index, is a constant that is not zero, or one
// whose value does not fit an int64. A bound that is missing or not constant is not known to be anything.
func hasNonZeroBound(bound intermediate_representation.Value) bool {
	if bound == nil {
		return false
	}
	// Staticcheck leaves constant propagation and value range analysis, which would say more here, for later.
	if boundConstant, isConstant := bound.(*intermediate_representation.Constant); isConstant {
		value, exact := constant.Int64Val(boundConstant.Value)
		return !exact || value != 0
	}
	return false
}

// isNilConstant is whether value is a constant whose value is nil.
func isNilConstant(value intermediate_representation.Value) bool {
	nilConstant, isConstant := value.(*intermediate_representation.Constant)
	if !isConstant {
		return false
	}
	return nilConstant.Value == nil
}
