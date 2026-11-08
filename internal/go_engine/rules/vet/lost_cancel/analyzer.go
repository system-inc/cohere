// Package lost_cancel is vet's lostcancel pass, a cancel function returned by context.WithCancel, WithTimeout,
// WithDeadline or one of their Cause variants that is discarded, or that some path from its definition to a return
// never uses, so the context it cancels stays live until its parent is. Any reference to the cancel variable counts as
// its use, even one inside a nested function literal, and a naked return counts as a use of a named result. A cancel
// variable declared outside the function is assumed to be used elsewhere, and main.main is not judged, since
// returning from it ends the process.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/lostcancel/lostcancel.go, whose
// logic it keeps and whose messages it reports word for word, at the same positions. It requires what the original
// requires, inspect and x/tools' public ctrlflow, and searches the control-flow graph ctrlflow builds for each
// function, the graph that already knows which calls never return, in this package and, through ctrlflow's facts, in
// its dependencies, so the port walks the same paths the original walks. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
//
// The port reports a function's cancel variables in the order their definitions are found, where the original
// ranges over a map, so its findings come out in a stable order; the findings themselves are the same. The
// original's debug printing of each graph is left out.
package lost_cancel

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/cfg"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "lostcancel",
	Doc: "check cancel func returned by context.WithCancel is called\n\n" +
		"The cancellation function returned by context.WithCancel, WithTimeout,\n" +
		"WithDeadline and variants such as WithCancelCause must be called,\n" +
		"or the new context will remain live until its parent context is cancelled.\n" +
		"(The background context is never cancelled.)",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/lostcancel",
	Requires: []*analysis.Analyzer{inspect.Analyzer, ctrlflow.Analyzer},
	Run:      run,
}

// contextPackage is the import path of the package whose functions return a cancel function.
const contextPackage = "context"

func run(pass *analysis.Pass) (any, error) {
	// A package that imports no context calls none of its functions, so none of its functions is walked.
	if !imports(pass.Pkg, contextPackage) {
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("lost_cancel: inspect's result is missing")
	}
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	if !isGraphs {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("lost_cancel: ctrlflow's result is missing")
	}
	// PreorderSeq visits each function declaration and literal in the order the original's Preorder does, a literal
	// after the function that holds it, and each is judged on its own.
	for function := range inspected.PreorderSeq((*ast.FuncLit)(nil), (*ast.FuncDecl)(nil)) {
		judgeFunction(pass, graphs, function)
	}
	return nil, nil
}

// judgeFunction judges one function declaration or literal: each cancel function its own body discards is reported,
// and each cancel variable it defines that some path to a return never uses is reported at its definition and at
// that return. x/tools' lostcancel.runFunc.
func judgeFunction(pass *analysis.Pass, graphs *ctrlflow.CFGs, function ast.Node) {
	// scope is the function's own scope, opened at its type, which holds its parameters, its results and its body.
	var scope *types.Scope
	switch declared := function.(type) {
	case *ast.FuncLit:
		scope = pass.TypesInfo.Scopes[declared.Type]
	case *ast.FuncDecl:
		scope = pass.TypesInfo.Scopes[declared.Type]
	}
	variables, definitions := cancelVariables(pass, scope, function)
	if len(variables) == 0 {
		// Nothing to search for, so the graph is never read.
		return
	}

	var graph *cfg.CFG
	var signature *types.Signature
	switch declared := function.(type) {
	case *ast.FuncDecl:
		// The original reads the declaration's type through Defs without a check, and would panic where Defs held no
		// object for its name. A type-checked declaration always has one, so the port, which judges nothing then,
		// never differs from it.
		declaredFunction, isFunction := pass.TypesInfo.Defs[declared.Name].(*types.Func)
		if !isFunction {
			return
		}
		signature = declaredFunction.Signature()
		if signature.Recv() == nil && declared.Name.Name == "main" && pass.Pkg.Name() == "main" {
			// Returning from main.main terminates the process, so there's no need to cancel contexts.
			return
		}
		graph = graphs.FuncDecl(declared)
	case *ast.FuncLit:
		literalSignature, isSignature := pass.TypesInfo.Types[declared.Type].Type.(*types.Signature)
		if !isSignature {
			// Missing type information, which the original also leaves unjudged.
			return
		}
		signature = literalSignature
		graph = graphs.FuncLit(declared)
	}

	for _, variable := range variables {
		definition := definitions[variable]
		returned := lostCancelPath(pass.TypesInfo, graph, variable, definition, signature)
		if returned == nil {
			continue
		}
		line := pass.Fset.Position(definition.Pos()).Line
		pass.ReportRangef(definition,
			"the %s function is not used on all paths (possible context leak)", variable.Name())
		// golang/go#64547: the return cfg makes explicit at a function's closing brace is synthetic, and its end, six
		// bytes on for the word return, may overflow the file, so such a return is reported as the point it starts at.
		end := returned.End()
		if pass.Fset.File(returned.Pos()) != pass.Fset.File(end) {
			end = returned.Pos()
		}
		pass.Report(analysis.Diagnostic{
			Pos: returned.Pos(),
			End: end,
			Message: fmt.Sprintf(
				"this return statement may be reached without using the %s var defined on line %d",
				variable.Name(), line,
			),
		})
	}
}

