package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/scope"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoPropertyAliasText is the rule's message, whose wording lives in
// `policy/messages/consistency-no-property-alias.json`.
var consistencyNoPropertyAliasText = policy.MessageOf("structure/consistency-no-property-alias", "noPropertyAlias")

// ConsistencyNoPropertyAlias flags `const x = object.x`, a local that exists only to rename a
// property onto a shorter, less-anchored name.
//
//	valid:   const value = options.other      // the names differ, so it is not a pure alias
//	valid:   const timeZone = format().timeZone // the chain computes, so this caches rather than aliases
//	valid:   const key = record[name]         // computed access carries intent we do not second-guess
//	valid:   const Foo = Bar.Foo              // module scope, a re-export shape rather than an alias
//	invalid: function run() { const promise = tracked.promise; return promise; }
//
// Four exemptions, each of which is a real judgment rather than a narrowing for safety.
//
// **A chain containing a call is caching, not aliasing.** `Intl.DateTimeFormat().resolvedOptions()`
// costs something to evaluate, so a local holding its result is doing work a reach would repeat.
// The test walks the object chain rather than checking the immediate object, because the call can
// sit at any depth.
//
// **Computed access is left alone.** `record[name]` says something a reader cannot get from
// `record.name`, and the rule has no way to tell an index from a property spelled dynamically.
//
// **Module scope is out of scope.** `const Foo = Bar.Foo` at the top of a file is a re-export
// shape, and a rule about local readability has nothing to say about it.
//
// **A local read inside a hook dependency array is allowed**, and this is the exemption that would
// look like a bug without its reason. Member expressions in a dependency array interact badly with
// exhaustive-deps and can cause referential thrash, so capturing the value into a stable local is
// the canonical workaround rather than a lapse. The check looks for the local anywhere in the
// enclosing function, not only beside the declaration, because the array is usually several lines
// below.
var ConsistencyNoPropertyAlias = rule.Rule{
	Name: "structure/consistency-no-property-alias",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindVariableDeclaration: func(node *ast.Node) {
				declaration := node.AsVariableDeclaration()

				name := declaration.Name()
				if name == nil || name.Kind != ast.KindIdentifier {
					return
				}
				localName := name.Text()

				initializer := declaration.Initializer
				if initializer == nil || initializer.Kind != ast.KindPropertyAccessExpression {
					return
				}
				access := initializer.AsPropertyAccessExpression()

				// An optional chain is exempt, and this is the exemption the original gets free
				// from its AST rather than states.
				//
				// ESTree wraps `options?.entityKey` in a ChainExpression, so the original's
				// `init.type !== 'MemberExpression'` test returns early and never sees it.
				// typescript-go has no such wrapper: an optional access is a
				// PropertyAccessExpression carrying a question-dot token, so without this check the
				// rule reads every `const x = o?.x` as a plain alias.
				//
				// Measured rather than reasoned: without it, cohere reported 71 findings on the
				// ahra tree where the gate reports zero, and the first ones inspected were all
				// `node.id?.name` and `options?.entityKey`. Matching the gate is the whole
				// acceptance criterion, so the exemption ports even though a case could be made
				// that an optional alias is just as much an alias.
				if chainContainsOptional(initializer) {
					return
				}

				// An element access (`record[name]`) is a different node kind entirely, so the kind
				// check above is what implements the computed-access exemption. Stated here because
				// the original spells it as an explicit `computed` test and a reader comparing the
				// two would otherwise look for the missing branch.
				property := access.Name()
				if property == nil || property.Kind != ast.KindIdentifier {
					return
				}
				if property.Text() != localName {
					return
				}

				if chainContainsCall(access.Expression) {
					return
				}

				enclosing := scope.EnclosingFunctionLike(node)
				if enclosing == nil {
					return
				}

				if isReadInsideHookDependencyArray(enclosing, localName, name) {
					return
				}

				sourceText := rule.TokenRange(ctx.SourceFile, access.Expression)
				objectText := ctx.SourceFile.Text()[sourceText.Pos():sourceText.End()]

				ctx.ReportNode(node, rule.Message{
					Id:          consistencyNoPropertyAliasText.Id,
					Description: consistencyNoPropertyAliasText.Render(map[string]string{"name": localName, "object": objectText}),
				})
			},
		}
	},
}

