// Package verbs is printf's match of a value's type against the kinds of operand a verb accepts, x/tools' printf
// argMatcher, shared by the printf rule and the restore from the host's run, which renders another package's
// unexported field by how its type answers each set of kinds a verb accepts (#b2t9xmf).
package verbs

import (
	"fmt"
	"go/types"
)

// Kinds is the set of kinds of operand a printf verb accepts, a bitmask.
type Kinds int

const (
	Bool Kinds = 1 << iota
	Byte
	Int
	Rune
	String
	Float
	Complex
	Pointer
	Error
	Any Kinds = ^0
)

// AcceptedSets are every set of kinds printf matches a type against below the argument's own: each verb's of its
// table but %, %v and %T, which match no type, and %w, which asks only whether the argument is an error, and the
// sets the file's Go version gives %q and %d, and the * of a width or precision. A struct fits a verb when every
// field does, so these are the answers a field can give printf; the printf package's tests hold its table to them.
var AcceptedSets = []Kinds{
	Int | Float | Complex | Pointer,
	Rune | Int,
	Int | Pointer,
	Float | Complex,
	Pointer,
	Rune | Int | String,
	String,
	Bool,
	Rune | Int | String | Pointer | Float | Complex,
	Rune | Byte | String,
	Int,
}

// ErrorInterface is the predeclared error's interface.
var ErrorInterface = universeError()

// universeError is the interface the predeclared error names.
func universeError() *types.Interface {
	errorInterface, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		// Invariant: the universe declares error as a named interface, `type error interface { Error() string }`.
		panic("the predeclared error is not an interface")
	}
	return errorInterface
}

// argumentMatcher matches types against the kinds a verb accepts, recursively: x/tools' printf argMatcher. A map,
// channel, slice or struct prints with %d when its elements do, and a type parameter is expanded into its type set,
// so seen holds the types matched or being matched, which stops the recursion. reason may say why a match failed.
type argumentMatcher struct {
	accepted Kinds
	seen     map[types.Type]bool
	reason   string
}

// Match reports whether a value of passedType, an argument's own type, fits a verb that accepts accepted, and the
// reason when it does not and there is one to give. A type parameter fits when every type of its type set does.
// %v and %T accept any argument, and are let through before this is asked.
func Match(accepted Kinds, passedType types.Type) (string, bool) {
	matcher := &argumentMatcher{accepted: accepted, seen: map[types.Type]bool{}}
	matched := matcher.match(passedType, true)
	return matcher.reason, matched
}

// MatchNested is Match for a type met below the argument's own, as a struct's field is (matchStruct), where no
// top-level rule applies: what the restore from the host renders of another package's unexported field for each
// accepted set (AcceptedSets), so a field answers each verb as printf's walk of its struct finds it.
func MatchNested(accepted Kinds, nestedType types.Type) (string, bool) {
	matcher := &argumentMatcher{accepted: accepted, seen: map[types.Type]bool{}}
	matched := matcher.match(nestedType, false)
	return matcher.reason, matched
}

