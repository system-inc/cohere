// Package correctness_require_invariant_reason is the Go engine's rule that a panic says why it can never happen,
// and that a Must function, which panics for its caller, runs only while the package initializes.
//
// A panic is a claim that a case cannot happen, and when the claim is wrong the process stops with a stack trace
// instead of a sentence. Written down beside the panic, the claim tells the next reader what they would break; left
// out, it is a crash nobody can reason about. A Must function makes the same claim about its input, and a package's
// var initializers and init functions run once, at start, so a bad constant there stops the program before it does
// anything. Called inside a function that runs later, it stops the process in the middle of its work.
package correctness_require_invariant_reason

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-invariant-reason"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_invariant_reason",
	Doc: "Flags a call to the builtin panic with no // Invariant: <reason> comment directly above its statement or " +
		"at the end of its line, except a re-panic of the value recover returned in the same function, and a call " +
		"to a function or method named Must or Must followed by an upper-case letter outside package " +
		"initialization: a package-level var initializer or an init function, and a function literal either calls " +
		"on the spot. Files ending in _test.go are not judged.",
	Requires: []*analysis.Analyzer{marker.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var unexplainedPanicMessage = policy.MessageOf(Name, "unexplainedPanic")

var invariantWithoutReasonMessage = policy.MessageOf(Name, "invariantWithoutReason")

var mustOutsideInitializationMessage = policy.MessageOf(Name, "mustOutsideInitialization")

// invariantMarker opens the comment that says why a panic can never happen, matched as written.
const invariantMarker = "Invariant:"

func run(pass *analysis.Pass) (any, error) {
	indexes, isIndexes := pass.ResultOf[marker.Analyzer].(*marker.Indexes)
	if !isIndexes {
		return nil, fmt.Errorf("reading the comment indexes: marker gave %T", pass.ResultOf[marker.Analyzer])
	}
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it: a test's panic and Must calls fail one test, not
		// a running program, so test files are not judged.
		if strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		judgePanics(pass, file, indexes.ForFile(file))
		judgeMustCalls(pass, file)
	}
	return nil, nil
}

// judgePanics reports every builtin panic in file that is not a re-panic and whose comments give no reason.
func judgePanics(pass *analysis.Pass, file *ast.File, comments *marker.Index) {
	written := writtenVariables(pass, file)
	ast.Inspect(file, func(node ast.Node) bool {
		// Each function is judged on its own, so a recover in one never excuses a panic in a closure it holds.
		switch function := node.(type) {
		case *ast.FuncDecl:
			if function.Body != nil {
				judgeFunctionPanics(pass, function.Body, comments, written)
			}
		case *ast.FuncLit:
			judgeFunctionPanics(pass, function.Body, comments, written)
		}
		return true
	})
}

// judgeFunctionPanics judges the panics directly in one function's body, leaving the function literals inside it to
// judgePanics, which reaches each of them on its own.
func judgeFunctionPanics(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	comments *marker.Index,
	written map[*types.Var]bool,
) {
	recovered := recoveredVariables(pass, body, written)
	inspectFunction(body, func(node ast.Node) {
		// panic returns no value, so a call to it is always a statement of its own, or the call a defer or go runs.
		var call *ast.CallExpr
		switch statement := node.(type) {
		case *ast.ExprStmt:
			call, _ = ast.Unparen(statement.X).(*ast.CallExpr)
		case *ast.DeferStmt:
			call = statement.Call
		case *ast.GoStmt:
			call = statement.Call
		}
		if call == nil || !isBuiltin(pass, call, "panic") {
			return
		}
		if len(call.Args) == 1 && isRecoveredValue(pass, call.Args[0], recovered) {
			return
		}
		message := unexplainedPanicMessage
		// The comment directly above the statement, on lines of its own, or at the end of its last line.
		switch comments.ExcuseFor(node, invariantMarker) {
		case marker.WithReason:
			return
		case marker.WithoutReason:
			message = invariantWithoutReasonMessage
		case marker.None:
		}
		pass.Report(analysis.Diagnostic{
			Pos:      call.Pos(),
			End:      call.End(),
			Category: message.Id,
			Message:  message.Render(map[string]string{}),
		})
	})
}

// inspectFunction visits every node of one function's body without entering the function literals inside it.
func inspectFunction(body *ast.BlockStmt, visit func(node ast.Node)) {
	ast.Inspect(body, func(node ast.Node) bool {
		if _, isLiteral := node.(*ast.FuncLit); isLiteral {
			return false
		}
		if node != nil {
			visit(node)
		}
		return true
	})
}

// recoveredVariables is every variable body declares holding what recover returned, `recovered := recover()` or `var
// recovered = recover()`, that nothing writes again, so a panic of it re-raises the value recover caught.
func recoveredVariables(pass *analysis.Pass, body *ast.BlockStmt, written map[*types.Var]bool) map[*types.Var]bool {
	recovered := map[*types.Var]bool{}
	declare := func(names []*ast.Ident, values []ast.Expr) {
		// Only one value per name pairs them; `first, second := pair()` assigns from one call's results.
		if len(names) != len(values) {
			return
		}
		for index, name := range names {
			variable, isVariable := pass.TypesInfo.Defs[name].(*types.Var)
			value, isCall := ast.Unparen(values[index]).(*ast.CallExpr)
			if isVariable && isCall && isBuiltin(pass, value, "recover") && !written[variable] {
				recovered[variable] = true
			}
		}
	}
	inspectFunction(body, func(node ast.Node) {
		switch declaration := node.(type) {
		case *ast.AssignStmt:
			if declaration.Tok != token.DEFINE {
				return
			}
			names := make([]*ast.Ident, 0, len(declaration.Lhs))
			for _, left := range declaration.Lhs {
				// A define's left side is identifiers only; the compiler refuses anything else.
				if name, isIdentifier := left.(*ast.Ident); isIdentifier {
					names = append(names, name)
				}
			}
			declare(names, declaration.Rhs)
		case *ast.ValueSpec:
			declare(declaration.Names, declaration.Values)
		}
	})
	return recovered
}

