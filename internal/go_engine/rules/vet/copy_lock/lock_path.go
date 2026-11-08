package copy_lock

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// lockerType is sync.Locker's method set, `interface{ Lock(); Unlock() }`, built here rather than looked up, since
// a package need not import sync to declare a lock of its own.
var lockerType = newLockerType()

// newLockerType builds lockerType. x/tools builds it in copylock.go's init.
func newLockerType() *types.Interface {
	nullary := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	return types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, nil, "Lock", nullary),
		types.NewFunc(token.NoPos, nil, "Unlock", nullary),
	}, nil).Complete()
}

// lockPathOfValue is the path to a lock in the value value denotes, nil when it holds none or makes a new value
// rather than copying one: a composite literal, or a call, which may return a zero value, or a pointer to one
// through `*`. Parentheses are looked through. x/tools' lockPathRhs.
func lockPathOfValue(context *rule.Context, value ast.Expr) typePath {
	unparenthesized := ast.Unparen(value)
	switch made := unparenthesized.(type) {
	case *ast.CompositeLit, *ast.CallExpr:
		return nil
	case *ast.StarExpr:
		if _, isCall := ast.Unparen(made.X).(*ast.CallExpr); isCall {
			return nil
		}
	}
	// A type or a package name is no value, and Types holds nothing for an expression the checker could not type.
	if typeAndValue, isTyped := context.TypesInfo.Types[unparenthesized]; isTyped && typeAndValue.IsValue() {
		return lockPath(typeAndValue.Type)
	}
	return nil
}

// lockPath is the path to a lock contained in typed, nil when it contains none. x/tools' lockPath, less its package
// parameter, which it never reads.
func lockPath(typed types.Type) typePath {
	return lockPathWithin(typed, map[types.Type]bool{})
}

// lockPathWithin is lockPath for a type met inside one already being walked, with seen holding each type met so
// far, so a type that contains itself, which only code that does not type-check declares, ends the walk rather than
// recursing forever.
//
// Mutates: seen recording typed as met, since the walk of every type typed holds shares the one map.
func lockPathWithin(typed types.Type, seen map[types.Type]bool) typePath {
	if typed == nil || seen[typed] {
		return nil
	}
	seen[typed] = true

	if typeParameter, isTypeParameter := types.Unalias(typed).(*types.TypeParam); isTypeParameter {
		// A type parameter holds a lock when a type its constraint permits does. An invalid constraint, an empty
		// type set and a constraint permitting every type all have no terms.
		for _, term := range structuralTerms(typeParameter) {
			path := lockPathWithin(term.Type(), seen)
			if len(path) == 0 {
				continue
			}
			if term.Tilde() {
				// The tilde makes the message say the containment is through an approximation element. Without it,
				// `func _[Mutex interface{ ~sync.Mutex; M() }](mutex Mutex) {}` would read "Mutex contains
				// sync.Mutex", when sync.Mutex lacks M and is no Mutex.
				path[len(path)-1] = "~" + path[len(path)-1]
			}
			return append(path, typed.String())
		}
		return nil
	}

	// An array copies its elements, so an array of locks is judged as its element, and the path names the element.
	element := typed
	for {
		array, isArray := element.Underlying().(*types.Array)
		if !isArray {
			break
		}
		element = array.Elem()
	}

	// A tuple is the type of a comma-ok read, `value, present := table[key]`, and copies each of its values.
	if tuple, isTuple := element.Underlying().(*types.Tuple); isTuple {
		for variable := range tuple.Variables() {
			if path := lockPathWithin(variable.Type(), seen); path != nil {
				return append(path, element.String())
			}
		}
		return nil
	}

	// Only a struct can hold a lock by value: an interface or a pointer is safe to copy.
	structure, isStructure := element.Underlying().(*types.Struct)
	if !isStructure {
		return nil
	}

	// A lock is a type whose pointer is a sync.Locker and whose value is not, which tells a struct embedding a lock
	// by value from one embedding an interface.
	if types.Implements(types.NewPointer(element), lockerType) && !types.Implements(element, lockerType) {
		return typePath{element.String()}
	}

	// Before Go 1.11 sync.noCopy had no Unlock method, so it is recognized by name too. The original keeps the
	// workaround, so the port does, though every toolchain cohere supports gives noCopy both methods.
	if isTypeNamed(element, "sync", "noCopy") {
		return typePath{element.String()}
	}

	for index := range structure.NumFields() {
		if path := lockPathWithin(structure.Field(index).Type(), seen); path != nil {
			return append(path, element.String())
		}
	}
	return nil
}

// isTypeNamed reports whether typed is, or is an alias of, the package-level defined type name declares in the
// package at packagePath. x/tools' typesinternal.IsTypeNamed, with one name rather than several.
func isTypeNamed(typed types.Type, packagePath string, name string) bool {
	named, isNamed := types.Unalias(typed).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	// x/tools' typesinternal.IsPackageLevel: the name is declared in its package's own scope.
	return typeName != nil && typeName.Pkg() != nil && typeName.Parent() == typeName.Pkg().Scope() &&
		typeName.Pkg().Path() == packagePath && typeName.Name() == name
}
