package core

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/class_members"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var NoDupeClassMembers = rule.Rule[rule.NoOptions]{
	Name: "no-dupe-class-members",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		// The collision judgment lives in `class_members` rather than here, because
		// `@typescript-eslint/no-dupe-class-members` needs the same answer and a rule package may
		// not import another rule package. This rule owns the wording and the message id; the
		// utility owns which members collide.
		check := func(members *ast.NodeList) {
			class_members.ForEachDuplicate(members, func(name *ast.Node, key class_members.Key) {
				ruleContext.ReportNode(name, rule.Message{
					Id: "noDupeClassMembers",
					Description: fmt.Sprintf(
						"This class already declares a member named %s. The later declaration wins "+
							"silently, so the earlier one is dead code that reads as live, and "+
							"nothing in the language or at runtime tells the two apart.", key.Name),
				})
			})
		}

		return rule.Listeners{
			ast.KindClassDeclaration: func(node *ast.Node) {
				check(node.AsClassDeclaration().Members)
			},
			ast.KindClassExpression: func(node *ast.Node) {
				check(node.AsClassExpression().Members)
			},
		}
	},
}
