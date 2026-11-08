package testing_goroutine

import (
	"go/ast"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// judge reports each forbidden call region makes, once for each call in startedAt that starts it, on a receiver not
// declared within that call's scope. The region's walk is the original's, all but the arguments the goroutine that
// starts a region evaluates, which callerEvaluated names: those run where the go statement or t.Run runs, so they are
// judged as part of the region enclosing it, and in none when that is the test itself, rather than as part of the
// region they start. The original walks them with the started region, and so reports a test's own call, such as the
// `t.FailNow()` in `go fmt.Println(func() int { t.FailNow(); return 0 }())`, as made from another goroutine.
func judge(pass *analysis.Pass, region ast.Node, startedAt map[ast.Node][]*asynchronousCall) {
	var visit func(node ast.Node) bool
	visit = func(node ast.Node) bool {
		if startedAt[node] != nil {
			// A region within this one is judged on its own, all but what this region's goroutine evaluates to start
			// it, which is judged here.
			for _, argument := range callerEvaluated(node) {
				inspectEvaluated(argument, visit)
			}
			return false
		}
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		receiver, selection, method := forbiddenMethod(pass.TypesInfo, call)
		if receiver == nil {
			return true
		}
		for _, started := range startedAt[region] {
			if !withinScope(started.scope, receiver) {
				report(pass, started, call, receiver, formatMethod(selection, method))
			}
		}
		return true
	}
	evaluatedByStarter := callerEvaluated(region)
	ast.Inspect(region, func(node ast.Node) bool {
		if node == region {
			// The region itself is always descended into.
			return true
		}
		if argument, isExpression := node.(ast.Expr); isExpression && slices.Contains(evaluatedByStarter, argument) {
			// The goroutine that starts region evaluates this argument, all but the function literals it hands over,
			// which the started goroutine may well call, and which the original judges here.
			for _, literal := range handedOver(argument) {
				ast.Inspect(literal, visit)
			}
			return false
		}
		return visit(node)
	})
}

// callerEvaluated is what the goroutine executing node evaluates before what node starts runs, when node is a region
// that is itself the go statement or t.Run that starts it: a go statement's call arguments, and a t.Run's name. A go
// statement whose callee is a literal, a variable first assigned one or a declared function starts that callee, and a
// t.Run whose function is a literal or a variable first assigned one starts that literal, and neither region holds
// the arguments, so nil.
func callerEvaluated(node ast.Node) []ast.Expr {
	switch started := node.(type) {
	case *ast.GoStmt:
		return started.Call.Args
	case *ast.CallExpr:
		// Only a t.Run call is a region that is a call, and it takes two arguments, its name first.
		if len(started.Args) == 0 {
			return nil
		}
		return started.Args[:1]
	}
	return nil
}

// inspectEvaluated walks argument with visit, as the goroutine evaluating it does, so passing over the function
// literals it hands over, whose bodies it does not run.
func inspectEvaluated(argument ast.Expr, visit func(ast.Node) bool) {
	handed := handedOver(argument)
	ast.Inspect(argument, func(node ast.Node) bool {
		if literal, isLiteral := node.(*ast.FuncLit); isLiteral && slices.Contains(handed, literal) {
			return false
		}
		return visit(node)
	})
}

// handedOver is each function literal within argument that is not called where it is written, outermost first: a
// value handed on to whatever is called, rather than code evaluating argument runs. A literal called in place, as in
// `func() int { t.FailNow(); return 0 }()`, runs as argument is evaluated, and is looked into.
func handedOver(argument ast.Expr) []*ast.FuncLit {
	calledInPlace := map[*ast.FuncLit]bool{}
	handed := []*ast.FuncLit{}
	ast.Inspect(argument, func(node ast.Node) bool {
		switch visited := node.(type) {
		case *ast.CallExpr:
			// A call is visited before its callee, so the literal it calls is marked before it is reached.
			if literal, isLiteral := ast.Unparen(visited.Fun).(*ast.FuncLit); isLiteral {
				calledInPlace[literal] = true
			}
		case *ast.FuncLit:
			if !calledInPlace[visited] {
				handed = append(handed, visited)
				return false
			}
		}
		return true
	})
	return handed
}
