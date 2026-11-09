// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines utilities for working with source positions
// or source-level named entities ("objects").

// No test checks that {Value,Instruction}.Pos() positions match
// the originating syntax, as specified.

import (
	"fmt"
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
func EnclosingFunction(irPackage *Package, path []ast.Node) *Function {
	// Start with package-level function...
	function := findEnclosingPackageLevelFunction(irPackage, path)
	if function == nil {
		return nil // not in any function
	}

	// ...then walk down the nested anonymous functions.
	pathLength := len(path)
	for index := range path {
		if lit, ok := path[pathLength-1-index].(*ast.FuncLit); ok {
			function = anonymousFunctionAt(function, lit)
			if function == nil {
				// IR function not found:
				// - package not yet built, or maybe
				// - builder skipped FuncLit in dead block
				//   (in principle; but currently the Builder
				//   generates even dead FuncLits).
				return nil
			}
		}
	}
	return function
}

// anonymousFunctionAt is the anonymous function of function that literal declares, found by the position of its
// func keyword, or nil when function holds none there.
func anonymousFunctionAt(function *Function, literal *ast.FuncLit) *Function {
	for _, anonymous := range function.AnonymousFunctions {
		if anonymous.Pos() == literal.Type.Func {
			return anonymous
		}
	}
	return nil
}

// HasEnclosingFunction returns true if the AST node denoted by path
// is contained within the declaration of some function or
// package-level variable.
//
// Unlike EnclosingFunction, the behaviour of this function does not
// depend on whether IR code for pkg has been built, so it can be
// used to quickly reject check inputs that will cause
// EnclosingFunction to fail, prior to IR building.
func HasEnclosingFunction(irPackage *Package, path []ast.Node) bool {
	return findEnclosingPackageLevelFunction(irPackage, path) != nil
}

// findEnclosingPackageLevelFunction returns the Function
// corresponding to the package-level function enclosing path.
func findEnclosingPackageLevelFunction(irPackage *Package, path []ast.Node) *Function {
	if pathLength := len(path); pathLength >= 2 { // [... {Gen,Func}Decl File]
		switch decl := path[pathLength-2].(type) {
		case *ast.GenDecl:
			if decl.Tok == token.VAR && pathLength >= 3 {
				// Package-level 'var' initializer.
				return irPackage.init
			}

		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name.Name == "init" {
				// Explicit init() function.
				for _, block := range irPackage.init.Blocks {
					for _, instr := range block.Instructions {
						if instr, ok := instr.(*Call); ok {
							if callee, ok := instr.Call.Value.(*Function); ok &&
								callee.Package == irPackage &&
								callee.Pos() == decl.Name.NamePos {
								return callee
							}
						}
					}
				}
				// Hack: return non-nil when IR is not yet
				// built so that HasEnclosingFunction works.
				return irPackage.init
			}
			// Declared function/method.
			return findNamedFunc(irPackage, decl.Name.NamePos)
		}
	}
	return nil // not in any function
}

// findNamedFunc returns the named function whose FuncDecl.Ident is at
// position pos.
func findNamedFunc(irPackage *Package, position token.Pos) *Function {
	// Look at all package members and method sets of named types.
	// Not very efficient.
	for _, mem := range irPackage.Members() {
		switch mem := mem.(type) {
		case *Function:
			if mem.Pos() == position {
				return mem
			}
		case *Type:
			mset := irPackage.Program.MethodSets.MethodSet(types.NewPointer(mem.Type()))
			for index, length := 0, mset.Len(); index < length; index++ {
				// Don't call Program.Method: avoid creating wrappers.
				object, isFunc := mset.At(index).Obj().(*types.Func)
				if !isFunc {
					// Invariant: every entry of a method set is a method selection, whose object is a *types.Func.
					panic(fmt.Sprintf("method set entry %s is not a function", mset.At(index)))
				}
				if object.Pos() == position {
					// obj from MethodSet may not be the origin type.
					function := object.Origin()
					declared, isFunction := irPackage.values[function].(*Function)
					if !isFunction {
						// Invariant: a method at a FuncDecl's name in this package was declared here, and
						// memberFromObject records every declared method in values as its *Function.
						panic(fmt.Sprintf(
							"method %s has no function in package %s",
							function,
							irPackage.Types.Path(),
						))
					}
					return declared
				}
			}
		}
	}
	return nil
}

// --- Lookup functions for source-level named entities (types.Objects) ---
