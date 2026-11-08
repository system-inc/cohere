// Package bools is vet's bools pass, a chain of `||` or `&&` that is redundant or suspect: one operand repeated,
// `e || e`, or one value tested against two constants with the equality that makes the chain always true or always
// false, `x != 1 || x != 2` and `x == 1 && x == 2`. An operand with side effects, a call or a receive, may change
// what the operands after it read, so the chain is judged in runs of side-effect-free operands, each run on its own.
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
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name:     "bools",
	Doc:      "check for common mistakes involving boolean operators",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/bools",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("bools: inspect's result is missing")
	}
	// expanded holds each `||` and `&&` already judged as part of the chain around it, so a chain is judged once,
	// from its outermost operator.
	expanded := map[*ast.BinaryExpr]bool{}
	// All visits each binary expression in preorder, as the original's Preorder does, and hands it over typed, so a
	// chain's outermost operator comes before the operators inside it.
	for binary := range inspector.All[*ast.BinaryExpr](inspected) {
		if expanded[binary] {
			continue
		}
		var operator booleanOperator
		switch binary.Op {
		case token.LOR:
			operator = orOperator
		case token.LAND:
			operator = andOperator
		default:
			continue
		}
		operands, operators := operator.split(binary)
		for _, inner := range operators {
			expanded[inner] = true
		}
		for _, set := range commutativeSets(pass.TypesInfo, operands) {
			operator.checkRedundant(pass, set)
			operator.checkSuspect(pass, set)
		}
	}
	return nil, nil
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
