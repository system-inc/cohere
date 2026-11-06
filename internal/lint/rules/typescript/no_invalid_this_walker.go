package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noInvalidThisWalker carries the validity stack across the recursive walk.
type noInvalidThisWalker struct {
	ctx      rule.Context
	settings noInvalidThisSettings

	// thisIsValid mirrors typescript-eslint's `thisIsValidStack`. Its top is the binding in force
	// for the `this` being visited, and an empty stack means top level, where a module's `this` is
	// `undefined` and therefore invalid.
	thisIsValid []bool
}

func (walker *noInvalidThisWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	pushed := false
	switch node.Kind {
	case ast.KindThisKeyword:
		if !walker.currentlyValid() {
			walker.ctx.ReportNode(node, messageUnexpectedThis)
		}

	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		// The TypeScript contribution: a declared `this` parameter binds `this` by contract, and
		// the checker enforces it at every call site.
		walker.thisIsValid = append(walker.thisIsValid,
			declaresThisParameter(node) || !thisIsDefaultBoundIn(walker.ctx, node, walker.settings))
		pushed = true

	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindConstructor, ast.KindClassStaticBlockDeclaration:
		// A method's `this` is the receiver and a static block's is the class. Upstream reaches
		// these through the same default-binding test, which answers false for all of them.
		walker.thisIsValid = append(walker.thisIsValid, true)
		pushed = true

	case ast.KindPropertyDeclaration:
		// A field initializer is an implicit function whose `this` is the instance. Upstream pushes
		// true for PropertyDefinition and AccessorProperty alike; `accessor` is a modifier here
		// rather than a separate node, so both arrive as this kind.
		walker.thisIsValid = append(walker.thisIsValid, true)
		pushed = true

	case ast.KindArrowFunction:
		// Deliberately no push. An arrow has no `this` of its own, so the enclosing binding stays
		// in force and a `this` inside it is judged exactly as one outside it would be.
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walk(child)
		return false
	})

	if pushed {
		walker.thisIsValid = walker.thisIsValid[:len(walker.thisIsValid)-1]
	}
}

// currentlyValid reads the top of the stack.
//
// An empty stack is top level. Every file cohere lints is a TypeScript module, where top-level
// `this` is `undefined`, so the answer there is false. Core reaches the same answer through
// `node.sourceType === "module"`.
func (walker *noInvalidThisWalker) currentlyValid() bool {
	if len(walker.thisIsValid) == 0 {
		return false
	}
	return walker.thisIsValid[len(walker.thisIsValid)-1]
}
