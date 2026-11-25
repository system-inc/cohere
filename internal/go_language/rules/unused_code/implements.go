package unused_code

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// lookupMethod returns the index of and method with matching package and name, or (-1, nil).
func lookupMethod(interfaceType *types.Interface, typesPackage *types.Package, name string) (int, *types.Func) {
	if name != "_" {
		for index := 0; index < interfaceType.NumMethods(); index++ {
			method := interfaceType.Method(index)
			if sameIdentifier(method, typesPackage, name) {
				return index, method
			}
		}
	}
	return -1, nil
}

// sameIdentifier is whether typesObject is spelled name and, unless it is exported, belongs to typesPackage: the
// original's sameId.
func sameIdentifier(typesObject types.Object, typesPackage *types.Package, name string) bool {
	// spec:
	// "Two identifiers are different if they are spelled differently,
	// or if they appear in different packages and are not exported.
	// Otherwise, they are the same."
	if name != typesObject.Name() {
		return false
	}
	// typesObject.Name() == name
	if typesObject.Exported() {
		return true
	}
	// not exported, so packages must be the same (typesPackage == nil for fields in Universe scope; this can only
	// happen for types introduced via Eval)
	if typesPackage == nil || typesObject.Pkg() == nil {
		return typesPackage == typesObject.Pkg()
	}
	// typesPackage != nil && typesObject.Pkg() != nil
	return typesPackage.Path() == typesObject.Pkg().Path()
}

// implements is whether candidate implements interfaceType, given candidate's method set, and the selections of the
// methods that make it: a concrete type uses each one (8.2), and the embedded fields each passes through (6.3). An
// interface that unifies a type parameter of a method with a type argument is implemented when the argument
// satisfies the parameter's constraint, a best-effort check that errs on accepting too many types.
func implements(
	candidate types.Type,
	interfaceType *types.Interface,
	candidateMethods *types.MethodSet,
) ([]*types.Selection, bool) {
	// fast path for common case
	if interfaceType.Empty() {
		return nil, true
	}

	if candidateInterface, _ := candidate.Underlying().(*types.Interface); candidateInterface != nil {
		// The original asks whether this code is reachable: entry never asks it of an interface.
		for method := range interfaceType.Methods() {
			_, found := lookupMethod(candidateInterface, method.Pkg(), method.Name())
			switch {
			case found == nil:
				return nil, false
			case !types.Identical(found.Type(), method.Type()):
				return nil, false
			}
		}
		return nil, true
	}

	// A concrete type implements interfaceType if it implements all methods of interfaceType.
	var selections []*types.Selection

	mapping := map[*types.TypeParam]types.Type{}
	for method := range interfaceType.Methods() {
		selection := candidateMethods.Lookup(method.Pkg(), method.Name())
		if selection == nil {
			return nil, false
		}

		function, _ := selection.Obj().(*types.Func)
		if function == nil {
			return nil, false
		}

		if unified := typeutil.Unify(function.Type(), method.Type(), mapping); !unified {
			return nil, false
		}

		selections = append(selections, selection)
	}
	for parameter, argument := range mapping {
		// This checks constraints on a best-effort basis, erring on the side of accepting too many types.
		if !satisfiesConstraint(argument, parameter) {
			return nil, false
		}
	}
	return selections, true
}

// satisfiesConstraint is whether argument satisfies parameter's constraint, or argument is nil, as it is when two type
// parameters were unified.
func satisfiesConstraint(argument types.Type, parameter *types.TypeParam) bool {
	if argument == nil {
		// argument is nil when we unify two type parameters.
		return true
	}
	bound, isInterface := parameter.Constraint().Underlying().(*types.Interface)
	if !isInterface {
		// A constraint's underlying type is always an interface; the original asserts it, and the port accepts, as
		// the check errs on accepting.
		return true
	}
	return types.Satisfies(argument, bound)
}
