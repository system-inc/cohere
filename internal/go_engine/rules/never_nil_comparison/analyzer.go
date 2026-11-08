// Package never_nil_comparison is staticcheck's SA4031, an if statement that compares with nil a value the function
// itself made and that can never be nil, an allocation, a make, a function or a closure, ported onto staticcheck's
// intermediate representation as a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4031/sa4031.go, whose logic it keeps and
// whose messages it reports word for word, from the start to the end of the comparison, with the related places
// staticcheck's report.Related gives it, each from the start to the end of the value's syntax. The original walks the
// package's if statements in its syntax and finds the function each one belongs to with ir.EnclosingFunction; the
// port walks each function's own syntax, leaving out the function literals inside it, which are functions of their
// own, so each if statement is judged in the function the original finds for it. Neither keeps anything from one if
// statement to the next. The original matches the condition with staticcheck's pattern language, through
// analysis/code, and leaves out what SA4022 reports by matching that check's pattern too; the port matches the same
// shapes in the syntax and the type information directly: a comparison by == or != whose right side is the universe's
// nil and whose left side is no address taken with &, parentheses stripped wherever the pattern strips them. It sorts
// the related values by position as the original does, stably where the original's sort.Slice leaves equal positions
// in an order it does not promise. The original stays the oracle: the parity tests beside this file run both over the
// same packages, related places included, and they differ nowhere.
package never_nil_comparison

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA4031",
	"Flags a comparison with nil of a value that is never nil: staticcheck's SA4031, ported onto its intermediate "+
		"representation.",
	Check,
)

// functionMessage is the original's message for a variable holding a function or a closure.
const functionMessage = "the checked variable contains a function and is never nil; did you mean to call it?"

// namedFunctionMessage is the original's message for a function compared by its own name or another expression.
const namedFunctionMessage = "functions are never nil; did you mean to call it?"

// sourced is a value with syntax, as every value neverNil tracks is.
type sourced interface {
	Source() ast.Node
}

// Check judges one function: each if statement in its own syntax whose condition compares with nil a value the
// function made and that is never nil.
func Check(context *rule.Context, function *ir.Function) {
	outermost := function
	for outermost.Parent() != nil {
		outermost = outermost.Parent()
	}
	if declaration, isDeclaration := outermost.Source().(*ast.FuncDecl); isDeclaration && declaration.Name.Name == "_" {
		// staticcheck builds a function named _, but ir.EnclosingFunction looks a declaration up among the package's
		// members and method sets, where no such function is, so the original judges nothing in one, nor in the
		// function literals inside it.
		return
	}
	var body *ast.BlockStmt
	switch syntax := function.Source().(type) {
	case *ast.FuncDecl:
		body = syntax.Body
	case *ast.FuncLit:
		body = syntax.Body
	}
	if body == nil {
		// A function with no syntax of its own, the package's initializer, holds no if statement but in the function
		// literals of its variables, which are functions of their own.
		return
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.IfStmt:
			checkIf(context, function, node)
		}
		return true
	})
}

// checkIf reports statement's condition when it compares with nil a value of function that is never nil.
func checkIf(context *rule.Context, function *ir.Function, statement *ast.IfStmt) {
	comparison, isComparison := ast.Unparen(statement.Cond).(*ast.BinaryExpr)
	if !isComparison || comparison.Op != token.EQL && comparison.Op != token.NEQ ||
		!isUniverseNil(context.TypesInfo, comparison.Y) {
		return
	}
	compared := ast.Unparen(comparison.X)
	if address, isUnary := compared.(*ast.UnaryExpr); isUnary && address.Op == token.AND {
		// Don't duplicate diagnostics reported by SA4022, the address of a variable compared with nil.
		return
	}
	value, isAddress := function.ValueForExpr(compared)
	if isAddress {
		return
	}
	values, isNeverNil := neverNilValues(value)
	if !isNeverNil {
		return
	}
	qualifier := "always"
	if comparison.Op == token.EQL {
		qualifier = "never"
	}
	diagnostic := analysis.Diagnostic{
		Pos:     comparison.Pos(),
		End:     comparison.End(),
		Message: fmt.Sprintf("this nil check is %s true", qualifier),
	}
	_, isFunction := value.(*ir.Function)
	identifier, isIdentifier := compared.(*ast.Ident)
	isVariable := false
	if isIdentifier {
		_, isVariable = context.TypesInfo.ObjectOf(identifier).(*types.Var)
	}
	if isVariable {
		if value.Parent() == function {
			diagnostic.Related = related(values, identifier.Name)
		}
		if _, isClosure := value.(*ir.MakeClosure); isClosure || isFunction {
			diagnostic.Message = functionMessage
		}
	} else if isFunction {
		diagnostic.Message = namedFunctionMessage
	}
	context.Report(diagnostic)
}

// isUniverseNil is whether expression is the identifier nil, parentheses stripped, naming the universe's nil and not
// a declaration that shadows it.
func isUniverseNil(typesInfo *types.Info, expression ast.Expr) bool {
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	return isIdentifier && identifier.Name == "nil" && typesInfo.ObjectOf(identifier) == types.Universe.Lookup("nil")
}

// neverNilValues is whether value is never nil, because the function made it as an allocation, a make, a function
// or a closure, or as a slicing or a field's address of one, through phis, and the values it was made from, sorted by
// position: each of them where value is no phi, and each edge where it is, for the related places of a finding.
func neverNilValues(value ir.Value) ([]ir.Value, bool) {
	seen := map[ir.Value]bool{}
	values := []ir.Value{}
	var neverNil func(value ir.Value, isTracked bool) bool
	neverNil = func(value ir.Value, isTracked bool) bool {
		if seen[value] {
			return true
		}
		seen[value] = true
		switch value := value.(type) {
		case *ir.MakeClosure, *ir.Function, *ir.MakeChan, *ir.MakeMap, *ir.MakeSlice, *ir.Alloc:
			if isTracked {
				values = append(values, value)
			}
			return true
		case *ir.Slice:
			if isTracked {
				values = append(values, value)
			}
			return neverNil(value.X, false)
		case *ir.FieldAddr:
			if isTracked {
				values = append(values, value)
			}
			return neverNil(value.X, false)
		case *ir.Phi:
			for _, edge := range value.Edges {
				if !neverNil(edge, true) {
					return false
				}
			}
			return true
		}
		return false
	}
	if value == nil || !neverNil(value, true) {
		// A value of nil is an expression the function holds no value for, which the original's walk rejects too.
		return nil, false
	}
	slices.SortStableFunc(values, func(first ir.Value, second ir.Value) int {
		return cmp.Compare(first.Pos(), second.Pos())
	})
	return values, true
}

// related is a related place for each of values that has syntax, naming the variable name holds, as
// staticcheck's report.Related gives them: a value without syntax has no place, and is left out.
func related(values []ir.Value, name string) []analysis.RelatedInformation {
	message := fmt.Sprintf("this is the value of %s", name)
	if len(values) != 1 {
		message = fmt.Sprintf("this is one of the value of %s", name)
	}
	places := []analysis.RelatedInformation{}
	for _, value := range values {
		withSource, hasSource := value.(sourced)
		if !hasSource || withSource.Source() == nil {
			continue
		}
		places = append(places, analysis.RelatedInformation{
			Pos:     withSource.Source().Pos(),
			End:     withSource.Source().End(),
			Message: message,
		})
	}
	return places
}
