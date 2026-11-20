package standing_presence

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// presenceEdge is one read of a function's body that a rule makes from another's, in the build's own syntax: caller,
// the declaration the read starts in, nil for a package-level initializer, and callee, the declaration whose body is
// read, with the reason of the row whose rule reads it.
type presenceEdge struct {
	caller *ast.FuncDecl
	callee *ast.FuncDecl
	reason fact_reach.Reason
}

// edgesLeaving are the bodies the edge rows take out of standing, each by the callee direction: a body the build holds
// present that starts a function of the package with go, or hands one to (*testing.T).Run by name, has that function's
// body read by the rule, so the function's body leaves. Present is what does not stand, what an earlier row takes out,
// the users of each object named and each body leaving, and, to a fixed point, each callee this takes out, whose own
// edges are then read too. A package-level initializer never stands, so an edge it starts is always read.
//
// The facts' fixed point runs after this (fact_reach.Settle), so a body it alone takes out is not read as present
// here: a body that leaves for a differing fact and starts a standing function leaves that function standing, until
// the edges are read again once the facts have settled.
func edgesLeaving(
	pass *analysis.Pass,
	host fact_reach.Host,
	standing map[fact_reach.Place]bool,
	named []fact_reach.Named,
	leaving []Leaving,
) []Leaving {
	declarations := functionDeclarations(pass)
	edges := slices.Concat(testingGoroutineEdges(pass, declarations), requireParallelEdges(pass, declarations))
	if len(edges) == 0 {
		return nil
	}
	left := map[fact_reach.Place]bool{}
	for _, body := range leaving {
		left[body.Place] = true
	}
	for _, object := range named {
		for _, user := range host.Users(object.Object) {
			left[user] = true
		}
	}
	isPresent := func(declaration *ast.FuncDecl) bool {
		if declaration == nil {
			return true
		}
		place := placeOf(pass, declaration)
		return !standing[place] || left[place]
	}
	var taken []Leaving
	for changed := true; changed; {
		changed = false
		for _, each := range edges {
			callee := placeOf(pass, each.callee)
			if !isPresent(each.caller) || !standing[callee] || left[callee] {
				continue
			}
			left[callee] = true
			taken = append(taken, Leaving{Place: callee, Reason: each.reason})
			changed = true
		}
	}
	return taken
}

// functionDeclarations are the declarations of the pass's files of each function and method they declare, by its
// object, as both edge rules look a callee's up (rules/vet/testing_goroutine/functions.go:11-30,
// rules/testing_require_parallel/analyzer.go:75-84).
func functionDeclarations(pass *analysis.Pass) map[*types.Func]*ast.FuncDecl {
	declarations := map[*types.Func]*ast.FuncDecl{}
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction {
				continue
			}
			if declared, isDeclared := pass.TypesInfo.Defs[function.Name].(*types.Func); isDeclared {
				declarations[declared] = function
			}
		}
	}
	return declarations
}

// placeOf is declaration's place as the standing set keys one (fact_reach.Place): the file a reader sees, past any
// `//line` directive, and the offset of its `func` in the file as parsed.
func placeOf(pass *analysis.Pass, declaration *ast.FuncDecl) fact_reach.Place {
	position := pass.Fset.Position(declaration.Pos())
	return fact_reach.Place{File: position.Filename, Offset: position.Offset}
}
