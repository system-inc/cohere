package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noInvalidThisWalker carries the validity stack across the recursive walk.
type noInvalidThisWalker struct {
	ruleContext rule.Context
	settings    noInvalidThisSettings

	// thisIsValid's top is the binding in force for the `this` being visited, and an empty stack
	// means top level, where a module's `this` is `undefined` and therefore invalid.
	thisIsValid []bool
}

func (walker *noInvalidThisWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	pushed := false
	// popped records that a computed key temporarily removed its member's push, so the walk can
	// put it back on the way out. Restoring `true` rather than the saved value is correct because
	// only the member arms push here, and every one of them pushes true.
	popped := false
	switch node.Kind {
	case ast.KindThisKeyword:
		if !walker.currentlyValid() {
			walker.ruleContext.ReportNode(node, messageNoInvalidThisUnexpectedThis)
		}

	case ast.KindComputedPropertyName:
		// A computed key is evaluated when the class or object literal is defined, before any
		// instance exists, so the member that encloses it binds nothing: `class C { [this.a]; }`
		// throws. The walk has already pushed for that member, so the push has to be undone for
		// the key -- the member's value genuinely is valid and both live under the same node.
		//
		// Undoing is a pop rather than a forced report, and that distinction is load-bearing.
		// `class C { static { class D { [this.x]; } } }` is a passing corpus case: popping D's
		// member exposes the static block's binding underneath, which is valid. Forcing a report
		// here instead reported it, and forcing one is what an earlier draft did.
		if len(walker.thisIsValid) > 0 {
			walker.thisIsValid = walker.thisIsValid[:len(walker.thisIsValid)-1]
			popped = true
		}

	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		walker.thisIsValid = append(
			walker.thisIsValid,
			!thisIsDefaultBoundIn(walker.ruleContext, node, walker.settings),
		)
		pushed = true

	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindConstructor, ast.KindClassStaticBlockDeclaration:
		// A method's `this` is the receiver and a static block's is the class. Core reaches these
		// through the same default-binding test, which answers false for all of them.
		walker.thisIsValid = append(walker.thisIsValid, true)
		pushed = true

	case ast.KindPropertyDeclaration:
		// A field initializer is an implicit function whose `this` is the instance. `accessor` is a
		// modifier here rather than a separate node, so upstream's PropertyDefinition and
		// AccessorProperty both arrive as this kind.
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
	if popped {
		walker.thisIsValid = append(walker.thisIsValid, true)
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
