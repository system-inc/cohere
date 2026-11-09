// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file implements the CREATE phase of IR construction.
// See builder.go for explanation.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"go/version"
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
func memberFromObject(goEnginePackage *Package, object types.Object, syntax ast.Node, goversion string) {
	name := object.Name()
	switch obj := object.(type) {
	case *types.Builtin:
		if goEnginePackage.Types != types.Unsafe {
			panic("unexpected builtin object: " + obj.String())
		}

	case *types.TypeName:
		if name != "_" {
			goEnginePackage.Members[name] = &Type{
				object: obj,
				pkg:    goEnginePackage,
			}
		}

	case *types.Const:
		constant := &NamedConstant{
			object: obj,
			Value:  NewConstant(obj.Val(), obj.Type(), syntax),
			pkg:    goEnginePackage,
		}
		goEnginePackage.values[obj] = constant
		if name != "_" {
			goEnginePackage.Members[name] = constant
		}

	case *types.Var:
		global := &Global{
			Package: goEnginePackage,
			name:    name,
			object:  obj,
			typ:     types.NewPointer(obj.Type()), // address
		}
		global.source = syntax
		goEnginePackage.values[obj] = global
		if name != "_" {
			goEnginePackage.Members[name] = global
		}

	case *types.Func:
		sig := obj.Type().(*types.Signature)
		if sig.Recv() == nil && name == "init" {
			goEnginePackage.ninit++
			name = fmt.Sprintf("init#%d", goEnginePackage.ninit)
		}
		function := createFunction(goEnginePackage.Program, obj, name, syntax, goEnginePackage.info, goversion)
		function.Package = goEnginePackage
		goEnginePackage.created = append(goEnginePackage.created, function)
		goEnginePackage.values[obj] = function
		goEnginePackage.Functions = append(goEnginePackage.Functions, function)
		if name != "_" && sig.Recv() == nil {
			goEnginePackage.Members[name] = function // package-level function
		}

	default: // (incl. *types.Package)
		panic("unexpected Object type: " + obj.String())
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
	sig := object.Type().(*types.Signature)

	/* declared function/method (from syntax or export data) */
	function := &Function{
		name:           name,
		object:         object,
		Signature:      sig,
		build:          (*builder).buildFromSyntax,
		info:           info,
		goversion:      goversion,
		pos:            object.Pos(),
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
				for _, id := range spec.(*ast.ValueSpec).Names {
					memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], nil, "")
				}
			}

		case token.VAR:
			for _, spec := range decl.Specs {
				for _, rhs := range spec.(*ast.ValueSpec).Values {
					goEnginePackage.initVersion[rhs] = goversion
				}
				for _, id := range spec.(*ast.ValueSpec).Names {
					memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], spec, goversion)
				}
			}

		case token.TYPE:
			for _, spec := range decl.Specs {
				id := spec.(*ast.TypeSpec).Name
				memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], nil, "")
			}
		}

	case *ast.FuncDecl:
		id := decl.Name
		memberFromObject(goEnginePackage, goEnginePackage.info.Defs[id], decl, goversion)
	}
}

// CreatePackage creates and returns an IR Package from the
// specified type-checked, error-free file ASTs, and populates its
// Members mapping.
//
// importable determines whether this package should be returned by a
// subsequent call to ImportedPackage(pkg.Path()).
//
// The real work of building IR form for each function is not done
// until a subsequent call to Package.Build.
func (prog *Program) CreatePackage(
	goPackage *types.Package,
	files []*ast.File,
	info *types.Info,
	importable bool,
) *Package {
	if goPackage == nil {
		panic("nil pkg") // otherwise pkg.Scope below returns types.Universe!
	}
	p := &Package{
		Program: prog,
		Members: make(map[string]Member),
		values:  make(map[types.Object]Member),
		Types:   goPackage,
		syntax:  info != nil,
		// transient values (cleared after Package.Build)
		info:        info,
		files:       files,
		initVersion: make(map[ast.Expr]string),
	}

	/* synthesized package initializer */
	p.init = &Function{
		name:      "init",
		Signature: new(types.Signature),
		Synthetic: "package initializer",
		Package:   p,
		Program:   prog,
		build:     (*builder).buildPackageInit,
		info:      p.info,
		goversion: "", // See Package.build for details.
	}
	p.Members[p.init.name] = p.init
	p.Functions = append(p.Functions, p.init)
	p.created = append(p.created, p.init)

	// Allocate all package members: vars, funcs, consts and types.
	if len(files) > 0 {
		// Go source package.
		for _, file := range files {
			goversion := version.Lang(fileGoVersion(p.info, file))
			for _, decl := range file.Decls {
				membersFromDecl(p, decl, goversion)
			}
		}
	} else {
		// GC-compiled binary package (or "unsafe")
		// No code.
		// No position information.
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			goObject := scope.Lookup(name)
			memberFromObject(p, goObject, nil, "")
			if typeName, ok := goObject.(*types.TypeName); ok {
				// No Unalias: aliases should not duplicate methods.
				if named, ok := typeName.Type().(*types.Named); ok {
					for index, n := 0, named.NumMethods(); index < n; index++ {
						memberFromObject(p, named.Method(index), nil, "")
					}
				}
			}
		}
	}

	// Add initializer guard variable.
	initguard := &Global{
		Package: p,
		name:    "init$guard",
		typ:     types.NewPointer(tBool),
	}
	p.Members[initguard.Name()] = initguard

	if importable {
		prog.imported[p.Types.Path()] = p
	}
	prog.packages[p.Types] = p

	return p
}

// AllPackages returns a new slice containing all packages created by
// prog.CreatePackage in unspecified order.
func (prog *Program) AllPackages() []*Package {
	pkgs := make([]*Package, 0, len(prog.packages))
	for _, goEnginePackage := range prog.packages {
		pkgs = append(pkgs, goEnginePackage)
	}
	return pkgs
}

// ImportedPackage returns the importable Package whose PkgPath
// is path, or nil if no such Package has been created.
//
// A parameter to CreatePackage determines whether a package should be
// considered importable. For example, no import declaration can resolve
// to the ad-hoc main package created by 'go build foo.go'.
//
// TODO(adonovan): rethink this function and the "importable" concept;
// most packages are importable. This function assumes that all
// types.Package.Path values are unique within the ir.Program, which is
// false---yet this function remains very convenient.
// Clients should use (*Program).Package instead where possible.
// IR doesn't really need a string-keyed map of packages.
//
// Furthermore, the graph of packages may contain multiple variants
// (e.g. "p" vs "p as compiled for q.test"), and each has a different
// view of its dependencies.
func (prog *Program) ImportedPackage(path string) *Package {
	return prog.imported[path]
}

// SetNoReturn sets the predicate used when building the ir.Program
// prog that reports whether a given function cannot return.
// This may be used to prune spurious control flow edges
// after (e.g.) log.Fatal, improving the precision of analyses.
//
// A typical implementation is the [ctrlflow.CFGs.NoReturn] method from
// [golang.org/x/tools/go/analysis/passes/ctrlflow].
func (prog *Program) SetNoReturn(predicate func(*types.Func) bool) {
	prog.noReturn = predicate
}
