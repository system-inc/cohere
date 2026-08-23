package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageUsePropertiesNotProps = rule.Message{
	Id: "usePropertiesNotProps",
	Description: "Name a component's parameter `properties` rather than `props`. The abbreviation " +
		"is the odd one out in a codebase that spells everything else in full, and the cost is not " +
		"aesthetic: half the components reading `props.label` and half reading `properties.label` " +
		"means every search for one spelling misses the other, which is the same drift the naming " +
		"rules exist to prevent everywhere else.",
}

// ReactComponentRequirePropertiesParameter flags a component whose first parameter is named `props`.
//
//	valid:   function Button(properties: ButtonProperties) { ... }
//	valid:   function Button({ label, ...buttonProperties }) { ... }
//	invalid: function Button(props: ButtonProperties) { ... }
//
// Only the name, and only when it is exactly `props`. A destructured parameter has no single name
// to judge and is another rule's business; a parameter named anything else is the author's choice.
//
// Fixable, since renaming a parameter is one identifier with one right answer. The fix rewrites
// only the declaration, not the uses, which the fixer's convergence check turns into a loud build
// error rather than a silent one: `props.label` inside a function whose parameter is now
// `properties` does not compile.
//
// That is worth stating plainly because it looks like a defect. The original does the same, and the
// alternative is a fix that renames every reference and has to decide which `props` in the body are
// the parameter and which are somebody else's. A rename that stops at the declaration fails
// immediately and visibly; one that guesses at references fails quietly and in the wrong place.
var ReactComponentRequirePropertiesParameter = rule.Rule{
	Name: "react-component-require-properties-parameter",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if !FileContextFor(ctx.SourceFile.FileName()).IsReactFile {
			return nil
		}

		check := func(functionNode *ast.Node, parameters []*ast.Node) {
			if len(parameters) == 0 {
				return
			}
			if !HasJsxOrReactHookCalls(functionNode) {
				// A function that renders nothing is not a component, and its parameter names are
				// nobody's business but its author's.
				return
			}

			name := parameters[0].AsParameterDeclaration().Name()
			if name == nil || name.Kind != ast.KindIdentifier || name.Text() != "props" {
				return
			}
			ctx.ReportNodeWithFixes(name, messageUsePropertiesNotProps, ctx.ReplaceNode(name, "properties"))
		}

		return rule.Listeners{
			ast.KindFunctionDeclaration: func(node *ast.Node) {
				check(node, parameterNodes(node.AsFunctionDeclaration().Parameters))
			},
			ast.KindFunctionExpression: func(node *ast.Node) {
				check(node, parameterNodes(node.AsFunctionExpression().Parameters))
			},
			ast.KindArrowFunction: func(node *ast.Node) {
				check(node, parameterNodes(node.AsArrowFunction().Parameters))
			},
		}
	},
}
