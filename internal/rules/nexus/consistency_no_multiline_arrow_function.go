package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/rule"
)

func messageReactHookArrow(hookName string) rule.Message {
	return rule.Message{
		Id: "reactHookArrow",
		Description: "Use a regular function instead of an arrow function with React." + hookName +
			". A named function shows up in a stack trace and in the React devtools as itself.",
	}
}

var messageAddEventListenerArrow = rule.Message{
	Id: "addEventListenerArrow",
	Description: "Use a regular function instead of an arrow function with addEventListener. A listener " +
		"you cannot name is a listener you cannot remove, since removeEventListener needs the same " +
		"reference back.",
}

var messageMultilineArrow = rule.Message{
	Id: "multilineArrow",
	Description: "Use a regular function instead of a multi-line arrow function. An arrow earns its " +
		"terseness on a single-line implicit return, and past that a named function declaration reads " +
		"better and hoists. Arrows that use `this` are exempt, since a function would rebind it.",
}

// ConsistencyNoMultilineArrowFunction steers multi-line arrows to function declarations.
//
//	valid:   const add = (a, b) => a + b
//	valid:   const f = () => {}
//	valid:   element.addEventListener('click', function handleClick() { ... })
//	invalid: const compute = (input) => {
//	             return input * 2;
//	         }
//
// Three exemptions, each of which cost something to learn.
//
// An arrow using `this` is left alone, because a function declaration would rebind it. Finding that
// out requires walking into nested arrows but not into nested functions: a nested function binds its
// own `this` and says nothing about this arrow's, while a nested arrow inherits this one's, so a
// `this` inside it is this arrow's. Treating a nested arrow as its own scope let an outer arrow
// convert and rebound `this` for every nested arrow at once.
//
// A single-line arrow is not a multi-line arrow whatever shape its body has. A block body stood in
// for "spans lines" once, and the two part company at `() => {}`: a no-op callback rewritten to
// `function() {}` for no reading gain. That one was not merely noise, because an arrow has no
// prototype and a function expression does, so a fixture that existed to be non-constructible became
// constructible and its assertion flipped. The rule's name is the honest test, so this measures the
// span.
//
// A React hook or addEventListener argument reports through its own message and must not be reported
// twice, so the general case skips what the call-expression case already claimed.
var ConsistencyNoMultilineArrowFunction = rule.Rule{
	Name: "nexus/consistency-no-multiline-arrow-function",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}
		sourceFile := ctx.SourceFile

		return rule.Listeners{
			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				if call == nil || call.Expression == nil || call.Arguments == nil {
					return
				}
				if call.Expression.Kind != ast.KindPropertyAccessExpression {
					return
				}
				access := call.Expression.AsPropertyAccessExpression()
				if access == nil || access.Expression == nil || access.Name() == nil {
					return
				}
				objectName := ""
				if access.Expression.Kind == ast.KindIdentifier {
					objectName = access.Expression.Text()
				}
				propertyName := access.Name().Text()

				arguments := call.Arguments.Nodes

				if isReactHookCallee(objectName, propertyName) && len(arguments) > 0 &&
					arguments[0].Kind == ast.KindArrowFunction {
					ctx.ReportNode(arguments[0], messageReactHookArrow(propertyName))
					return
				}
				if propertyName == "addEventListener" && len(arguments) > 1 &&
					arguments[1].Kind == ast.KindArrowFunction {
					ctx.ReportNode(arguments[1], messageAddEventListenerArrow)
				}
			},

			ast.KindArrowFunction: func(node *ast.Node) {
				arrow := node.AsArrowFunction()
				if arrow == nil || arrow.Body == nil {
					return
				}

				// An arrow that uses `this` would change meaning as a function declaration.
				if usesThis(arrow.Body) {
					return
				}
				// An implicit return is the shape an arrow is for.
				if arrow.Body.Kind != ast.KindBlock {
					return
				}
				// One line is not multi-line, whatever the body's shape.
				if spansOneLine(sourceFile, node) {
					return
				}
				// Already claimed by the call-expression case above.
				if isClaimedByCallExpressionCase(node) {
					return
				}

				ctx.ReportNode(node, messageMultilineArrow)
			},
		}
	},
}

// isReactHookCallee reports whether a callee is React.forwardRef or a React hook.
func isReactHookCallee(objectName string, propertyName string) bool {
	if objectName != "React" {
		return false
	}
	return propertyName == "forwardRef" || strings.HasPrefix(propertyName, "use")
}

// usesThis reports whether a `this` appears in a body, counting only the ones that would rebind.
//
// A nested function declaration or expression binds its own `this`, so its contents say nothing
// about the enclosing arrow and are not searched. A nested arrow has no `this` of its own and
// inherits the enclosing one, so it is searched.
func usesThis(node *ast.Node) bool {
	found := false
	var walk func(current *ast.Node)
	walk = func(current *ast.Node) {
		if current == nil || found {
			return
		}
		if current.Kind == ast.KindThisKeyword {
			found = true
			return
		}
		if current.Kind == ast.KindFunctionDeclaration || current.Kind == ast.KindFunctionExpression ||
			current.Kind == ast.KindMethodDeclaration || current.Kind == ast.KindClassDeclaration ||
			current.Kind == ast.KindClassExpression {
			return
		}
		current.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return found
		})
	}
	walk(node)
	return found
}

// spansOneLine reports whether a node's own text begins and ends on the same source line.
//
// The span is measured from the token range rather than from Pos(), because Pos() sits before the
// node's leading trivia. An arrow written inside a multi-line array literal is preceded by a newline
// and indentation, so measuring from Pos() puts its start on the previous line and a genuinely
// single-line `() => {}` reads as spanning two. That is the exact case this exemption exists for,
// and it produced the one false finding this rule had on the ahra tree.
func spansOneLine(sourceFile *ast.SourceFile, node *ast.Node) bool {
	tokenRange := rule.TokenRange(sourceFile, node)
	startLine, _ := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, tokenRange.Pos())
	endLine, _ := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, tokenRange.End())
	return startLine == endLine
}

// isClaimedByCallExpressionCase reports whether an arrow is the argument the call-expression
// listener already reported, so one arrow never produces two findings.
func isClaimedByCallExpressionCase(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil || parent.Kind != ast.KindCallExpression {
		return false
	}
	call := parent.AsCallExpression()
	if call == nil || call.Expression == nil || call.Expression.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := call.Expression.AsPropertyAccessExpression()
	if access == nil || access.Name() == nil {
		return false
	}
	objectName := ""
	if access.Expression != nil && access.Expression.Kind == ast.KindIdentifier {
		objectName = access.Expression.Text()
	}
	propertyName := access.Name().Text()
	return isReactHookCallee(objectName, propertyName) || propertyName == "addEventListener"
}