// chainContainsOptional reports whether any link in a property-access chain is optional.
//
// The whole chain is tested rather than the outermost link, because the token sits wherever the
// author wrote the question mark. `account.data?.profile.displayName` carries it two links in, so a
// check on the outer access alone finds nothing and reports the alias. Measured: the outer-only
// version left exactly one false finding on the ahra tree, and it was that shape.
func chainContainsOptional(expression *ast.Node) bool {
	for current := expression; current != nil; {
		switch current.Kind {
		case ast.KindPropertyAccessExpression:
			access := current.AsPropertyAccessExpression()
			if access.QuestionDotToken != nil {
				return true
			}
			current = access.Expression
		case ast.KindElementAccessExpression:
			elementAccess := current.AsElementAccessExpression()
			if elementAccess.QuestionDotToken != nil {
				return true
			}
			current = elementAccess.Expression
		case ast.KindCallExpression:
			call := current.AsCallExpression()
			if call.QuestionDotToken != nil {
				return true
			}
			current = call.Expression
		case ast.KindParenthesizedExpression:
			current = current.AsParenthesizedExpression().Expression
		default:
			return false
		}
	}
	return false
}

// chainContainsCall reports whether any link in a property-access chain is a call.
//
// `format().timeZone` and `a.b().c.d` both compute, so a local holding the result is caching work
// rather than renaming a reach. The walk descends the object side because the call can sit at any
// depth, and stops at the first node that is neither an access nor a call.
func chainContainsCall(expression *ast.Node) bool {
	for current := expression; current != nil; {
		switch current.Kind {
		case ast.KindCallExpression:
			return true
		case ast.KindPropertyAccessExpression:
			current = current.AsPropertyAccessExpression().Expression
		case ast.KindElementAccessExpression:
			current = current.AsElementAccessExpression().Expression
		case ast.KindParenthesizedExpression:
			current = current.AsParenthesizedExpression().Expression
		default:
			return false
		}
	}
	return false
}

// isReadInsideHookDependencyArray reports whether the local is mentioned inside a hook call's
// dependency array anywhere within the enclosing function.
//
// The whole function is searched rather than the statements near the declaration, because the array
// that justifies the alias is usually several lines below it. The declaration's own name node is
// excluded, since the binding is not a read of itself.
func isReadInsideHookDependencyArray(enclosing *ast.Node, localName string, declarationName *ast.Node) bool {
	found := false

	var walk func(node *ast.Node) bool
	walk = func(node *ast.Node) bool {
		if node == nil || found {
			return false
		}
		if node.Kind == ast.KindCallExpression {
			call := node.AsCallExpression()
			// The package's own isHookCall is used rather than a local one, and it is stricter
			// than the obvious version: it requires the namespaced form to be React specifically,
			// so `somethingElse.useThing(...)` is not a hook. A second copy written here accepted
			// any namespace and would have exempted aliases the gate still reports.
			if isHookCall(call) && call.Arguments != nil {
				// The dependency array is the second argument onward, matching the original. A
				// first-argument array is the callback position and means something else.
				for index, argument := range call.Arguments.Nodes {
					if index == 0 || argument.Kind != ast.KindArrayLiteralExpression {
						continue
					}
					if arrayMentions(argument, localName, declarationName) {
						found = true
						return true
					}
				}
			}
		}
		node.ForEachChild(walk)
		return found
	}
	walk(enclosing)

	return found
}

// arrayMentions reports whether a dependency array names the local.
//
// An entry may be the bare identifier or the head of a longer reach (`value.property`), so the walk
// descends rather than comparing top-level elements. The declaration's own name node is skipped
// because a binding is not a read.
func arrayMentions(array *ast.Node, localName string, declarationName *ast.Node) bool {
	mentioned := false

	var walk func(node *ast.Node) bool
	walk = func(node *ast.Node) bool {
		if node == nil || mentioned {
			return false
		}
		if node.Kind == ast.KindIdentifier && node != declarationName && node.Text() == localName {
			mentioned = true
			return true
		}
		node.ForEachChild(walk)
		return mentioned
	}
	walk(array)

	return mentioned
}
