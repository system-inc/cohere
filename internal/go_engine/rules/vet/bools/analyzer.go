// Package bools is vet's bools pass, a chain of `||` or `&&` that is redundant or suspect: one operand repeated,
// `e || e`, or one value tested against two constants with the equality that makes the chain always true or always
// false, `x != 1 || x != 2` and `x == 1 && x == 2`. An operand with side effects, a call or a receive, may change
// what the operands after it read, so the chain is judged in runs of side-effect-free operands, each run on its own.
// It sits on the engine's syntax dispatcher, hearing each binary expression on the walk the dispatcher makes once for
// every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/bools/bools.go, whose logic it
// keeps and whose messages it reports word for word, at the same position. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package bools

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "bools"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for common mistakes involving boolean operators"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/bools"

// Listener is what the port hears of each package from the dispatcher: every binary expression, in preorder, as the
// original's Preorder visits them, so a chain's outermost operator comes before the operators inside it, of which it
// judges each chain of `||` or `&&` once.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BinaryExpr)(nil)},
	Node:  checkChain,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// packageState is what the port keeps over the package's walk, held in its context's state.
type packageState struct {
	// expanded holds each `||` and `&&` already judged as part of the chain around it, so a chain is judged once,
	// from its outermost operator. It spans the package, as the original's does.
	expanded map[*ast.BinaryExpr]bool
}

// checkChain judges node when it heads a chain of `||` or `&&` not already judged as part of the chain around it.
func checkChain(context *rule.Context, node ast.Node, _ []ast.Node) {
	binary, isBinary := node.(*ast.BinaryExpr)
	if !isBinary {
		return
	}
	state := rule.State[packageState](context)
	if state.expanded == nil {
		state.expanded = map[*ast.BinaryExpr]bool{}
	}
	if state.expanded[binary] {
		return
	}
	var operator booleanOperator
	switch binary.Op {
	case token.LOR:
		operator = orOperator
	case token.LAND:
		operator = andOperator
	default:
		return
	}
	operands, operators := operator.split(binary)
	for _, inner := range operators {
		state.expanded[inner] = true
	}
	for _, set := range commutativeSets(context.TypesInfo, operands) {
		operator.checkRedundant(context, set)
		operator.checkSuspect(context, set)
	}
}

// commutativeSets is every run of side-effect-free operands, split at each operand with side effects, which belongs
// to no run, since what it does may change what the operands after it read. Given the operands of
// `a || b || f() || c || d` in split's order, it is {{b, a}, {d, c}}.
func commutativeSets(information *types.Info, operands []ast.Expr) [][]ast.Expr {
	sets := [][]ast.Expr{}
	// start is the index of the first operand of the run being gathered.
	start := 0
	for index := 0; index <= len(operands); index++ {
		if index == len(operands) || !hasNoEffects(information, operands[index]) {
			if start < index {
				sets = append(sets, operands[start:index])
			}
			start = index + 1
		}
	}
	return sets
}

// formatted is node's source as go/printer prints it, so two operands written with different spacing read the same.
// x/tools' astutil.Format.
func formatted(fileSet *token.FileSet, node ast.Node) string {
	var builder strings.Builder
	// Ignored: a strings.Builder never fails a write, and printing a type-checked expression has nothing else to fail
	// on; the original drops the error too.
	_ = printer.Fprint(&builder, fileSet, node)
	return builder.String()
}

// hasNoEffects is whether evaluating expression can change nothing a later operand reads: no call other than a
// conversion or a pure builtin, and no receive. x/tools' typesinternal.NoEffects.
func hasNoEffects(information *types.Info, expression ast.Expr) bool {
	noEffects := true
	ast.Inspect(expression, func(node ast.Node) bool {
		switch visited := node.(type) {
		case nil, *ast.Ident, *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr,
			*ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr, *ast.TypeAssertExpr,
			*ast.StarExpr, *ast.CompositeLit,
			// Not expressions, but they may appear within one.
			*ast.KeyValueExpr,
			*ast.FieldList,
			*ast.Field,
			*ast.Ellipsis,
			*ast.IndexListExpr:
			// No effect of its own.

		case *ast.ArrayType,
			*ast.StructType,
			*ast.ChanType,
			*ast.FuncType,
			*ast.MapType,
			*ast.InterfaceType:
			// Type syntax has no effects anywhere within it, so the walk does not descend.
			return false

		case *ast.UnaryExpr:
			// A receive, `<-channel`, takes a value off the channel, so a second receive may read another.
			if visited.Op == token.ARROW {
				noEffects = false
			}

		case *ast.CallExpr:
			// A conversion has no effects, and a pure builtin such as len(e) none of its own, though its operands
			// may.
			if !information.Types[visited.Fun].IsType() && !callsPureBuiltin(information, visited) {
				noEffects = false
			}

		case *ast.FuncLit:
			// A function literal does nothing until it is called, so the walk does not descend into its body.
			return false

		default:
			// Every other expression has effects. Kept for fidelity: in a type-checked expression the cases above
			// leave only *ast.BadExpr, which a package that type-checks never holds.
			noEffects = false
		}
		return noEffects
	})
	return noEffects
}

// callsPureBuiltin is whether call calls a builtin that computes over its operands alone, as an operator does, so
// evaluating it reads no program state. new and make allocate, so two calls give different values, but neither
// changes what another operand reads. x/tools' typesinternal.CallsPureBuiltin.
func callsPureBuiltin(information *types.Info, call *ast.CallExpr) bool {
	identifier, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdentifier {
		return false
	}
	builtin, isBuiltin := information.ObjectOf(identifier).(*types.Builtin)
	if !isBuiltin {
		return false
	}
	switch builtin.Name() {
	// Not append, clear, close, copy, delete, panic, print, println or recover, which act on program state.
	case "len", "cap", "complex", "imag", "real", "make", "new", "max", "min":
		return true
	}
	return false
}
