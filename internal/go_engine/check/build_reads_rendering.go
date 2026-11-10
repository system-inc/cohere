package check

import (
	"go/types"
	"strings"
)

// ownRendering is the line of object itself in its closure digest (ReadsBuild.closureDigest).
func ownRendering(object types.Object) string {
	owner := "universe"
	if object.Pkg() != nil {
		owner = object.Pkg().Path()
	}
	switch declared := object.(type) {
	case *types.Var:
		kind := "var"
		switch {
		case declared.Embedded():
			kind = "embedded field"
		case declared.IsField():
			kind = "field"
		}
		return kind + " of " + owner + " " + declared.Name() + " " + writtenType(declared.Type())
	case *types.Func:
		receiver := "none"
		if declared.Signature().Recv() != nil {
			receiver = writtenType(declared.Signature().Recv().Type())
		}
		return "func of " + owner + " " + declared.Name() + " receiver " + receiver +
			renderedTypeParameters(declared.Signature().TypeParams()) + " " + writtenType(declared.Type())
	case *types.TypeName:
		return "type name of " + owner + " " + declared.Name()
	default:
		// A constant's line holds its exact value; any other kind is written by its kind, name and type.
		return "of " + owner + " " + strings.Join(renderedObject(object), " ")
	}
}

// rootTypes are the types object's closure starts from: its type, and a method's receiver type, which a signature's
// walk leaves alone.
func rootTypes(object types.Object) []types.Type {
	roots := []types.Type{object.Type()}
	if function, isFunction := object.(*types.Func); isFunction && function.Signature().Recv() != nil {
		roots = append(roots, function.Signature().Recv().Type())
	}
	return roots
}

// directTypeNames are the named types and aliases roots name, each once, in the order a walk meets them: through
// pointers, slices, arrays, maps, channels, signatures (type parameters and their constraints, parameters, results),
// struct fields, unexported ones too, interface methods and embedded types, union terms and instance type arguments.
// The walk stops at each named type and alias, whose declaration is a node of its own (declarationTypes), so a closure
// is the named types these reach, and what their declarations reach in turn. A type of no package is left out: the
// universe's are the same in every build.
func directTypeNames(roots []types.Type) []*types.TypeName {
	seen := map[*types.TypeName]bool{}
	var reached []*types.TypeName
	var walk func(walked types.Type)
	walkParameters := func(parameters *types.TypeParamList) {
		for parameter := range parameters.TypeParams() {
			walk(parameter.Constraint())
		}
	}
	// A signature's receiver is left alone: a method's is the type that declares it, a node already, and an interface
	// method's is the interface, which the walk is inside of.
	walkSignature := func(signature *types.Signature) {
		walkParameters(signature.RecvTypeParams())
		walkParameters(signature.TypeParams())
		walk(signature.Params())
		walk(signature.Results())
	}
	reach := func(typeName *types.TypeName) {
		if typeName.Pkg() != nil && !seen[typeName] {
			seen[typeName] = true
			reached = append(reached, typeName)
		}
	}
	walk = func(walked types.Type) {
		switch composite := walked.(type) {
		case *types.Named:
			reach(composite.Origin().Obj())
			for argument := range composite.TypeArgs().Types() {
				walk(argument)
			}
		case *types.Alias:
			reach(composite.Origin().Obj())
			for argument := range composite.TypeArgs().Types() {
				walk(argument)
			}
		case *types.Pointer:
			walk(composite.Elem())
		case *types.Slice:
			walk(composite.Elem())
		case *types.Array:
			walk(composite.Elem())
		case *types.Chan:
			walk(composite.Elem())
		case *types.Map:
			walk(composite.Key())
			walk(composite.Elem())
		case *types.Signature:
			walkSignature(composite)
		case *types.Tuple:
			for variable := range composite.Variables() {
				walk(variable.Type())
			}
		case *types.Struct:
			for field := range composite.Fields() {
				walk(field.Type())
			}
		case *types.Interface:
			for method := range composite.ExplicitMethods() {
				walkSignature(method.Signature())
			}
			for embedded := range composite.EmbeddedTypes() {
				walk(embedded)
			}
		case *types.Union:
			for index := range composite.Len() {
				walk(composite.Term(index).Type())
			}
		}
		// A basic type is the same in every build, and a type parameter's constraint is walked where it is declared,
		// in its type's or signature's list, never at each use, where a constraint naming its own parameter would walk
		// without end.
	}
	for _, root := range roots {
		walk(root)
	}
	return reached
}

// declarationTypes are the types typeName's declaration names, which its rendered lines write (renderedTypeName): a
// defined type's type parameters' constraints, its underlying type and its methods' signatures, and an alias's type
// parameters' constraints, the type its declaration names and the type it stands for.
func declarationTypes(typeName *types.TypeName) []types.Type {
	var named []types.Type
	constraints := func(parameters *types.TypeParamList) {
		for parameter := range parameters.TypeParams() {
			named = append(named, parameter.Constraint())
		}
	}
	switch declared := typeName.Type().(type) {
	case *types.Named:
		constraints(declared.TypeParams())
		named = append(named, declared.Underlying())
		for method := range declared.Methods() {
			named = append(named, method.Signature())
		}
	case *types.Alias:
		constraints(declared.TypeParams())
		named = append(named, declared.Rhs(), types.Unalias(declared))
	default:
		named = append(named, declared)
	}
	return named
}
