package core

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// maxDepthWalker carries the per-function depth stack across the recursive walk.
type maxDepthWalker struct {
	ctx      rule.Context
	settings MaxDepthSettings

	// depths is upstream's `functionStack`. Its top is the nesting depth in force inside the
	// innermost enclosing function, and entering a function pushes a fresh zero so a nested
	// function starts over rather than inheriting its parent's depth.
	depths []int
}

func (walker *maxDepthWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	pushedFunction := false
	pushedBlock := false

	switch node.Kind {
	// Upstream's startFunction set. `Program` is handled by the caller, since the walk starts
	// there.
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindClassStaticBlockDeclaration:
		walker.depths = append(walker.depths, 0)
		pushedFunction = true

	// Upstream's pushBlock set, with the else-if exemption folded into the IfStatement arm.
	case ast.KindIfStatement:
		if !maxDepthIsElseIf(node) {
			walker.pushBlock(node)
			pushedBlock = true
		}

	case ast.KindSwitchStatement, ast.KindTryStatement, ast.KindDoStatement,
		ast.KindWhileStatement, ast.KindWithStatement, ast.KindForStatement,
		ast.KindForInStatement, ast.KindForOfStatement:
		walker.pushBlock(node)
		pushedBlock = true

	// A class member's body is its own frame, and this arm is what makes that true here.
	//
	// Upstream needs no such arm because ESTree wraps every method body in a FunctionExpression,
	// which its startFunction set already covers, so the frame is pushed by the body rather than by
	// the member. Our parser hangs the body directly off the member -- probed, there is no wrapping
	// node at all -- so the frame has to be pushed here instead.
	//
	// The distinguishing shape is a class nested inside an already-deep block, and finding it took
	// a correction worth recording. The obvious argument -- "without a frame, two sibling methods
	// would accumulate depth" -- is wrong, because the depth decrements on the way out, so siblings
	// never accumulate whether or not a frame is pushed. A mutation deleting this arm survived both
	// the corpus and a first set of sibling-method fixtures written from that argument.
	//
	// What a frame actually does is reset the count, so it only shows where there is a count to
	// reset. Measured against the installed rule, which reports zero on every row:
	//
	//	function f() { if (a) { class C { m() { if (b) {} } } } }             limit 1, clean
	//	function f() { if (a) { if (b) { class C { m() { if (c) {} } } } } }  limit 2, clean
	//	function f() { if (a) { class C { get g() { if (b) {} } } } }         limit 1, clean
	//	function f() { if (a) { class C { constructor() { if (b) {} } } } }   limit 1, clean
	//
	// Without this arm the port reports all four, because the inner `if` inherits the enclosing
	// block's depth instead of starting over. Verified by deleting the arm and watching exactly
	// those four rows fail.
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor:
		walker.depths = append(walker.depths, 0)
		pushedFunction = true
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walk(child)
		return false
	})

	if pushedBlock {
		walker.depths[len(walker.depths)-1]--
	}
	if pushedFunction {
		walker.depths = walker.depths[:len(walker.depths)-1]
	}
}

// pushBlock is upstream's `pushBlock`: increment the current function's depth and report if the new
// depth exceeds the limit.
//
// Reporting on the way in rather than on the way out is upstream's order and it matters for a file
// with several findings, since it decides the sequence they arrive in.
func (walker *maxDepthWalker) pushBlock(node *ast.Node) {
	if len(walker.depths) == 0 {
		return
	}
	walker.depths[len(walker.depths)-1]++
	depth := walker.depths[len(walker.depths)-1]
	if depth <= walker.settings.Maximum {
		return
	}

	// The keyword alone, which is upstream's `getFirstToken(node).loc`. See the span section on the
	// rule for why `rule.TokenRange` is the wrong helper.
	tokenRange := scanner.GetRangeOfTokenAtPosition(walker.ctx.SourceFile, node.Pos())
	walker.ctx.Report(rule.Diagnostic{
		Range:      tokenRange,
		SourceFile: walker.ctx.SourceFile,
		Message: rule.Message{
			Id: messageMaxDepthTooDeeply.Id,
			Description: fmt.Sprintf("Blocks are nested too deeply (%d). Maximum allowed is %d. %s",
				depth, walker.settings.Maximum, messageMaxDepthTooDeeply.Description),
		},
	})
}
