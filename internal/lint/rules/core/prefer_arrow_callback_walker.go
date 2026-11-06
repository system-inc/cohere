package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// preferArrowCallbackWalker carries the frame stack across the recursive walk.
type preferArrowCallbackWalker struct {
	ruleContext rule.Context
	settings    PreferArrowCallbackSettings
	frames      []*preferArrowCallbackFrame
}

// walk visits a node and records on owner, the nearest enclosing function's frame, what the node uses.
//
// Mutates: owner the frame is the walk's record of its function, which judge reads once the body is walked
func (walker *preferArrowCallbackWalker) walk(node *ast.Node, owner *preferArrowCallbackFrame) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindThisKeyword:
		if owner != nil {
			owner.usesThis = true
		}
	case ast.KindSuperKeyword:
		if owner != nil {
			owner.usesSuper = true
		}
	case ast.KindMetaProperty:
		// Upstream's `checkMetaProperty(node, "new", "target")`. `import.meta` is the other meta
		// property and it does not count.
		if owner != nil && node.AsMetaProperty() != nil &&
			node.AsMetaProperty().KeywordToken == ast.KindNewKeyword {
			owner.usesNewTarget = true
		}
	case ast.KindIdentifier:
		// `arguments` belongs to the nearest enclosing function, which is exactly what `owner` is.
		if owner != nil && node.Text() == "arguments" && !preferArrowCallbackIsDeclarationName(node) &&
			!preferArrowCallbackResolvesToADeclaration(walker.ruleContext, node) {
			owner.usesArguments = true
		}
	}

	// A function declaration or expression opens a frame; an arrow deliberately does not, because
	// it inherits `this`, `super`, `new.target` and `arguments` from the function around it.
	nextOwner := owner
	var opened *preferArrowCallbackFrame
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor,
		ast.KindConstructor, ast.KindClassStaticBlockDeclaration:
		opened = &preferArrowCallbackFrame{node: node}
		walker.frames = append(walker.frames, opened)
		nextOwner = opened
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walk(child, nextOwner)
		return false
	})

	if opened != nil {
		walker.frames = walker.frames[:len(walker.frames)-1]
		if node.Kind == ast.KindFunctionExpression {
			walker.judge(node, opened)
		}
	}
}

// judge is upstream's `FunctionExpression:exit`.
func (walker *preferArrowCallbackWalker) judge(node *ast.Node, frame *preferArrowCallbackFrame) {
	expression := node.AsFunctionExpression()
	if expression == nil {
		return
	}

	if walker.settings.AllowNamedFunctions && expression.Name() != nil {
		return
	}
	// An arrow cannot be a generator, so there is nothing to convert to.
	if expression.AsteriskToken != nil {
		return
	}
	// A function that calls itself by name needs that binding, which an arrow does not provide.
	if preferArrowCallbackReferencesOwnName(walker.ruleContext, node, expression) {
		return
	}
	// An arrow has no `arguments` binding either.
	if frame.usesArguments {
		return
	}

	callback := preferArrowCallbackInfoFor(node)
	if !callback.isCallback {
		return
	}
	if walker.settings.AllowUnboundThis && frame.usesThis && !callback.isLexicalThis {
		return
	}
	if frame.usesSuper || frame.usesNewTarget {
		return
	}

	// The repair is withheld in five cases; see the rule's doc comment. Reporting without a fix is
	// the subset that can be shown correct, which is what upstream does through a `fix` returning
	// null.
	if fix, ok := preferArrowCallbackFix(walker.ruleContext, node, expression, frame, callback); ok {
		walker.ruleContext.ReportNodeWithFixes(node, messagePreferArrowCallbackPreferArrowCallback, fix...)
		return
	}
	walker.ruleContext.ReportNode(node, messagePreferArrowCallbackPreferArrowCallback)
}
