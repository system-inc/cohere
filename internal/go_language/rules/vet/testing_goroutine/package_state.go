package testing_goroutine

import (
	"go/ast"
	"go/types"
)

// packageState is what the port keeps over one package: what it reads once, on the first node heard, since the
// listener has no hook as the walk enters the package, which declarations take a test, and the regions found.
type packageState struct {
	// isRead is whether importsTesting, reportsSubtest and declarationOf have been set.
	isRead bool
	// importsTesting is whether the package imports the testing package. One that does not calls none of its
	// methods by its own hand, and is not judged.
	importsTesting bool
	// reportsSubtest is the subtest option.
	reportsSubtest bool
	// declarationOf is the declaration in the package of each function and method it declares.
	declarationOf func(*types.Func) *ast.FuncDecl
	// takesTest is, for each function declaration a heard node sits in, whether its parameters take a *testing.T or
	// a *testing.B, worked out once.
	takesTest map[*ast.FuncDecl]bool
	// startedAt maps each region whose statements run concurrently with some test to the asynchronous calls that
	// start it. A test helper may be started from several places, so one region may have several.
	startedAt map[ast.Node][]*asynchronousCall
	// regions holds the regions in the order they were first found, the order the original reports in.
	regions []ast.Node
}

// add records that started starts its region, and nothing for a nil started.
func (state *packageState) add(started *asynchronousCall) {
	if started == nil {
		return
	}
	if state.startedAt[started.region] == nil {
		state.regions = append(state.regions, started.region)
	}
	state.startedAt[started.region] = append(state.startedAt[started.region], started)
}

// isPruned reports whether the original's walk never reaches a node whose ancestors are stack: one within a function
// declaration whose parameters take no *testing.T or *testing.B, which the original does not descend into, so what it
// starts is never judged. A function declaration is always a file's own child, so it is the node's enclosing one when
// it is the stack's second, just after the file, and the node is within none otherwise.
func (state *packageState) isPruned(stack []ast.Node) bool {
	if len(stack) < 2 {
		return false
	}
	declaration, isDeclaration := stack[1].(*ast.FuncDecl)
	if !isDeclaration {
		return false
	}
	takesTest, isKnown := state.takesTest[declaration]
	if !isKnown {
		takesTest = hasBenchmarkOrTestParameters(declaration)
		state.takesTest[declaration] = takesTest
	}
	return !takesTest
}
