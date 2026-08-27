package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageEmptyInterface = rule.Message{
	Id: "noEmptyInterface",
	Description: "An interface with no members allows any non-nullish value, so it constrains " +
		"nothing: every object, every string, every number satisfies it. It reads like a type and " +
		"behaves like `unknown` with better manners. Give it members, or use the type it actually " +
		"means.",
}

var messageEmptyObjectType = rule.Message{
	Id: "noEmptyObjectType",
	Description: "`{}` does not mean an empty object. It means any value that is not null or " +
		"undefined, so a string and a number both satisfy it. That is almost never what the author " +
		"intended, and the two readings are impossible to tell apart at a glance. Use `object` for " +
		"any non-primitive, or `unknown` for genuinely any value.",
}

// NoEmptyObjectType flags an interface with no members and the `{}` type literal.
//
//	valid:   interface Thing { name: string }
//	valid:   interface Both extends Base, Derived {}
//	valid:   type Thing = Base & {}
//	invalid: interface Thing {}
//	invalid: interface Thing extends Base {}
//	invalid: type Thing = {}
//	invalid: let value: {}
//
// Ported from oxc's `no_empty_object_type`, which is what the gate runs.
//
// Two boundaries came from oxc's corpus rather than from reading the rule, and neither is obvious.
//
// An interface extending *two* or more interfaces is allowed even when empty, because that is how a
// reader expresses an intersection of named types where a union would be wrong. Extending exactly
// one is flagged: it is an alias with extra steps. The corpus states this as a comment on the
// passing case, which is the only place the reasoning is written down.
//
// `{}` inside an intersection is allowed. `Base & {}` is a known idiom for forcing a type to display
// expanded rather than by name, and the empty half is doing real work there.
//
// The repair is a *suggestion* rather than a fix, and there are two of them: `object` and `unknown`
// mean different things and only the author knows which was meant. A fix that picked one would be
// changing the type rather than repairing a spelling.
//
// The upstream rule takes three options. None is set in this tree's config, so the defaults are what
// matters here and they are the strictest reading: interfaces never allowed empty, object types never
// allowed empty, no name exemption. The options are deliberately not implemented rather than
// implemented and unused, because an option nobody configures is a surface that cannot be tested
// against real settings.
//
// One upstream behavior is not reproduced. oxc suppresses the report when an empty interface
// declaration-merges with a class or another interface of the same name, which needs the symbol
// table our checker shim does not expose. The effect is a false positive on a merged declaration.
// That is named here rather than hidden, and the tree has none: verified by planting rather than by
// reading, since a rule reporting zero and a rule that cannot report look identical.
var NoEmptyObjectType = rule.Rule{
	Name: "@typescript-eslint/no-empty-object-type",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindInterfaceDeclaration: func(node *ast.Node) {
				declaration := node.AsInterfaceDeclaration()
				if declaration == nil || declaration.Members == nil {
					return
				}
				if len(declaration.Members.Nodes) != 0 {
					return
				}

				// Two or more heritage names is the intersection idiom and is allowed. Zero or one is
				// not: an empty interface extending nothing constrains nothing, and one extending a
				// single name is an alias written the long way.
				if countHeritageNames(declaration.HeritageClauses) >= 2 {
					return
				}

				ctx.ReportNode(node, messageEmptyInterface)
			},

			ast.KindTypeLiteral: func(node *ast.Node) {
				typeLiteral := node.AsTypeLiteralNode()
				if typeLiteral == nil || typeLiteral.Members == nil {
					return
				}
				if len(typeLiteral.Members.Nodes) != 0 {
					return
				}

				// `Base & {}` is the display-expansion idiom, where the empty half is load-bearing.
				if node.Parent != nil && node.Parent.Kind == ast.KindIntersectionType {
					return
				}

				ctx.ReportNodeWithSuggestions(node, messageEmptyObjectType,
					rule.Suggestion{
						Message: rule.Message{
							Id:          "replaceWithObject",
							Description: "Replace `{}` with `object`, which means any non-primitive.",
						},
						Fixes: []rule.Fix{ctx.ReplaceNode(node, "object")},
					},
					rule.Suggestion{
						Message: rule.Message{
							Id:          "replaceWithUnknown",
							Description: "Replace `{}` with `unknown`, which means any value at all.",
						},
						Fixes: []rule.Fix{ctx.ReplaceNode(node, "unknown")},
					},
				)
			},
		}
	},
}

// countHeritageNames counts the names an interface extends, across every heritage clause.
//
// The count is of names rather than clauses: an interface has one `extends` clause holding a list,
// so counting clauses would answer one for `extends Base, Derived` and miss the distinction the rule
// turns on.
func countHeritageNames(clauses *ast.NodeList) int {
	if clauses == nil {
		return 0
	}

	count := 0
	for _, clause := range clauses.Nodes {
		heritage := clause.AsHeritageClause()
		if heritage == nil || heritage.Types == nil {
			continue
		}
		count += len(heritage.Types.Nodes)
	}
	return count
}
