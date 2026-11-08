// Package redundant_make_arguments is staticcheck's S1019, a call of the builtin make that spells out a length or a
// capacity make already defaults to, `make(chan T, 0)` for a channel's zero buffer and `make([]T, n, n)` for a
// slice's capacity equal to its length, ported as a check on types and syntax that reports through the kit every port
// of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1019/s1019.go, whose logic it keeps and whose
// messages it reports word for word, at the same range, the argument make defaults. The original matches two queries,
// `(CallExpr (Builtin "make") [typ size@(IntegerLiteral "0")])` and `(CallExpr (Builtin "make") [typ size size])`,
// through staticcheck's pattern language with code.Match, which consults no type index; the port asks the same of
// each call directly, reading pattern/match.go for what each construct of the queries matches, and reports through
// staticcheck_report.Report where the original reports through report.Report. It suggests no fix, as the original
// does not, and keeps nothing from one call to the next.
//
// It parts from the original in one place, where the original is wrong: a length that receives from a channel is
// read anew for the capacity, so `make([]T, <-c, <-c)` receives twice and is not `make([]T, <-c)`, as the original's
// own `TODO` says. The port drops those findings. The registry still runs the original, which stays the oracle: the
// parity tests beside this file run both over the same packages, and the differences fixture records each drop.
package redundant_make_arguments

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("redundant_make_arguments: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1019",
	"S1019",
	"Flags a make call that spells out a length or capacity make defaults to: staticcheck's S1019, ported.",
	&Listener,
)

// Check judges one call: the builtin make given a channel type and a size written as the integer literal 0, which
// is the unbuffered channel make makes without it, or given any type and a length and a capacity that are the same
// syntax and receive from no channel, which is the capacity make gives a slice without it.
func Check(context *rule.Context, call *ast.CallExpr) {
	// The original leaves alone the runtime's own tests of map creation, which spell out make's defaults on purpose:
	// the package runtime_test, in a file whose base name is map_test.go. It reads the name a position displays, so
	// a line directive naming map_test.go is skipped there too, and one naming another file in map_test.go is not.
	if context.Package.Path() == "runtime_test" &&
		filepath.Base(context.Fset.Position(call.Pos()).Filename) == "map_test.go" {
		return
	}
	// `(Builtin "make")` is the universe's make, named by an identifier parenthesized or not; a make the code
	// declares itself, in a block or at package scope, is not it. The list of patterns is exactly so many arguments,
	// and the two queries ask for two and three, so at most one of them applies to a call.
	if !staticcheck_code.IsBuiltin(context, call.Fun, "make") {
		return
	}
	switch len(call.Args) {
	case 2:
		checkChannelSize(context, call)
	case 3:
		checkSliceCapacity(context, call)
	}
}

// checkChannelSize judges a make of two arguments by the first query, `[typ size@(IntegerLiteral "0")]`: a size
// written as an integer literal whose constant prints as 0, given a type whose core type is a channel.
func checkChannelSize(context *rule.Context, call *ast.CallExpr) {
	// The pattern's matcher looks through parentheses before it binds, so each binding holds its argument with them
	// removed: the message renders the type without them, and the finding spans the literal inside them.
	typeExpression := ast.Unparen(call.Args[0])
	size := ast.Unparen(call.Args[1])
	// `(IntegerLiteral "0")` compares the String "0" with the literal's recorded constant as it prints, not by
	// constant.Compare: 0, 0x0, 00 and -0 all print as 0, and a named constant, a conversion such as int(0), a
	// character or float literal and arithmetic such as 1-1 are no integer literal, whatever their value.
	literal, isLiteral := staticcheck_code.IntegerLiteral(context, size)
	if !isLiteral || literal.Value.String() != "0" {
		return
	}
	// The original asks the core type, so a type parameter whose every type is a channel of one element, its
	// directions agreeing, is a channel here, and one that holds a slice is not.
	core := typeutil.CoreType(context.TypesInfo.TypeOf(typeExpression))
	if core == nil {
		// The original calls Underlying on the core type unguarded and would panic here, but make type-checks only
		// on a type with a core type, and the checker passes over a package that does not type-check, so this is
		// never reached; a call without one is no channel's.
		return
	}
	if _, isChannel := core.Underlying().(*types.Chan); !isChannel {
		return
	}
	staticcheck_report.Report(context, size,
		fmt.Sprintf("should use make(%s) instead", staticcheck_report.Render(context, typeExpression)),
		staticcheck_report.FilterGenerated())
}

// checkSliceCapacity judges a make of three arguments by the second query, `[typ size size]`: a capacity that is
// the same syntax as the length, of any type, since only a slice's make takes three arguments, unless the length
// receives from a channel, where the original is wrong.
func checkSliceCapacity(context *rule.Context, call *ast.CallExpr) {
	typeExpression := ast.Unparen(call.Args[0])
	length := ast.Unparen(call.Args[1])
	// The second size recalls the node the first bound, which match.go's matchAST compares as syntax: parentheses
	// looked through at every depth and identifiers by name, so two calls f() are the same, and so are two receives
	// <-channel, which the next judgment refuses.
	if !staticcheck_code.SameSyntax(length, call.Args[2]) {
		return
	}
	// Here the port parts from the original, which is wrong where the length receives: each receive takes the next
	// value from its channel, so the capacity is another value and dropping it receives once where the code receives
	// twice. A call may also answer anew each time, but only staticcheck's purity pass could tell, so a call stays
	// reported as the original reports it, since dropping it would lose the findings that are right.
	if receives(length) {
		return
	}
	staticcheck_report.Report(context, length,
		fmt.Sprintf("should use make(%s, %s) instead", staticcheck_report.Render(context, typeExpression),
			staticcheck_report.Render(context, length)),
		staticcheck_report.FilterGenerated())
}

// receives is whether evaluating expression receives from a channel: a receive anywhere in it but inside a function
// literal, whose body runs only when it is called, and a call of a literal with a result is never the same syntax as
// another, since the pattern language never finds a field list with an untagged field the same as itself.
func receives(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.UnaryExpr:
			if node.Op == token.ARROW {
				found = true
			}
		}
		return !found
	})
	return found
}
