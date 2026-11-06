package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageUnusedExpression = rule.Message{
	Id: "unusedExpression",
	Description: "This expression is evaluated and then thrown away. Reading a property, comparing " +
		"two values, or naming a variable on a line of its own computes something the program " +
		"immediately forgets, so the line cannot affect what the code does and is almost always a " +
		"typo for something that would: a missing call, a missing `await`, or an assignment whose " +
		"left side was dropped. Use the value or delete the line.",
}

// NoUnusedExpressionsOptions configures which expression forms are tolerated in statement position.
//
// The five names are ESLint's `meta.schema` verbatim. Our rule inventory records this rule as taking
// no options, which is wrong, and the schema is the authoritative surface.
type NoUnusedExpressionsOptions struct {
	// AllowShortCircuit tolerates `a && b()`, where the logical operator is being used for control
	// flow rather than for its value. The right operand still has to be a used form, so `a && b`
	// keeps reporting.
	AllowShortCircuit bool `json:"allowShortCircuit"`

	// AllowTernary tolerates `a ? b() : c()`, the conditional spelling of the same idea. Both
	// branches have to be used forms, because either one can be the branch that runs.
	AllowTernary bool `json:"allowTernary"`

	// AllowTaggedTemplates tolerates ``tag`text` ``, where the tag is a call and may well have an
	// effect. The untagged template `` `text` `` is unaffected and keeps reporting, since it only
	// ever builds a string.
	AllowTaggedTemplates bool `json:"allowTaggedTemplates"`

	// EnforceForJsx extends the rule to `<div />` in statement position, which is off by default in
	// both upstreams because an element expression is a common intermediate while editing.
	EnforceForJsx bool `json:"enforceForJSX"`

	// IgnoreDirectives is inert in this port and deliberately so.
	//
	// ESLint runs two directive checks: one reading the parser's own `.directive` flag, applied
	// always, and a structural re-derivation applied only under this option. The two agree on every
	// input, so the option changes nothing there either. This port has the structural check alone
	// and applies it unconditionally, which reaches the same verdict by one road instead of two.
	// The field exists so that a config writing the name is accepted rather than rejected.
	IgnoreDirectives bool `json:"ignoreDirectives"`
}

// NoUnusedExpressions reports expressions in statement position whose value is discarded.
//
// Valid:
//
//	a = b;
//	f();
//	new Thing();
//	i++;
//	delete foo.bar;
//	void doSomething();
//	'use strict';
//
// Invalid:
//
//	a;
//	foo.bar;
//	a + b;
//	f(), 0;
//	`untagged template`;
//	Map<string, string>;
//	foo!;
//
// The judgment is upstream's and so is the shape: one listener on expression statements, a recursive
// verdict on the expression, and an exemption for a real directive prologue.
//
// The directive exemption is the one piece with no counterpart in oxc's rule file. oxc's parser
// hoists a directive prologue into a separate vector on the enclosing body, so a real directive
// never reaches its listener at all and its `ignore_directives` option is unreachable. TypeScript's
// parser leaves directives as ordinary expression statements, so without the exemption written here
// every `'use client'` at the top of a component file would report.
var NoUnusedExpressions = rule.Rule[NoUnusedExpressionsOptions]{
	Name: "@typescript-eslint/no-unused-expressions",
	Run: func(ruleContext rule.Context, options rule.Given[NoUnusedExpressionsOptions]) rule.Listeners {
		verdict := unusedExpressionVerdict{}
		if parsed, ok := options.Read(); ok {
			verdict.options = parsed
		}

		return rule.Listeners{
			ast.KindExpressionStatement: func(node *ast.Node) {
				if !verdict.isDisallowed(node.Expression()) {
					return
				}
				if isDirectivePrologueStatement(node) {
					return
				}
				ruleContext.ReportNode(node, messageUnusedExpression)
			},
		}
	},
}

// isDirectivePrologueStatement answers whether a statement is a real directive in its position.
//
// This is ESLint's pair of `isTopLevelExpressionStatement` and `directives`. The position half is
// hand-written because no shelf function answers it. `ast.IsPrologueDirective` is exactly
// `Kind == KindExpressionStatement && Expression().Kind == KindStringLiteral` and nothing more,
// verified by reading its body rather than trusting its name: it is positionally blind, so it
// answers true for a string in the middle of a function body and for a string in a class static
// block, both of which upstream reports. It is used below for the shape test alone, which is all it
// can honestly answer, and the position is decided here. A port that took it for the whole question
// would silence eight of the corpus's fail cases.
//
// A template literal is deliberately not a string literal for this purpose, matching both upstreams
// and the runtime: a backticked `use strict` at the top of a function does not enable strict mode.
//
// Two conditions, per the specification's directive prologue:
//
// The container must be one that has a prologue at all: a source file, a module block, or a block
// belonging to something function-like. A class static block is a block whose parent is *not*
// function-like, which is what makes upstream report both strings in one.
//
// And the statement must be inside the unbroken leading run of string statements. The first
// non-string ends the prologue, so a string after a declaration is an ordinary unused expression.
func isDirectivePrologueStatement(node *ast.Node) bool {
	container := node.Parent
	if container == nil {
		return false
	}

	switch container.Kind {
	case ast.KindSourceFile, ast.KindModuleBlock:
	case ast.KindBlock:
		if !ast.IsFunctionLike(container.Parent) {
			return false
		}
	default:
		return false
	}

	// Upstream takes the leading run of string statements and asks whether this node is in it, so
	// the string test comes first: a node that is the container's very first statement is still not
	// a directive unless it is itself a string.
	for _, statement := range container.Statements() {
		if !ast.IsPrologueDirective(statement) {
			return false
		}
		if statement == node {
			return true
		}
	}
	return false
}
