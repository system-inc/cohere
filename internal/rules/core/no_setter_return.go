package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
)

// NoSetterReturn flags a `return` carrying a value out of a setter.
//
//	valid:   ({ set foo(val) { return; } })
//	valid:   ({ set foo(val) { if (val) { return; } } })
//	valid:   ({ get foo() { return 1; } })
//	valid:   ({ set: function(val) { return 1; } })
//	valid:   ({ set foo(val) { var inner = function() { return 1; }; } })
//	invalid: ({ set a(val) { return 1; } })
//	invalid: class A { set a(val) { return val; } }
//	invalid: (class { static set a(val) { return this._a; } })
//
// A setter's return value is discarded by the language: assignment evaluates to the right-hand side
// no matter what the setter hands back, so `obj.a = 1` is `1` whether the setter returns `2`, throws
// the value away, or never returns at all. That makes a returning setter one of two things, and both
// are worth a line. Either the value is dead and the `return` is doing nothing, or the author
// believed it would reach a caller, in which case the code is wrong in a way nothing at runtime will
// ever say out loud.
//
// A bare `return` is legal and common, because it is control flow rather than a value. That is the
// discrimination the whole rule turns on, and it is why the check is on the argument rather than on
// the statement.
//
// # Why this is syntactic, and how that was established
//
// Upstream reaches for `ctx.scoping()` here, which usually means the algorithm lives in the semantic
// layer and the rule file is a thin caller. It does not mean that in this case. The two things it
// asks a scope are `is_set_accessor` and `is_function`, and both are properties of the enclosing
// syntactic construct rather than of any name binding. Nothing here resolves an identifier, so
// nothing here needs the checker.
//
// That was measured rather than reasoned. A throwaway probe ran this exact ancestry walk under the
// untyped harness against all 142 of upstream's cases, asserting the per-input diagnostic counts
// taken from the snapshot, and matched on every one. Disabling the function boundary in the same
// probe turned 15 of them red, so the green was a measurement and not a vacuous pass.
//
// The walk stops at the first function-like ancestor because a nested function is its own return
// target. `({ set foo(val) { var inner = function() { return 1; } } })` returns from `inner`, which
// is an ordinary function whose value is used, so nine of upstream's clean cases are exactly this
// shape. Checking only the immediately enclosing function would be wrong in the other direction: a
// `return` inside an `if` inside a `try` inside a setter is still the setter's, which is why this
// walks instead of reading one parent.
//
// # The gap upstream leaves open, reproduced rather than improved on
//
// ESLint also catches setters declared through property descriptors, as in
// `Object.defineProperty(foo, 'bar', { set(val) { return 1; } })`. Upstream carries all twenty-four
// of those as commented-out fail cases and forty related clean cases, so it knows about the family
// and does not implement it. This does not implement it either.
//
// That is a deliberate divergence from ESLint and it is worth stating, because the forty clean
// fixtures make it look tested when what they pin is the absence. Closing it would need the piece
// this rule otherwise does without: `Object` in that call has to be the global rather than a local
// shadowing it, which is a name-resolution question and would pull in the checker along with the
// per-file lock it takes. Upstream's own clean list contains `let Object; Object.defineProperty(...)`
// for precisely that reason.
var NoSetterReturn = rule.Rule{
	Name: "no-setter-return",

	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindReturnStatement: func(node *ast.Node) {
				statement := node.AsReturnStatement()
				// A bare `return` is control flow rather than a value, and every setter in
				// upstream's clean list that returns at all returns bare.
				if statement == nil || statement.Expression == nil {
					return
				}

				if enclosingSetAccessor(node) == nil {
					return
				}

				ctx.ReportNode(node, rule.Message{
					Id: "noSetterReturn",
					Description: "This returns a value from a setter, and the language discards " +
						"it: an assignment evaluates to the value assigned, never to what the " +
						"setter returned, so nothing can ever read this. Either the value is dead " +
						"and the return should be bare, or it was meant to reach a caller and this " +
						"is a bug that stays silent at runtime.",
				})
			},
		}
	},
}

// enclosingSetAccessor returns the setter this node returns from, or nil if it returns from anything
// else.
//
// The first function-like ancestor wins, because that is what a `return` binds to. Reaching a setter
// first means the value is discarded; reaching any other function first means the `return` belongs to
// that function and is somebody else's business, even when a setter encloses it further up.
//
// A top-level `return` reaches neither and answers nil, which is upstream's behavior on the four
// clean cases that return outside any function at all.
func enclosingSetAccessor(node *ast.Node) *ast.Node {
	return ast.FindAncestorOrQuit(node.Parent, func(ancestor *ast.Node) ast.FindAncestorResult {
		if ast.IsSetAccessorDeclaration(ancestor) {
			return ast.FindAncestorTrue
		}
		// Every other function-like construct is its own return target. This arm is what keeps a
		// nested function, arrow, getter, method, or constructor inside a setter from reporting.
		if ast.IsFunctionLike(ancestor) {
			return ast.FindAncestorQuit
		}
		return ast.FindAncestorFalse
	})
}
