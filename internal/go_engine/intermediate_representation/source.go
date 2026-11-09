// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines utilities for working with source positions
// or source-level named entities ("objects").

// TODO(adonovan): test that {Value,Instruction}.Pos() positions match
// the originating syntax, as specified.

import (
	"go/ast"
	"go/token"
	"go/types"
)

// EnclosingFunction returns the function that contains the syntax
// node denoted by path.
//
// Syntax associated with package-level variable specifications is
// enclosed by the package's init() function.
//
// Returns nil if not found; reasons might include:
//   - the node is not enclosed by any function.
//   - the node is within an anonymous function (FuncLit) and
//     its IR function has not been created yet
//     (pkg.Build() has not yet been called).
func EnclosingFunction(goEnginePackage *Package, path []ast.Node) *Function {
	// Start with package-level function...
	function := findEnclosingPackageLevelFunction(goEnginePackage, path)
	if function == nil {
		return nil // not in any function
	}

	// ...then walk down the nested anonymous functions.
	pathLength := len(path)
outer:
	for index := range path {
		if lit, ok := path[pathLength-1-index].(*ast.FuncLit); ok {
			for _, anon := range function.AnonymousFunctions {
				if anon.Pos() == lit.Type.Func {
					function = anon
					continue outer
				}
			}
			// IR function not found:
			// - package not yet built, or maybe
			// - builder skipped FuncLit in dead block
			//   (in principle; but currently the Builder
			//   generates even dead FuncLits).
			return nil
		}
	}
	return function
}

// HasEnclosingFunction returns true if the AST node denoted by path
// is contained within the declaration of some function or
// package-level variable.
//
// Unlike EnclosingFunction, the behaviour of this function does not
// depend on whether IR code for pkg has been built, so it can be
// used to quickly reject check inputs that will cause
// EnclosingFunction to fail, prior to IR building.
func HasEnclosingFunction(goEnginePackage *Package, path []ast.Node) bool {
	return findEnclosingPackageLevelFunction(goEnginePackage, path) != nil
}

// findEnclosingPackageLevelFunction returns the Function
// corresponding to the package-level function enclosing path.
func findEnclosingPackageLevelFunction(goEnginePackage *Package, path []ast.Node) *Function {
	if pathLength := len(path); pathLength >= 2 { // [... {Gen,Func}Decl File]
		switch decl := path[pathLength-2].(type) {
		case *ast.GenDecl:
			if decl.Tok == token.VAR && pathLength >= 3 {
				// Package-level 'var' initializer.
				return goEnginePackage.init
			}

		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name.Name == "init" {
				// Explicit init() function.
				for _, block := range goEnginePackage.init.Blocks {
					for _, instr := range block.Instructions {
						if instr, ok := instr.(*Call); ok {
							if callee, ok := instr.Call.Value.(*Function); ok &&
								callee.Package == goEnginePackage &&
								callee.Pos() == decl.Name.NamePos {
								return callee
							}
						}
					}
				}
				// Hack: return non-nil when IR is not yet
				// built so that HasEnclosingFunction works.
				return goEnginePackage.init
			}
			// Declared function/method.
			return findNamedFunc(goEnginePackage, decl.Name.NamePos)
		}
	}
	return nil // not in any function
}

// findNamedFunc returns the named function whose FuncDecl.Ident is at
// position pos.
func findNamedFunc(goEnginePackage *Package, position token.Pos) *Function {
	// Look at all package members and method sets of named types.
	// Not very efficient.
	for _, mem := range goEnginePackage.Members {
		switch mem := mem.(type) {
		case *Function:
			if mem.Pos() == position {
				return mem
			}
		case *Type:
			mset := goEnginePackage.Program.MethodSets.MethodSet(types.NewPointer(mem.Type()))
			for index, length := 0, mset.Len(); index < length; index++ {
				// Don't call Program.Method: avoid creating wrappers.
				object := mset.At(index).Obj().(*types.Func)
				if object.Pos() == position {
					// obj from MethodSet may not be the origin type.
					function := object.Origin()
					return goEnginePackage.values[function].(*Function)
				}
			}
		}
	}
	return nil
}

// --- Lookup functions for source-level named entities (types.Objects) ---