// cancelVariables walks function, short of the function literals inside it, for each call of one of context's
// functions that return a cancel function whose result is defined or assigned by a statement of two or more names:
// a cancel function discarded with the blank identifier is reported there, and a cancel variable defined in the
// function is returned, in the order first found, with the statement that last defines it, the one the search starts
// from. scope is function's own scope, and a cancel variable declared outside it is left alone, assumed to be used
// elsewhere. The first half of x/tools' lostcancel.runFunc.
func cancelVariables(
	pass *analysis.Pass,
	scope *types.Scope,
	function ast.Node,
) (variables []*types.Var, definitions map[*types.Var]ast.Node) {
	definitions = map[*types.Var]ast.Node{}
	// record keeps the statement that last defines variable, as the original's map does, and the order variables are
	// first found in.
	record := func(variable *types.Var, statement ast.Node) {
		if _, recorded := definitions[variable]; !recorded {
			variables = append(variables, variable)
		}
		definitions[variable] = statement
	}
	ast.PreorderStack(function, nil, func(node ast.Node, stack []ast.Node) bool {
		if _, isLiteral := node.(*ast.FuncLit); isLiteral && len(stack) > 0 {
			// A nested function literal is judged on its own, when run's walk reaches it.
			return false
		}
		// The selector called, beneath its call and the statement that holds the call:
		//
		//   ctx, cancel    := context.WithCancel(...)
		//   ctx, cancel     = context.WithCancel(...)
		//   var ctx, cancel = context.WithCancel(...)
		//
		// The original asks only that the selector's parent be a call, not that the selector be what it calls, so a
		// selector passed as an argument, `ctx, cancel := apply(context.WithCancel)`, is judged too, as here. The
		// selector is never function itself, and its parent call never a function's root, so stack holds both.
		selector := contextWithCancel(pass.TypesInfo, node)
		if selector == nil || !isCall(stack[len(stack)-1]) {
			return true
		}
		statement := stack[len(stack)-2]
		var identifier *ast.Ident
		switch defining := statement.(type) {
		case *ast.ValueSpec:
			if len(defining.Names) > 1 {
				identifier = defining.Names[1]
			}
		case *ast.AssignStmt:
			if len(defining.Lhs) > 1 {
				// A cancel function assigned to a field or an element, `_, holder.cancel = ...`, names no variable and
				// is left alone.
				if assigned, isIdentifier := defining.Lhs[1].(*ast.Ident); isIdentifier {
					identifier = assigned
				}
			}
		}
		if identifier == nil {
			return true
		}
		if identifier.Name == "_" {
			pass.ReportRangef(identifier,
				"the cancel function returned by context.%s should be called, not discarded, to avoid a context leak",
				selector.Sel.Name)
			return true
		}
		// Uses holds a variable assigned with `=`, and Defs one defined with `:=` or `var`. A variable assigned
		// whose declaration lies outside function is left alone, without falling back to Defs, as in the original.
		if used, isVariable := pass.TypesInfo.Uses[identifier].(*types.Var); isVariable {
			if scope.Contains(used.Pos()) {
				record(used, statement)
			}
		} else if defined, isVariable := pass.TypesInfo.Defs[identifier].(*types.Var); isVariable {
			record(defined, statement)
		}
		return true
	})
	return variables, definitions
}

// isCall reports whether node is a call. x/tools' lostcancel.isCall.
func isCall(node ast.Node) bool {
	_, isCallExpression := node.(*ast.CallExpr)
	return isCallExpression
}

