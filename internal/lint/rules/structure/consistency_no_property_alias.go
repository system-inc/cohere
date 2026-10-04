package structure

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
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
// Six exemptions, each of which is a real judgment rather than a narrowing for safety.
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
//
// **A binding written again after its declaration is not an alias.** `let label = options.label`
// followed by `if (compact) label = short` is a default a branch overrides, and the reach could
// not stand in for it. Nine sites in api had the shape (#nd52037, PostResolver.ts:135 among them).
//
// **A snapshot taken before its source is written is not an alias either.** api's
// BaseWorkerNodeRunner.ts:109 takes `const httpServer = this.httpServer`, sets
// `this.httpServer = undefined`, then reads the local. A reach there would read undefined, so the
// local holds a value the property no longer does. Exempt when a link of the source chain (the
// property, an object above it, or the root binding) is assigned, updated or deleted after the
// declaration and before the local's last read (#nd52037).
//
// Both read names, not symbols, within the enclosing function, as the hook exemption does, so a
// nested binding of the same name counts too. Structure's ESLint twin decides the same two.
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

				if isWrittenAfterItsDeclaration(enclosing, localName, name) {
					return
				}

				if isSnapshotTakenBeforeItsSourceIsWritten(enclosing, node, localName, name, initializer) {
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

// isWrittenAfterItsDeclaration reports whether anything in the enclosing function writes the local's
// name again: an assignment, a compound assignment, an update, or a destructuring target.
func isWrittenAfterItsDeclaration(enclosing *ast.Node, localName string, declarationName *ast.Node) bool {
	written := false
	var walk func(node *ast.Node) bool
	walk = func(node *ast.Node) bool {
		if node == nil || written {
			return false
		}
		if node.Kind == ast.KindIdentifier && node != declarationName && node.Text() == localName &&
			reference.WritesToBinding(node) {
			written = true
			return true
		}
		node.ForEachChild(walk)
		return written
	}
	walk(enclosing)
	return written
}

// isSnapshotTakenBeforeItsSourceIsWritten reports whether a link of the reach the local was taken
// from is written after the declaration and before the local's last read.
//
// The links are the reach itself and every object above it down to the root: for
// `const value = state.inner.value`, a write to `state.inner.value`, to `state.inner` or to `state`
// each changes what a reach at the read would see. A write is an assignment of any operator, an
// update, or a `delete`.
func isSnapshotTakenBeforeItsSourceIsWritten(enclosing *ast.Node, declaration *ast.Node, localName string, declarationName *ast.Node, initializer *ast.Node) bool {
	lastRead := -1
	var findLastRead func(node *ast.Node) bool
	findLastRead = func(node *ast.Node) bool {
		if node == nil {
			return false
		}
		if node.Kind == ast.KindIdentifier && node != declarationName && node.Text() == localName &&
			!reference.WritesToBinding(node) && node.End() > lastRead {
			lastRead = node.End()
		}
		node.ForEachChild(findLastRead)
		return false
	}
	findLastRead(enclosing)
	if lastRead < 0 {
		return false
	}

	var links []*ast.Node
	for current := unwrapReach(initializer); current != nil; {
		links = append(links, current)
		if current.Kind != ast.KindPropertyAccessExpression {
			break
		}
		current = unwrapReach(current.AsPropertyAccessExpression().Expression)
	}

	found := false
	var walk func(node *ast.Node) bool
	walk = func(node *ast.Node) bool {
		if node == nil || found {
			return false
		}
		if target := writeTarget(node); target != nil && node.Pos() >= declaration.End() && node.End() <= lastRead {
			for _, link := range links {
				if sameReach(target, link) {
					found = true
					return true
				}
			}
		}
		node.ForEachChild(walk)
		return found
	}
	walk(enclosing)
	return found
}

// writeTarget is what a node writes, for an assignment of any operator, an update, or a `delete`,
// and nil for anything else.
func writeTarget(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		if binary.OperatorToken != nil && ast.IsAssignmentOperator(binary.OperatorToken.Kind) {
			return binary.Left
		}
	case ast.KindPrefixUnaryExpression:
		unary := node.AsPrefixUnaryExpression()
		if reference.IsUpdateOperator(unary.Operator) {
			return unary.Operand
		}
	case ast.KindPostfixUnaryExpression:
		unary := node.AsPostfixUnaryExpression()
		if reference.IsUpdateOperator(unary.Operator) {
			return unary.Operand
		}
	case ast.KindDeleteExpression:
		return node.AsDeleteExpression().Expression
	}
	return nil
}

// unwrapReach sees through what does not change which value an expression reaches: parentheses, a
// non-null assertion, and a type assertion.
func unwrapReach(node *ast.Node) *ast.Node {
	for node != nil {
		switch node.Kind {
		case ast.KindParenthesizedExpression, ast.KindNonNullExpression, ast.KindAsExpression,
			ast.KindSatisfiesExpression, ast.KindTypeAssertionExpression:
			node = node.Expression()
		default:
			return node
		}
	}
	return nil
}

// sameReach reports whether two expressions name the same place by spelling: the same identifier,
// `this`, or the same property of the same reach.
func sameReach(first *ast.Node, second *ast.Node) bool {
	first, second = unwrapReach(first), unwrapReach(second)
	if first == nil || second == nil || first.Kind != second.Kind {
		return false
	}
	switch first.Kind {
	case ast.KindIdentifier:
		return first.Text() == second.Text()
	case ast.KindThisKeyword:
		return true
	case ast.KindPropertyAccessExpression:
		firstName, secondName := first.AsPropertyAccessExpression().Name(), second.AsPropertyAccessExpression().Name()
		return firstName != nil && secondName != nil && firstName.Text() == secondName.Text() &&
			sameReach(first.AsPropertyAccessExpression().Expression, second.AsPropertyAccessExpression().Expression)
	}
	return false
}