// match reports whether candidateType fits the verb. topLevel is set for the argument's own type, where special rules
// apply, and for the types of a top-level type parameter's set, which a type argument replaces at compile time.
func (matcher *argumentMatcher) match(candidateType types.Type, topLevel bool) bool {
	// %w accepts only errors.
	if matcher.accepted == Error {
		return types.ConvertibleTo(candidateType, ErrorInterface)
	}
	// A type that implements fmt.Formatter leaves nothing to check.
	if IsFormatter(candidateType) {
		return true
	}
	// A verb that takes a string takes a value whose String or Error method may give one.
	if matcher.accepted&String != 0 && IsConvertibleToString(candidateType) {
		return true
	}
	if typeParameter, isTypeParameter := types.Unalias(candidateType).(*types.TypeParam); isTypeParameter {
		return matcher.matchTypeParameter(typeParameter, topLevel)
	}
	underlying := candidateType.Underlying()
	if matcher.seen[underlying] {
		// Matched already, so it fit, or being matched, and the recursion stops here. A type such as
		// `type R struct { F []R }` is wrongly let through, a false negative x/tools accepts.
		return true
	}
	matcher.seen[underlying] = true
	switch underlyingType := underlying.(type) {
	case *types.Signature:
		return matcher.accepted == Pointer
	case *types.Map:
		if matcher.accepted == Pointer {
			return true
		}
		// map[int]int matches %d.
		return matcher.match(underlyingType.Key(), false) && matcher.match(underlyingType.Elem(), false)
	case *types.Chan:
		return matcher.accepted&Pointer != 0
	case *types.Array:
		if types.Identical(underlyingType.Elem().Underlying(), types.Typ[types.Byte]) &&
			matcher.accepted&String != 0 {
			return true // %s matches [n]byte.
		}
		// [n]int matches %d.
		return matcher.match(underlyingType.Elem(), false)
	case *types.Slice:
		if types.Identical(underlyingType.Elem().Underlying(), types.Typ[types.Byte]) &&
			matcher.accepted&String != 0 {
			return true // %s matches []byte.
		}
		if matcher.accepted == Pointer {
			return true // %p prints the address of a slice's first element.
		}
		// []int matches %d, and `type T []T` stops at seen; a `type T []*T` stops at the pointer.
		return matcher.match(underlyingType.Elem(), false)
	case *types.Pointer:
		return matcher.matchPointer(underlyingType, topLevel)
	case *types.Struct:
		return matcher.matchStruct(underlyingType)
	case *types.Interface:
		// Which verbs fit depends on the value it holds, which the writer may know.
		return true
	case *types.Basic:
		return matcher.matchBasic(underlyingType)
	}
	return false
}

// matchTypeParameter reports whether every type of typeParameter's type set fits the verb.
func (matcher *argumentMatcher) matchTypeParameter(typeParameter *types.TypeParam, topLevel bool) bool {
	// A type set can name the type parameter again.
	if matcher.seen[typeParameter] {
		return true
	}
	matcher.seen[typeParameter] = true
	terms, computed := structuralTerms(typeParameter)
	if !computed {
		return true // An invalid type, maybe an empty type set.
	}
	if len(terms) == 0 {
		// The type set restricts no underlying type. A type parameter that implements error, fmt.Formatter or
		// fmt.Stringer was let through above, and %v and %T by matchArgumentType, so some type of the set fails
		// the checks below.
		if matcher.accepted == Any {
			// Invariant: matchArgumentType lets every argument of %v and %T through before it matches.
			panic("a type parameter matched against every type")
		}
		return false
	}
	// The reason makes sense only for the argument's own type parameter, and a type set can hold other type
	// parameters, so topLevel alone cannot say which this is.
	reportReason := len(matcher.seen) == 1
	for _, term := range terms {
		if matcher.match(term.termType, topLevel) {
			continue
		}
		if reportReason {
			if term.tilde {
				matcher.reason = fmt.Sprintf("contains ~%s", term.termType)
			} else {
				matcher.reason = fmt.Sprintf("contains %s", term.termType)
			}
		}
		return false
	}
	return true
}

// matchPointer reports whether a pointer fits the verb.
func (matcher *argumentMatcher) matchPointer(pointer *types.Pointer, topLevel bool) bool {
	// A pointer to an invalid type, probably from an import that failed.
	if pointer.Elem() == types.Typ[types.Invalid] {
		return true
	}
	// With %p a pointer prints as one.
	if matcher.accepted == Pointer {
		return true
	}
	if isTypeParameter(pointer.Elem()) {
		return true // Whether the rules below apply is unknown, so the match gives up.
	}
	under := pointer.Elem().Underlying()
	switch under.(type) {
	case *types.Struct, *types.Array, *types.Slice, *types.Map:
	default:
		// Whether the verb prints pointers.
		if matcher.accepted&Pointer == 0 && matcher.accepted&Int != 0 {
			matcher.reason = "use %p for a pointer"
		}
		return matcher.accepted&Pointer != 0
	}
	// A top-level pointer to a struct, array, slice or map prints as what it points to does. A pointer deeper
	// down does not, which keeps fmt out of loops.
	if !topLevel {
		return false
	}
	return matcher.match(under, false)
}

