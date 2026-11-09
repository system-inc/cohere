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
func EnclosingFunction(pkg *Package, path []ast.Node) *Function {
	// Start with package-level function...
	fn := findEnclosingPackageLevelFunction(pkg, path)
	if fn == nil {
		return nil // not in any function
	}

	// ...then walk down the nested anonymous functions.
	n := len(path)
outer:
	for i := range path {
		if lit, ok := path[n-1-i].(*ast.FuncLit); ok {
			for _, anon := range fn.AnonFuncs {
				if anon.Pos() == lit.Type.Func {
					fn = anon
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
	return fn
}

// HasEnclosingFunction returns true if the AST node denoted by path
// is contained within the declaration of some function or
// package-level variable.
//
// Unlike EnclosingFunction, the behaviour of this function does not
// depend on whether IR code for pkg has been built, so it can be
// used to quickly reject check inputs that will cause
// EnclosingFunction to fail, prior to IR building.
func HasEnclosingFunction(pkg *Package, path []ast.Node) bool {
	return findEnclosingPackageLevelFunction(pkg, path) != nil
}

// findEnclosingPackageLevelFunction returns the Function
// corresponding to the package-level function enclosing path.
func findEnclosingPackageLevelFunction(pkg *Package, path []ast.Node) *Function {
	if n := len(path); n >= 2 { // [... {Gen,Func}Decl File]
		switch decl := path[n-2].(type) {
		case *ast.GenDecl:
			if decl.Tok == token.VAR && n >= 3 {
				// Package-level 'var' initializer.
				return pkg.init
			}

		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name.Name == "init" {
				// Explicit init() function.
				for _, b := range pkg.init.Blocks {
					for _, instr := range b.Instructions {
						if instr, ok := instr.(*Call); ok {
							if callee, ok := instr.Call.Value.(*Function); ok && callee.Package == pkg && callee.Pos() == decl.Name.NamePos {
								return callee
							}
						}
					}
				}
				// Hack: return non-nil when IR is not yet
				// built so that HasEnclosingFunction works.
				return pkg.init
			}
			// Declared function/method.
			return findNamedFunc(pkg, decl.Name.NamePos)
		}
	}
	return nil // not in any function
}

// findNamedFunc returns the named function whose FuncDecl.Ident is at
// position pos.
func findNamedFunc(pkg *Package, pos token.Pos) *Function {
	// Look at all package members and method sets of named types.
	// Not very efficient.
	for _, mem := range pkg.Members {
		switch mem := mem.(type) {
		case *Function:
			if mem.Pos() == pos {
				return mem
			}
		case *Type:
			mset := pkg.Program.MethodSets.MethodSet(types.NewPointer(mem.Type()))
			for i, n := 0, mset.Len(); i < n; i++ {
				// Don't call Program.Method: avoid creating wrappers.
				obj := mset.At(i).Obj().(*types.Func)
				if obj.Pos() == pos {
					// obj from MethodSet may not be the origin type.
					m := obj.Origin()
					return pkg.values[m].(*Function)
				}
			}
		}
	}
	return nil
}

// --- Lookup functions for source-level named entities (types.Objects) ---

// Package returns the IR Package corresponding to the specified
// type-checker package object.
// It returns nil if no such IR package has been created.
func (prog *Program) Package(obj *types.Package) *Package {
	return prog.packages[obj]
}

// packageLevelMember returns the package-level member corresponding
// to the specified symbol, which may be a package-level const
// (*NamedConst), var (*Global) or func/method (*Function) of some
// package in prog.
//
// It returns nil if the object belongs to a package that has not been
// created by prog.CreatePackage.
func (prog *Program) packageLevelMember(obj types.Object) Member {
	if pkg, ok := prog.packages[obj.Pkg()]; ok {
		return pkg.values[obj]
	}
	return nil
}

// FuncValue returns the SSA function or (non-interface) method
// denoted by the specified func symbol. It returns nil if the symbol
// denotes an interface method, or belongs to a package that was not
// created by prog.CreatePackage.
func (prog *Program) FunctionValue(obj *types.Func) *Function {
	fn, _ := prog.packageLevelMember(obj).(*Function)
	return fn
}

// ConstValue returns the IR Value denoted by the source-level named
// constant obj.
func (prog *Program) ConstValue(obj *types.Const) *Constant {
	// TODO(adonovan): opt: share (don't reallocate)
	// Consts for const objects and constant ast.Exprs.

	// Universal constant? {true,false,nil}
	if obj.Parent() == types.Universe {
		return NewConstant(obj.Val(), obj.Type(), nil)
	}
	// Package-level named constant?
	if v := prog.packageLevelMember(obj); v != nil {
		return v.(*NamedConst).Value
	}
	return NewConstant(obj.Val(), obj.Type(), nil)
}
