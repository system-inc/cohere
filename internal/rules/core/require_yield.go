package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageMissingYield = rule.Message{
	Id: "missingYield",
	Description: "This generator function contains no `yield`, so calling it returns an iterator " +
		"that finishes immediately with the function's return value and never produces anything. " +
		"A caller writing `for(const item of generate())` gets zero iterations. Either the " +
		"asterisk is left over from a refactor and the function should be a plain one, or a " +
		"`yield` is missing.",
}

// RequireYield flags a generator function whose body contains no yield.
//
//	valid:   function* generate() { yield 1; }
//	valid:   function generate() { return 1; }
//	valid:   function* generate() { yield* other(); }
//	invalid: function* generate() { return 1; }
//	invalid: const generate = function* () { return 1; };
//	invalid: class C { *generate() { return 1; } }
//
// A generator with no yield is not a syntax error and not a runtime error. It is a function whose
// callers silently get nothing, which is why this is worth a rule: the failure appears at every call
// site as an empty loop rather than at the definition as a mistake.
//
// Four shapes, enumerated before the listener rather than after: a function declaration, a function
// expression, a method, and an object literal's method shorthand. An arrow function cannot be a
// generator, so there is no fifth.
//
// A nested function does not count. A `yield` inside an inner function belongs to that function, and
// if the inner one is not itself a generator the yield is a syntax error anyway, so the walk stops
// at every function boundary. That is the whole subtlety here, and a walk that descended would call
// an empty generator satisfied by a yield it does not own.
//
// An empty body is reported. `function* generate() {}` yields nothing and is the clearest case.
//
// No fix. The two repairs are removing the asterisk and adding a yield, and those produce different
// functions.
var RequireYield = rule.Rule{
	Name: "require-yield",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		check := func(node *ast.Node, asteriskToken *ast.Node, body *ast.Node) {
			if asteriskToken == nil || body == nil {
				return
			}
			if bodyContainsYield(body) {
				return
			}
			ctx.ReportNode(node, messageMissingYield)
		}

		return rule.Listeners{
			ast.KindFunctionDeclaration: func(node *ast.Node) {
				declaration := node.AsFunctionDeclaration()
				check(node, declaration.AsteriskToken, declaration.Body)
			},
			ast.KindFunctionExpression: func(node *ast.Node) {
				expression := node.AsFunctionExpression()
				check(node, expression.AsteriskToken, expression.Body)
			},
			ast.KindMethodDeclaration: func(node *ast.Node) {
				method := node.AsMethodDeclaration()
				check(node, method.AsteriskToken, method.Body)
			},
		}
	},
}

// bodyContainsYield reports a yield belonging to this function rather than to a nested one.
//
// The walk stops at every function boundary, since a yield inside an inner function is that
// function's. Descending would let an inner generator's yield satisfy an outer empty one.
func bodyContainsYield(body *ast.Node) bool {
	found := false

	var visit func(*ast.Node)
	visit = func(current *ast.Node) {
		if current == nil || found {
			return
		}
		if current.Kind == ast.KindYieldExpression {
			found = true
			return
		}

		current.ForEachChild(func(child *ast.Node) bool {
			switch child.Kind {
			case ast.KindFunctionDeclaration, ast.KindFunctionExpression,
				ast.KindArrowFunction, ast.KindMethodDeclaration,
				ast.KindClassDeclaration, ast.KindClassExpression:
				// A different function's body, and a different function's yields.
				return false
			}
			visit(child)
			return found
		})
	}
	visit(body)
	return found
}
