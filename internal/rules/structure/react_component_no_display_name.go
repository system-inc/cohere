package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/utilities/react"
)

var messageNoDisplayNameAssignment = rule.Message{
	Id: "noDisplayNameAssignment",
	Description: "This assigns displayName by hand. A function component already carries its own " +
		"name and DevTools reads it, so the assignment is a second copy of the name that a rename " +
		"does not update: rename the component and the tree still shows the old label, with " +
		"nothing failing. The one case that needs it is a wrapper returning an anonymous " +
		"component, and that is exempt.",
}

// anonymousComponentWrapperNames are the wrappers whose result carries no name of its own.
//
// A component built by one of these genuinely has nothing for DevTools to read, so the assignment
// is the only way to label it and the rule has to allow it. Anything else assigning displayName is
// copying a name that already exists.
var anonymousComponentWrapperNames = map[string]bool{
	"memo":       true,
	"forwardRef": true,
}

// ReactComponentNoDisplayName flags a hand-written displayName assignment.
//
//	valid:   function Field() { ... }                      // the name is already there
//	valid:   const Field = memo(function () { ... }); Field.displayName = 'Field'
//	invalid: function Field() { ... } Field.displayName = 'Field'
//
// The exemption is why this rule needs state rather than a single listener. Whether an assignment
// is allowed depends on how the target was declared, which is a different node seen earlier, so the
// rule collects wrapper-initialized bindings during the walk and consults that set at the
// assignment.
//
// Declaration order is therefore load-bearing, and it matches the original: a binding declared
// after the assignment does not exempt it. That is a real limit rather than an oversight, and it
// is the right one, because `Field.displayName = 'x'` above `const Field = memo(...)` is a
// temporal dead zone error at runtime anyway. A rule that exempted it would be excusing code that
// cannot run.
var ReactComponentNoDisplayName = rule.Rule{
	Name: "structure/react-component-no-display-name",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		// Scoped to this file's walk, which is the whole lifetime that matters: the original keys
		// the same set on one `create` call per file.
		wrappedAsAnonymous := map[string]bool{}

		return rule.Listeners{
			ast.KindVariableDeclaration: func(node *ast.Node) {
				declaration := node.AsVariableDeclaration()
				name := declaration.Name()
				if name == nil || name.Kind != ast.KindIdentifier {
					return
				}
				if isAnonymousWrapperCall(declaration.Initializer) {
					wrappedAsAnonymous[name.Text()] = true
				}
			},

			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()

				// Only a plain assignment. A compound one (`+=`) cannot be establishing a display
				// name, and reporting it would be a claim about arithmetic on a string.
				if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindEqualsToken {
					return
				}

				left := ast.SkipParentheses(binary.Left)
				if left == nil || left.Kind != ast.KindPropertyAccessExpression {
					// An element access (`Field['displayName']`) is deliberately not matched,
					// following the original. It is not how anyone writes this, and matching it
					// would mean deciding what a computed key resolves to.
					return
				}

				access := left.AsPropertyAccessExpression()
				property := access.Name()
				if property == nil || property.Kind != ast.KindIdentifier || property.Text() != "displayName" {
					return
				}

				target := ast.SkipParentheses(access.Expression)
				if target != nil && target.Kind == ast.KindIdentifier && wrappedAsAnonymous[target.Text()] {
					return
				}

				ctx.ReportNode(node, messageNoDisplayNameAssignment)
			},
		}
	},
}

// isAnonymousWrapperCall reports `memo(...)`, `forwardRef(...)`, or their React.-qualified forms.
//
// The nil check comes before SkipParentheses rather than after it. SkipParentheses dereferences its
// argument, so passing it a nil initializer panics, and a declaration without one is ordinary:
// `declare const memo` and `let Field` both reach here. Found by a fixture crashing the walk, which
// is the failure mode worth keeping in mind for the rest of this family, since almost every rule
// here reaches for an initializer that does not have to exist.
func isAnonymousWrapperCall(initializer *ast.Node) bool {
	if initializer == nil {
		return false
	}
	initializer = ast.SkipParentheses(initializer)
	if initializer == nil || initializer.Kind != ast.KindCallExpression {
		return false
	}

	callee := ast.SkipParentheses(initializer.AsCallExpression().Expression)
	if callee == nil {
		return false
	}

	switch callee.Kind {
	case ast.KindIdentifier:
		return anonymousComponentWrapperNames[callee.Text()]
	case ast.KindPropertyAccessExpression:
		return react.IsNamespacedMember(callee, func(name string) bool {
			return anonymousComponentWrapperNames[name]
		})
	}
	return false
}
