package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// correctnessNoProcessExitAfterOutputWriters answers whether a call writes, directly or through one
// callee, remembering each callee's answer for the file.
type correctnessNoProcessExitAfterOutputWriters struct {
	ctx           rule.Context
	byDeclaration map[*ast.Node]bool
}

// writes says whether a call writes to stdout or stderr itself, or runs a callee whose own body does.
func (writers correctnessNoProcessExitAfterOutputWriters) writes(call *ast.Node) bool {
	if correctnessNoProcessExitAfterOutputIsWrite(writers.ctx, call) {
		return true
	}
	callee := writers.followedCallee(call)
	if callee == nil {
		return false
	}
	answer, known := writers.byDeclaration[callee]
	if !known {
		answer = correctnessNoProcessExitAfterOutputBodyWrites(writers.ctx, callee)
		writers.byDeclaration[callee] = answer
	}
	return answer
}

// followedCallee is the function a call runs, when the rule reads its body: see the rule's doc
// comment for each refusal. Nil means the call is not followed.
func (writers correctnessNoProcessExitAfterOutputWriters) followedCallee(call *ast.Node) *ast.Node {
	signature := writers.ctx.TypeChecker.GetResolvedSignature(call)
	if signature == nil {
		return nil
	}
	declaration := signature.Declaration()
	if declaration == nil {
		return nil
	}
	switch declaration.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration:
	default:
		return nil
	}
	if declaration.Body() == nil {
		return nil
	}
	file := ast.GetSourceFileOfNode(declaration)
	// A declaration file needs no test of its own: it has no bodies, so the check above refused it
	if file == nil || (file != writers.ctx.SourceFile && !ast.IsExternalModule(file)) {
		return nil
	}
	flags := ast.GetFunctionFlags(declaration)
	if flags&ast.FunctionFlagsGenerator != 0 {
		return nil
	}
	if flags&ast.FunctionFlagsAsync != 0 && !correctnessNoProcessExitAfterOutputIsAwaited(call) {
		return nil
	}
	returnType := writers.ctx.TypeChecker.GetReturnTypeOfSignature(signature)
	if returnType != nil && returnType.Flags()&checker.TypeFlagsNever != 0 {
		return nil
	}
	return declaration
}
