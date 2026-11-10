package unused_code

import (
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/analysis/lint"
	"honnef.co/go/tools/go/types/typeutil"
)

// embeddedField sees the field declared by the embedded field syntax, and marks the type as used by the field. It is
// nil only where the original would panic, should the identifier ever name no field.
//
// Embedded fields are special in two ways: they don't have names, so we don't have immediate access to an ast.Ident to
// resolve to the field's types.Var and need to instead walk the syntax, and we cannot use graph.read on the type
// because eventually we do get to an ast.Ident, and ObjectOf resolves embedded fields to the field they declare, not
// the type. That's why we have code specially for handling embedded fields.
func (graph *graph) embeddedField(syntax ast.Node, by types.Object) *types.Var {
	// We need to traverse the tree to find the ast.Ident, but all the nodes we traverse should be used by the object we
	// get once we resolve the identifier. Collect the nodes and process them once we've found the identifier.
	pending := make([]ast.Node, 0, 4)
	// current is where the traversal is. The original reassigns its parameter; the house never does.
	current := syntax
	for {
		switch step := current.(type) {
		case *ast.Ident:
			// field is the field
			field, isField := graph.typesInfo.ObjectOf(step).(*types.Var)
			if !isField {
				return nil
			}
			// the field is declared by the enclosing type
			graph.see(field, by)
			for _, each := range pending {
				graph.read(each, field)
			}

			if typeName, isTypeName := graph.typesInfo.Uses[step].(*types.TypeName); isTypeName && typeName.IsAlias() {
				// When embedding an alias we want to use the alias, not what the alias points to.
				graph.use(typeName, field)
			} else {
				switch stepType := typeutil.Dereference(graph.typesInfo.TypeOf(step)).(type) {
				case *types.Named:
					// (7.2) fields use their types
					graph.use(stepType.Obj(), field)
				case *types.Basic:
					// Nothing to do
				default:
					// Other types are only possible for aliases, which we've already handled
					lint.ExhaustiveTypeSwitch(stepType)
				}
			}
			return field
		case *ast.StarExpr:
			current = step.X
		case *ast.SelectorExpr:
			current = step.Sel
			pending = append(pending, step.X)
		case *ast.IndexExpr:
			current = step.X
			pending = append(pending, step.Index)
		case *ast.IndexListExpr:
			current = step.X
		default:
			lint.ExhaustiveTypeSwitch(step)
		}
	}
}

// isNoCopyType reports whether a type represents the NoCopy sentinel type. The NoCopy type is a named struct with no
// fields and exactly one method `func Lock()` that is empty. It also takes a second method, `func Unlock()`, and the
// original notes it does not yet check that the function body is empty.
func isNoCopyType(goType types.Type) bool {
	structType, isStruct := goType.Underlying().(*types.Struct)
	if !isStruct {
		return false
	}
	if structType.NumFields() != 0 {
		return false
	}

	named, isNamed := types.Unalias(goType).(*types.Named)
	if !isNamed {
		return false
	}
	switch count := named.NumMethods(); count {
	case 1, 2:
		for index := range count {
			method := named.Method(index)
			if method.Name() != "Lock" && method.Name() != "Unlock" {
				return false
			}
			signature, isSignature := method.Type().(*types.Signature)
			if !isSignature || signature.Params().Len() != 0 || signature.Results().Len() != 0 {
				return false
			}
		}
	default:
		return false
	}
	return true
}

// namedType records what a named type uses of the type it is declared as, specification.
func (graph *graph) namedType(typeName *types.TypeName, specification ast.Expr) {
	// (2.2) named types use the type they're based on

	structSyntax, isStruct := specification.(*ast.StructType)
	if !isStruct {
		graph.read(specification, typeName)
		return
	}

	hasHostLayout := false

	// Named structs are special in that their unexported fields are only used if they're being written to. That is,
	// the fields are not used by the named type itself, nor are the types of the fields.
	for _, field := range structSyntax.Fields.List {
		seen := map[*types.Struct]struct{}{}
		// For `type x struct { *x; F int }`, don't visit the embedded x
		if declaredStruct, isDeclaredStruct := graph.typesInfo.TypeOf(structSyntax).(*types.Struct); isDeclaredStruct {
			seen[declaredStruct] = struct{}{}
		}
		var hasExportedField func(goType types.Type) bool
		hasExportedField = func(goType types.Type) bool {
			structType, isStructType := typeutil.Dereference(goType).Underlying().(*types.Struct)
			if !isStructType {
				return false
			}
			if _, isSeen := seen[structType]; isSeen {
				return false
			}
			seen[structType] = struct{}{}
			for inner := range structType.Fields() {
				if inner.Exported() {
					return true
				}
				if inner.Embedded() && hasExportedField(inner.Type()) {
					return true
				}
			}
			return false
		}

		if len(field.Names) == 0 {
			if embedded := graph.embeddedField(field.Type, typeName); embedded != nil {
				// DefaultOptions' ExportedIsUsed.
				if token.IsExported(embedded.Name()) {
					// (6.2) structs use exported fields
					graph.use(embedded, typeName)
				}
				// DefaultOptions' ExportedIsUsed and ExportedFieldsAreUsed.
				if hasExportedField(embedded.Type()) {
					// (6.5) structs use embedded structs that have exported fields (recursively)
					graph.use(embedded, typeName)
				}
			}
		} else {
			for _, name := range field.Names {
				declared := graph.typesInfo.ObjectOf(name)
				graph.see(declared, typeName)
				// (7.2) fields use their types
				//
				// This handles aliases correctly because ObjectOf(alias) returns the TypeName of the alias, not what
				// the alias points to.
				graph.read(field.Type, declared)
				if name.Name == "_" {
					// (9.9) objects named the blank identifier are used
					graph.use(declared, typeName)
				} else if token.IsExported(name.Name) {
					// (6.2) structs use exported fields: DefaultOptions' ExportedIsUsed.
					graph.use(declared, typeName)
				}

				if isNoCopyType(declared.Type()) {
					// (6.1) structs use fields of type NoCopy sentinel
					graph.use(declared, typeName)
				}
			}
		}

		// (6.6) if the struct has a field of type structs.HostLayout, then this signals that all fields are relevant
		// to match some externally specified memory layout.
		//
		// This augments the 5.2 heuristic of using all fields when converting via unsafe.Pointer. For example, 5.2
		// doesn't currently handle conversions involving more than one level of pointer indirection (although it
		// probably should). Another example that doesn't involve the use of unsafe at all is exporting symbols for
		// use by C libraries.
		//
		// The actual requirements for the use of structs.HostLayout fields haven't been determined yet. It's an open
		// question whether named types of underlying type structs.HostLayout, aliases of it, generic instantiations,
		// or embedding structs that themselves contain a HostLayout field count as valid uses of the marker (see
		// https://golang.org/issues/66408#issuecomment-2120644459)
		//
		// For now, we require a struct to have a field of type structs.HostLayout or an alias of it, where the field
		// itself may be embedded. We don't handle fields whose types are type parameters.
		if fieldType, isNamed := types.Unalias(graph.typesInfo.TypeOf(field.Type)).(*types.Named); isNamed {
			if fieldType.Obj().Name() == "HostLayout" && fieldType.Obj().Pkg().Path() == "structs" {
				hasHostLayout = true
			}
		}
	}

	// For 6.6.
	if hasHostLayout {
		graph.useAllFieldsRecursively(typeName.Type(), typeName)
	}
}