// contextWithCancel is node when it is one of the qualified identifiers context.With{Cancel,Timeout,Deadline} or
// their Cause variants, and nil otherwise. x/tools' lostcancel.isContextWithCancel, returning the selector the
// original asserts node to be when it reports a discard.
func contextWithCancel(information *types.Info, node ast.Node) *ast.SelectorExpr {
	selector, isSelector := node.(*ast.SelectorExpr)
	if !isSelector {
		return nil
	}
	switch selector.Sel.Name {
	case "WithCancel", "WithCancelCause",
		"WithTimeout", "WithTimeoutCause",
		"WithDeadline", "WithDeadlineCause":
	default:
		return nil
	}
	qualifier, isIdentifier := selector.X.(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	if packageName, isPackageName := information.Uses[qualifier].(*types.PkgName); isPackageName {
		if packageName.Imported().Path() != contextPackage {
			return nil
		}
		return selector
	}
	// The original's heuristic for an import that failed, whose package path can't be checked: the local name
	// alone. It also takes a variable named context, of a type with a method of one of these names, as the package.
	if qualifier.Name != "context" {
		return nil
	}
	return selector
}

// lostCancelPath is a return statement, perhaps the synthetic one cfg makes at a function's closing brace, that some
// path through graph from definition, the statement that defines variable, reaches without using variable, and nil
// when every path uses it. signature is the function's type. x/tools' lostcancel.lostCancelPath.
func lostCancelPath(
	information *types.Info,
	graph *cfg.CFG,
	variable *types.Var,
	definition ast.Node,
	signature *types.Signature,
) *ast.ReturnStmt {
	isNamedResult := tupleContains(signature.Results(), variable)

	// The definition's block in the graph, and the rest of that block's statements after it.
	var definingBlock *cfg.Block
	var rest []ast.Node
	for _, block := range graph.Blocks {
		if index := slices.Index(block.Nodes, definition); index >= 0 {
			definingBlock = block
			rest = block.Nodes[index+1:]
			break
		}
	}
	if definingBlock == nil {
		// Invariant: cfg places every statement of a function's body, and each var specification of a declaration in
		// it, in some block, reachable or not, and the definition is one of them.
		panic("lost_cancel: can't find defining block for cancel var")
	}

	// Is variable used in the remainder of its defining block?
	if usesVariable(information, variable, isNamedResult, rest) {
		return nil
	}
	// Does the defining block return without using variable?
	if returned := definingBlock.Return(); returned != nil {
		return returned
	}

	// Search the graph depth-first, successors in order, for a path from the defining block to a return block on
	// which variable is never used. A block is visited once, so whether it uses variable is asked of it once, which
	// is why the original's cache of the answer is left out.
	seen := map[*cfg.Block]bool{}
	var search func(blocks []*cfg.Block) *ast.ReturnStmt
	search = func(blocks []*cfg.Block) *ast.ReturnStmt {
		for _, block := range blocks {
			if seen[block] {
				continue
			}
			seen[block] = true
			// Prune the search where the block uses variable.
			if usesVariable(information, variable, isNamedResult, block.Nodes) {
				continue
			}
			// Found a path to a return statement?
			if returned := block.Return(); returned != nil {
				return returned
			}
			if returned := search(block.Succs); returned != nil {
				return returned
			}
		}
		return nil
	}
	return search(definingBlock.Succs)
}

// usesVariable reports whether nodes refer to variable anywhere, nested function literals included, or, when
// variable is a named result, hold a naked return, which reads it. The uses closure in x/tools'
// lostcancel.lostCancelPath.
func usesVariable(information *types.Info, variable *types.Var, isNamedResult bool, nodes []ast.Node) bool {
	found := false
	for _, node := range nodes {
		ast.Inspect(node, func(inspected ast.Node) bool {
			switch visited := inspected.(type) {
			case *ast.Ident:
				if information.Uses[visited] == variable {
					found = true
				}
			case *ast.ReturnStmt:
				// A naked return statement counts as a use of the named result variables.
				if visited.Results == nil && isNamedResult {
					found = true
				}
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// tupleContains reports whether variable is one of tuple's. x/tools' lostcancel.tupleContains.
func tupleContains(tuple *types.Tuple, variable *types.Var) bool {
	for member := range tuple.Variables() {
		if member == variable {
			return true
		}
	}
	return false
}

// imports reports whether importing imports the package at path directly. x/tools' typesinternal.Imports.
func imports(importing *types.Package, path string) bool {
	for _, dependency := range importing.Imports() {
		if dependency.Path() == path {
			return true
		}
	}
	return false
}
