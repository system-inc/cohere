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
// are built unlifted, as go/ir's NaiveForm builds them, until lifting lands (#ee33njz), and without
// debug information, sanity checks, printing or monomorphized generics.
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
func NewProgram(fset *token.FileSet) *Program {
	return &Program{
		FileSet:  fset,
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
// Mutates: goEnginePackage gains the member, its value and, for a function, its created and Functions entries
func memberFromObject(goEnginePackage *Package, object types.Object, syntax ast.Node, goversion string) {
	name := object.Name()
	switch typedObject := object.(type) {
	case *types.Builtin:
		if goEnginePackage.Types != types.Unsafe {
			// Invariant: go/types declares builtins only in the universe scope and in package unsafe's scope, and
			// the objects given here come from a package's own declarations or scope.
			panic("unexpected builtin object: " + typedObject.String())
		}

	case *types.TypeName:
		if name != "_" {
			goEnginePackage.Members[name] = &Type{
				object:    typedObject,
				irPackage: goEnginePackage,
			}
		}

	case *types.Const:
		constant := &NamedConstant{
			object:    typedObject,
			Value:     NewConstant(typedObject.Val(), typedObject.Type(), syntax),
			irPackage: goEnginePackage,
		}
		goEnginePackage.values[typedObject] = constant
		if name != "_" {
			goEnginePackage.Members[name] = constant
		}

	case *types.Var:
		global := &Global{
			Package: goEnginePackage,
			name:    name,
			object:  typedObject,
			typ:     types.NewPointer(typedObject.Type()), // address
		}
		global.source = syntax
		goEnginePackage.values[typedObject] = global
		if name != "_" {
			goEnginePackage.Members[name] = global
		}

	case *types.Func:
		sig, isSignature := typedObject.Type().(*types.Signature)
		if !isSignature {
			// Invariant: go/types gives every *types.Func a *types.Signature type.
			panic(fmt.Sprintf(
				"memberFromObject: func %s has type %s, not a signature", typedObject, typedObject.Type()))
		}
		if sig.Recv() == nil && name == "init" {
			goEnginePackage.ninit++
			name = fmt.Sprintf("init#%d", goEnginePackage.ninit)
		}
		function := createFunction(goEnginePackage.Program, typedObject, name, syntax, goEnginePackage.info, goversion)
		function.Package = goEnginePackage
		goEnginePackage.created = append(goEnginePackage.created, function)
		goEnginePackage.values[typedObject] = function
		goEnginePackage.Functions = append(goEnginePackage.Functions, function)
		if name != "_" && sig.Recv() == nil {
			goEnginePackage.Members[name] = function // package-level function
		}

	default: // (incl. *types.Package)
		// Invariant: a package's scope and the definitions of its package-level declarations hold only constants,
		// variables, type names and functions, and builtins in package unsafe, each a case above.
		panic("unexpected Object type: " + typedObject.String())
	}
}

// createFunction creates a function or method. It supports both
// CreatePackage (with or without syntax) and the on-demand creation
// of methods in non-created packages based on their types.Func.
func createFunction(
	prog *Program,
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
		Program:        prog,
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
func membersFromDecl(goEnginePackage *Package, decl ast.Decl, goversion string) {
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
					memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], nil, "")
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
					goEnginePackage.initVersion[rhs] = goversion
				}
				for _, id := range valueSpec.Names {
					memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], spec, goversion)
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
				memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], nil, "")
			}
		}

	case *ast.FuncDecl:
		id := decl.Name
		memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], decl, goversion)
	}
}