// writtenVariables is every variable file writes after declaring it: assigned, ranged into with `=`, or given away
// by its address. Such a variable may hold something other than what it was declared with when a panic reads it.
func writtenVariables(pass *analysis.Pass, file *ast.File) map[*types.Var]bool {
	written := map[*types.Var]bool{}
	mark := func(expression ast.Expr) {
		// A declaration's own name is in Defs, never Uses, so only a later write is marked.
		if name, isIdentifier := ast.Unparen(expression).(*ast.Ident); isIdentifier {
			if variable, isVariable := pass.TypesInfo.Uses[name].(*types.Var); isVariable {
				written[variable] = true
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch write := node.(type) {
		case *ast.AssignStmt:
			for _, left := range write.Lhs {
				mark(left)
			}
		case *ast.RangeStmt:
			if write.Tok == token.ASSIGN {
				mark(write.Key)
				mark(write.Value)
			}
		case *ast.UnaryExpr:
			if write.Op == token.AND {
				mark(write.X)
			}
		}
		return true
	})
	return written
}

// isRecoveredValue is whether argument is what recover returned in this function: the call itself, or a variable
// holding it.
func isRecoveredValue(pass *analysis.Pass, argument ast.Expr, recovered map[*types.Var]bool) bool {
	switch value := ast.Unparen(argument).(type) {
	case *ast.CallExpr:
		return isBuiltin(pass, value, "recover")
	case *ast.Ident:
		variable, isVariable := pass.TypesInfo.Uses[value].(*types.Var)
		return isVariable && recovered[variable]
	}
	return false
}

// isBuiltin is whether call calls the predeclared builtin of that name, found by its object, so a function or
// variable of the same name declared anywhere is never mistaken for it.
func isBuiltin(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	callee := typeutil.Callee(pass.TypesInfo, call)
	return callee != nil && callee == types.Universe.Lookup(name)
}

// judgeMustCalls reports every call to a Must function in file that does not run while the package initializes.
func judgeMustCalls(pass *analysis.Pass, file *ast.File) {
	for _, declaration := range file.Decls {
		// An init function runs once, at start, after the package's vars are set, so its body is initialization.
		// Only a top-level function without receiver or type parameters named init is one; a method named init is
		// called like any other.
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil &&
			function.Recv == nil && function.Type.TypeParams == nil && function.Name.Name == "init" {
			judgeMustCallsIn(pass, function.Body, true)
			continue
		}
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			judgeMustCallsIn(pass, declaration, false)
			continue
		}
		for _, specification := range general.Specs {
			// A var declaration holds only value specifications; the parser builds nothing else under one.
			if value, isValue := specification.(*ast.ValueSpec); isValue {
				for _, initializer := range value.Values {
					judgeMustCallsIn(pass, initializer, true)
				}
			}
		}
	}
}

// judgeMustCallsIn reports the Must calls under root, unless root runs while the package initializes, where a Must
// call runs once at start. A function literal there that is called on the spot runs then too, so it stays
// initialization; one stored, or started by go, runs whenever it is called, so its body is judged as any other.
func judgeMustCallsIn(pass *analysis.Pass, root ast.Node, packageInitializer bool) {
	ast.Inspect(root, func(node ast.Node) bool {
		if !packageInitializer {
			return reportMustCall(pass, node)
		}
		switch initialization := node.(type) {
		case *ast.GoStmt:
			// A goroutine started during initialization may run after main has begun its work.
			judgeMustCallsIn(pass, initialization.Call, false)
			return false
		case *ast.CallExpr:
			if literal, isLiteral := ast.Unparen(initialization.Fun).(*ast.FuncLit); isLiteral {
				judgeMustCallsIn(pass, literal.Body, true)
				for _, argument := range initialization.Args {
					judgeMustCallsIn(pass, argument, true)
				}
				return false
			}
		case *ast.FuncLit:
			judgeMustCallsIn(pass, initialization.Body, false)
			return false
		}
		return true
	})
}

// reportMustCall reports node when it is a call to a Must function, and always lets the walk go on below it.
func reportMustCall(pass *analysis.Pass, node ast.Node) bool {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return true
	}
	function, isFunction := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !isFunction || !isMustName(function.Name()) {
		return true
	}
	pass.Report(analysis.Diagnostic{
		Pos:      call.Pos(),
		End:      call.End(),
		Category: mustOutsideInitializationMessage.Id,
		Message:  mustOutsideInitializationMessage.Render(map[string]string{"name": calleeName(pass, function)}),
	})
	return true
}

// isMustName is whether name is Must, as template.Must, or Must followed by an upper-case letter, as
// regexp.MustCompile, so Mustard and Muster are other words.
func isMustName(name string) bool {
	rest, found := strings.CutPrefix(name, "Must")
	if !found {
		return false
	}
	if rest == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(rest)
	return unicode.IsUpper(first)
}

// calleeName is function's name as a reader of this package would write the call: qualified by its package when it
// is another package's function, and bare when it is this package's or a method, whose receiver the call shows.
func calleeName(pass *analysis.Pass, function *types.Func) string {
	if function.Signature().Recv() != nil || function.Pkg() == nil || function.Pkg() == pass.Pkg {
		return function.Name()
	}
	return function.Pkg().Name() + "." + function.Name()
}
