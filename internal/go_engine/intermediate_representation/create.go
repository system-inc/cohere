// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file implements the create phase of IR construction.
// See builder.go for explanation.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// NewProgram returns a new IR Program. go/ir takes a BuilderMode here; ours has none. Its functions
// are lifted (#ee33njz), as go/ir builds them without NaiveForm, and built without sanity checks,
// printing or monomorphized generics; a package records its expression map only with SetDebugMode.
//
// To construct an SSA program:
//
//   - Call NewProgram to create an empty Program.
//   - Call CreatePackage providing typed syntax for each package
//     you want to build, and call it with types but not
//     syntax for each of those package's direct dependencies.
//   - Call [Package.Build] on each syntax package you wish to build,
//     or [Program.Build] to build all of them.
//
// See the Example tests for simple examples.
func NewProgram(fileSet *token.FileSet) *Program {
	return &Program{
		FileSet:  fileSet,
		imported: make(map[string]*Package),
		packages: make(map[*types.Package]*Package),
		canon:    newCanonizer(),
		ctxt:     types.NewContext(),
	}
}

// memberFromObject populates package pkg with a member for the
// typechecker object obj.
//
// For objects from Go source code, syntax is the associated syntax tree
// (for funcs and vars only) and goversion defines the appropriate
// interpretation; they will be used during the build phase.
//
// Mutates: irPackage gains the member, its value and, for a function, its created and Functions entries
func memberFromObject(irPackage *Package, object types.Object, syntax ast.Node, goversion string) {
	name := object.Name()
	switch typedObject := object.(type) {
	case *types.Builtin:
		if irPackage.Types != types.Unsafe {
			// Invariant: go/types declares builtins only in the universe scope and in package unsafe's scope, and
			// the objects given here come from a package's own declarations or scope.
			panic("unexpected builtin object: " + typedObject.String())
		}

	case *types.TypeName:
		if name != "_" {
			irPackage.members[name] = &Type{
				object:    typedObject,
				irPackage: irPackage,
			}
		}

	case *types.Const:
		constant := &NamedConstant{
			object:    typedObject,
			Value:     NewConstant(typedObject.Val(), typedObject.Type(), syntax),
			irPackage: irPackage,
		}
		irPackage.values[typedObject] = constant
		if name != "_" {
			irPackage.members[name] = constant
		}

	case *types.Var:
		global := &Global{
			Package:   irPackage,
			name:      name,
			object:    typedObject,
			valueType: types.NewPointer(typedObject.Type()), // address
		}
		global.source = syntax
		irPackage.values[typedObject] = global
		if name != "_" {
			irPackage.members[name] = global
		}

	case *types.Func:
		sig, isSignature := typedObject.Type().(*types.Signature)
		if !isSignature {
			// Invariant: go/types gives every *types.Func a *types.Signature type.
			panic(fmt.Sprintf(
				"memberFromObject: func %s has type %s, not a signature", typedObject, typedObject.Type()))
		}
		if sig.Recv() == nil && name == "init" {
			irPackage.ninit++
			name = fmt.Sprintf("init#%d", irPackage.ninit)
		}
		function := createFunction(irPackage.Program, typedObject, name, syntax, irPackage.info, goversion)
		function.Package = irPackage
		irPackage.created = append(irPackage.created, function)
		irPackage.values[typedObject] = function
		irPackage.Functions = append(irPackage.Functions, function)
		if name != "_" && sig.Recv() == nil {
			irPackage.members[name] = function // package-level function
		}

	default: // (incl. *types.Package)
		// Invariant: a package's scope and the definitions of its package-level declarations hold only constants,
		// variables, type names and functions, and builtins in package unsafe, each a case above.
		panic("unexpected Object type: " + typedObject.String())
	}
}

// forEachScopeMember calls create with each object a package created from its types has a member for, in go/ir's
// order: every object of its scope, each named type's methods after it.
func forEachScopeMember(packageTypes *types.Package, create func(object types.Object)) {
	scope := packageTypes.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		create(object)
		if typeName, ok := object.(*types.TypeName); ok {
			// No Unalias: aliases should not duplicate methods.
			if named, ok := typeName.Type().(*types.Named); ok {
				for index, methodCount := 0, named.NumMethods(); index < methodCount; index++ {
					create(named.Method(index))
				}
			}
		}
	}
}

// createFunction creates a function or method. It supports both
// CreatePackage (with or without syntax) and the on-demand creation
// of methods in non-created packages based on their types.Func.
func createFunction(
	program *Program,
	object *types.Func,
	name string,
	syntax ast.Node,
	info *types.Info,
	goversion string,
) *Function {
	sig, isSignature := object.Type().(*types.Signature)
	if !isSignature {
		// Invariant: go/types gives every *types.Func a *types.Signature type.
		panic(fmt.Sprintf("createFunction: func %s has type %s, not a signature", object, object.Type()))
	}

	/* declared function/method (from syntax or export data) */
	function := &Function{
		name:           name,
		object:         object,
		Signature:      sig,
		build:          (*builder).buildFromSyntax,
		info:           info,
		goversion:      goversion,
		position:       object.Pos(),
		syntax:         syntax,
		Package:        nil, // may be set by caller
		Program:        program,
		recvtypeparams: sig.RecvTypeParams(),
		typeparams:     sig.TypeParams(),
	}
	if syntax == nil {
		function.Synthetic = "from type information"
		function.build = (*builder).buildParamsOnly
	}
	if function.hasTypeParameters() {
		function.generic = new(generic)
	}
	return function
}

// membersFromDecl populates package pkg with members for each
// typechecker object (var, func, const or type) associated with the
// specified decl.
func membersFromDecl(irPackage *Package, decl ast.Decl, goversion string) {
	switch decl := decl.(type) {
	case *ast.GenDecl: // import, const, type or var
		switch decl.Tok {
		case token.CONST:
			for _, spec := range decl.Specs {
				valueSpec, isValueSpec := spec.(*ast.ValueSpec)
				if !isValueSpec {
					// Invariant: go/ast holds a *ast.ValueSpec for each spec of a const declaration.
					panic(fmt.Sprintf("membersFromDecl: const declaration holds %T, not a value spec", spec))
				}
				for _, id := range valueSpec.Names {
					memberFromObject(irPackage, irPackage.info.Defs[id], nil, "")
				}
			}

		case token.VAR:
			for _, spec := range decl.Specs {
				valueSpec, isValueSpec := spec.(*ast.ValueSpec)
				if !isValueSpec {
					// Invariant: go/ast holds a *ast.ValueSpec for each spec of a var declaration.
					panic(fmt.Sprintf("membersFromDecl: var declaration holds %T, not a value spec", spec))
				}
				for _, rhs := range valueSpec.Values {
					irPackage.initVersion[rhs] = goversion
				}
				for _, id := range valueSpec.Names {
					memberFromObject(irPackage, irPackage.info.Defs[id], spec, goversion)
				}
			}

		case token.TYPE:
			for _, spec := range decl.Specs {
				typeSpec, isTypeSpec := spec.(*ast.TypeSpec)
				if !isTypeSpec {
					// Invariant: go/ast holds a *ast.TypeSpec for each spec of a type declaration.
					panic(fmt.Sprintf("membersFromDecl: type declaration holds %T, not a type spec", spec))
				}
				id := typeSpec.Name
				memberFromObject(irPackage, irPackage.info.Defs[id], nil, "")
			}
		}

	case *ast.FuncDecl:
		id := decl.Name
		memberFromObject(irPackage, irPackage.info.Defs[id], decl, goversion)
	}
}
