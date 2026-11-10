package check

import (
	"cmp"
	"go/types"
	"slices"
	"strings"
)

// closureRendering is what an importer can observe of object, another package's, written out so that two builds
// whose types render alike for it give it the same lines, the reading an extra build compares with the host's to
// tell whether a package that read object reads the same there (#b2t9xmf). It errs wide, as exportRendering does: a
// rendering that changes when it need not costs the reading package a recheck, while one that stays the same when it
// should change restores a stale analysis.
//
//   - The object's own line: its kind, package, name and type, a constant with its exact value, a field with whether
//     it is embedded, a method with its receiver.
//   - Every named type and alias reachable from its type, each once, sorted by package path, name and rendering: its
//     path and name, then its lines as exportRendering writes them (renderedTypeName), which hold its type parameters
//     with their constraints, its underlying type with every field, unexported ones too, and every method with its
//     signature. The walk follows each into what those lines name (closureTypeNames), so a struct two levels down
//     whose unexported field differs by target changes the rendering of every object that reaches it.
//
// A named type of no package, the universe's error or comparable, is the same in every build, and is left out.
func closureRendering(object types.Object) []string {
	lines := []string{ownRendering(object)}
	roots := []types.Type{object.Type()}
	if function, isFunction := object.(*types.Func); isFunction && function.Signature().Recv() != nil {
		roots = append(roots, function.Signature().Recv().Type())
	}
	type renderedName struct {
		path  string
		name  string
		lines []string
	}
	reached := closureTypeNames(roots)
	rendered := make([]renderedName, 0, len(reached))
	for _, typeName := range reached {
		rendered = append(rendered, renderedName{path: typeName.Pkg().Path(), name: typeName.Name(),
			lines: renderedTypeName(typeName)})
	}
	// Two of one path and name are local types of two functions, which no package-level type can name, so they are
	// rare; their lines settle the order, never their positions, which differ by build.
	slices.SortFunc(rendered, func(left, right renderedName) int {
		return cmp.Or(cmp.Compare(left.path, right.path), cmp.Compare(left.name, right.name),
			slices.Compare(left.lines, right.lines))
	})
	for _, typeName := range rendered {
		lines = append(lines, "named "+typeName.path+"."+typeName.name)
		lines = append(lines, typeName.lines...)
	}
	return lines
}

// ownRendering is the line of object itself in its closureRendering.
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

// closureTypeNames are the named types and aliases reachable from roots, each once, in the order the walk meets them:
// through pointers, slices, arrays, maps, channels, signatures (type parameters and their constraints, parameters,
// results), struct fields, unexported ones too, interface methods and embedded types, union terms and instance type
// arguments, and from each named type into its type parameters' constraints, its underlying type and its methods'
// signatures, and from each alias into its type parameters' constraints, the type its declaration names and the type
// it stands for. A type of no package is left out, and so is its walk: the universe's are the same in every build.
func closureTypeNames(roots []types.Type) []*types.TypeName {
	seen := map[*types.TypeName]bool{}
	var reached []*types.TypeName
	var walk func(walked types.Type)
	walkParameters := func(parameters *types.TypeParamList) {
		for parameter := range parameters.TypeParams() {
			walk(parameter.Constraint())
		}
	}
	// A signature's receiver is left alone: a method's is the type that declares it, walked already, and an interface
	// method's is the interface, which the walk is inside of.
	walkSignature := func(signature *types.Signature) {
		walkParameters(signature.RecvTypeParams())
		walkParameters(signature.TypeParams())
		walk(signature.Params())
		walk(signature.Results())
	}
	visit := func(typeName *types.TypeName) {
		if typeName.Pkg() == nil || seen[typeName] {
			return
		}
		seen[typeName] = true
		reached = append(reached, typeName)
		switch declared := typeName.Type().(type) {
		case *types.Named:
			walkParameters(declared.TypeParams())
			walk(declared.Underlying())
			for method := range declared.Methods() {
				walkSignature(method.Signature())
			}
		case *types.Alias:
			walkParameters(declared.TypeParams())
			walk(declared.Rhs())
			walk(types.Unalias(declared))
		default:
			walk(declared)
		}
	}
	walk = func(walked types.Type) {
		switch composite := walked.(type) {
		case *types.Named:
			visit(composite.Origin().Obj())
			for argument := range composite.TypeArgs().Types() {
				walk(argument)
			}
		case *types.Alias:
			visit(composite.Origin().Obj())
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