// matchStruct reports whether every field of a struct fits the verb: with %d, every field must print with %d.
func (matcher *argumentMatcher) matchStruct(structType *types.Struct) bool {
	for field := range structType.Fields() {
		if !matcher.match(field.Type(), false) {
			return false
		}
		if matcher.accepted&String != 0 && !field.Exported() && IsConvertibleToString(field.Type()) {
			// Issue #17798: fmt cannot call the String or Error method of an unexported field.
			return false
		}
	}
	return true
}

// matchBasic reports whether a basic type fits the verb.
func (matcher *argumentMatcher) matchBasic(basic *types.Basic) bool {
	switch basic.Kind() {
	case types.UntypedBool, types.Bool:
		return matcher.accepted&Bool != 0
	case types.Byte:
		return matcher.accepted&(Int|Byte) != 0
	case types.Rune, types.UntypedRune:
		return matcher.accepted&(Int|Rune) != 0
	case types.UntypedInt, types.Int, types.Int8, types.Int16, types.Int64, types.Uint, types.Uint16, types.Uint32,
		types.Uint64, types.Uintptr:
		// int32 is the case of rune, and uint8 of byte.
		return matcher.accepted&Int != 0
	case types.UntypedFloat, types.Float32, types.Float64:
		return matcher.accepted&Float != 0
	case types.UntypedComplex, types.Complex64, types.Complex128:
		return matcher.accepted&Complex != 0
	case types.UntypedString, types.String:
		return matcher.accepted&String != 0
	case types.UnsafePointer:
		if matcher.accepted&Pointer == 0 && matcher.accepted&Int != 0 {
			matcher.reason = "use %p for a pointer"
		}
		return matcher.accepted&Pointer != 0
	case types.UntypedNil:
		return false
	case types.Invalid:
		return true // Probably a problem the type checker reports.
	}
	// Invariant: every kind of basic type is a case above, int32 and uint8 as rune and byte.
	panic("a basic type of a kind printf does not know")
}

// isConvertibleToString reports whether a value of valueType may give a string through an Error method or a String
// method of fmt.Stringer's shape.
func IsConvertibleToString(valueType types.Type) bool {
	// Untyped nil converts to both interfaces, and would only panic.
	if basic, isBasic := types.Unalias(valueType).(*types.Basic); isBasic && basic.Kind() == types.UntypedNil {
		return false
	}
	if types.ConvertibleTo(valueType, ErrorInterface) {
		return true // Through Error.
	}
	method, _, _ := types.LookupFieldOrMethod(valueType, false, nil, "String")
	function, isFunction := method.(*types.Func)
	if !isFunction {
		return false
	}
	signature := function.Signature()
	return signature.Params().Len() == 0 && signature.Results().Len() == 1 &&
		signature.Results().At(0).Type() == types.Typ[types.String]
}

// IsFormatter reports whether goType could satisfy fmt.Formatter, whose one method is Format(State, rune).
func IsFormatter(goType types.Type) bool {
	// An interface's value might be a Formatter. A type parameter is not taken for one, since a constraint can say
	// so and is expected to.
	if _, isInterface := goType.Underlying().(*types.Interface); isInterface && !isTypeParameter(goType) {
		return true
	}
	method, _, _ := types.LookupFieldOrMethod(goType, false, nil, "Format")
	function, isFunction := method.(*types.Func)
	if !isFunction {
		return false
	}
	signature := function.Signature()
	return signature.Params().Len() == 2 && signature.Results().Len() == 0 &&
		isTypeNamed(signature.Params().At(0).Type(), "fmt", "State") &&
		types.Identical(signature.Params().At(1).Type(), types.Typ[types.Rune])
}

// isTypeParameter reports whether goType is a type parameter: x/tools' typeparams.IsTypeParam.
func isTypeParameter(goType types.Type) bool {
	_, isParameter := types.Unalias(goType).(*types.TypeParam)
	return isParameter
}

// isTypeNamed reports whether goType is, or is an alias of, the package-level type name of the package at
// packagePath: x/tools' typesinternal.IsTypeNamed.
func isTypeNamed(goType types.Type, packagePath string, name string) bool {
	named, isNamed := types.Unalias(goType).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil && typeName.Pkg() != nil && typeName.Parent() == typeName.Pkg().Scope() &&
		typeName.Pkg().Path() == packagePath && typeName.Name() == name
}
