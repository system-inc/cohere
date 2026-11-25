// Package correctness_require_invariant_reason is cohere-go's rule that a panic says why it can never happen,
// and that a Must function, which panics for its caller, runs only while the package initializes.
//
// A panic is a claim that a case cannot happen, and when the claim is wrong the process stops with a stack trace
// instead of a sentence. Written down beside the panic, the claim tells the next reader what they would break; left
// out, it is a crash nobody can reason about. A Must function makes the same claim about its input, and a package's
// var initializers and init functions run once, at start, so a bad constant there stops the program before it does
// anything. Called inside a function that runs later, it stops the process in the middle of its work.
package correctness_require_invariant_reason

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_language/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-invariant-reason"

// listener judges each file whole as the syntax dispatcher's walk enters it, walking the file itself, once: a panic
// is judged against every write of its recovered variables, a write after the panic included, so the panics are judged
// only once the walk has gathered the whole file, a panic's recovered variables are its own function's and none of the
// literals inside it, and a Must call is judged by whether it runs while the package initializes, which a second walk,
// taken only of a file that calls a Must function, carries down from the declaration holding it and through each
// function literal, never entering a var's names or type.
var listener = rule.Listener{
	FileLocal: true, Requires: []*analysis.Analyzer{marker.Analyzer}, FileEnter: judgeFile}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "correctness_require_invariant_reason", "Flags a call to the builtin "+
	"panic with no // Invariant: <reason> comment directly above its statement or at the end of its line, except a "+
	"re-panic of the value recover returned in the same function, and a call to a function or method named Must or "+
	"Must followed by an upper-case letter outside package initialization: a package-level var initializer or an "+
	"init function, and a function literal either calls on the spot. Files ending in _test.go are not judged.",
	&listener)

// Rule is this rule's entry in cohere-go's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var unexplainedPanicMessage = policy.MessageOf(Name, "unexplainedPanic")

var invariantWithoutReasonMessage = policy.MessageOf(Name, "invariantWithoutReason")

var mustOutsideInitializationMessage = policy.MessageOf(Name, "mustOutsideInitialization")

// judgeFile judges file's panics, then its Must calls.
func judgeFile(context *rule.Context, file *ast.File) {
	// The file's own name, not one a //line directive gives it: a test's panic and Must calls fail one test, not a
	// running program, so test files are not judged.
	if strings.HasSuffix(context.FileSet.PositionFor(file.Package, false).Filename, "_test.go") {
		return
	}
	indexes, isIndexes := context.ResultOf(marker.Analyzer).(*marker.Indexes)
	if !isIndexes {
		// Invariant: marker's result is always its Indexes, so another is a mistake in cohere-go, and the
		// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
		panic(fmt.Sprintf("reading the comment indexes: marker gave %T", context.ResultOf(marker.Analyzer)))
	}
	judgeSyntax(context, file, indexes.ForFile(file))
}

// judgeSyntax judges file's panics against comments, its comment index, then its Must calls.
func judgeSyntax(context *rule.Context, file *ast.File, comments *marker.Index) {
	walk := &syntaxWalk{context: context, functions: make([]openFunction, 0, 8)}
	ast.Walk(walk, file)
	judgePanics(context, walk, comments)
	// Only a call whose callee is a Must function is ever reported as one, so a file holding none reports nothing
	// however its initialization is read, and is not walked again.
	if walk.holdsMustCall {
		judgeMustCalls(context, file)
	}
}

// judgePanics reports every panic walk gathered that is not a re-panic and whose comments give no reason: each
// function's panics in the order they stand, and the functions in the order they begin, an outer function's panics
// before those of a literal it holds.
func judgePanics(context *rule.Context, walk *syntaxWalk, comments *marker.Index) {
	// The walk meets a literal's panics before any of its outer function's that come after the literal, so the panics
	// are put in their functions' order; stable, so each function's keep the order they stand in.
	slices.SortStableFunc(walk.panics, func(first panicStatement, second panicStatement) int {
		return cmp.Compare(first.function, second.function)
	})
	for _, gathered := range walk.panics {
		if len(gathered.call.Args) == 1 && walk.isRecoveredValue(gathered.call.Args[0], gathered.function) {
			continue
		}
		message := unexplainedPanicMessage
		// The comment directly above the statement, on lines of its own, or at the end of its last line.
		switch comments.ExcuseFor(gathered.statement, marker.Invariant) {
		case marker.WithReason:
			continue
		case marker.WithoutReason:
			message = invariantWithoutReasonMessage
		case marker.None:
		}
		context.Report(analysis.Diagnostic{
			Pos:      gathered.call.Pos(),
			End:      gathered.call.End(),
			Category: message.Id,
			Message:  message.Render(map[string]string{}),
		})
	}
}

// isBuiltin is whether call calls the predeclared builtin of that name, found by its object, so a function or
// variable of the same name declared anywhere is never mistaken for it.
func isBuiltin(context *rule.Context, call *ast.CallExpr, name string) bool {
	callee := typeutil.Callee(context.TypesInfo, call)
	return callee != nil && callee == types.Universe.Lookup(name)
}

// judgeMustCalls reports every call to a Must function in file that does not run while the package initializes.
func judgeMustCalls(context *rule.Context, file *ast.File) {
	for _, declaration := range file.Decls {
		// An init function runs once, at start, after the package's vars are set, so its body is initialization.
		// Only a top-level function without receiver or type parameters named init is one; a method named init is
		// called like any other.
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil &&
			function.Recv == nil && function.Type.TypeParams == nil && function.Name.Name == "init" {
			judgeMustCallsIn(context, function.Body, true)
			continue
		}
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			judgeMustCallsIn(context, declaration, false)
			continue
		}
		for _, specification := range general.Specs {
			// A var declaration holds only value specifications; the parser builds nothing else under one.
			if value, isValue := specification.(*ast.ValueSpec); isValue {
				for _, initializer := range value.Values {
					judgeMustCallsIn(context, initializer, true)
				}
			}
		}
	}
}

// judgeMustCallsIn reports the Must calls under root, unless root runs while the package initializes, where a Must
// call runs once at start. A function literal there that is called on the spot runs then too, so it stays
// initialization; one stored, or started by go, runs whenever it is called, so its body is judged as any other.
func judgeMustCallsIn(context *rule.Context, root ast.Node, packageInitializer bool) {
	ast.Inspect(root, func(node ast.Node) bool {
		if !packageInitializer {
			return reportMustCall(context, node)
		}
		switch initialization := node.(type) {
		case *ast.GoStmt:
			// A goroutine started during initialization may run after main has begun its work.
			judgeMustCallsIn(context, initialization.Call, false)
			return false
		case *ast.CallExpr:
			if literal, isLiteral := ast.Unparen(initialization.Fun).(*ast.FuncLit); isLiteral {
				judgeMustCallsIn(context, literal.Body, true)
				for _, argument := range initialization.Args {
					judgeMustCallsIn(context, argument, true)
				}
				return false
			}
		case *ast.FuncLit:
			judgeMustCallsIn(context, initialization.Body, false)
			return false
		}
		return true
	})
}

// reportMustCall reports node when it is a call to a Must function, and always lets the walk go on below it.
func reportMustCall(context *rule.Context, node ast.Node) bool {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return true
	}
	function, isMust := mustCallee(context, call)
	if !isMust {
		return true
	}
	context.Report(analysis.Diagnostic{
		Pos:      call.Pos(),
		End:      call.End(),
		Category: mustOutsideInitializationMessage.Id,
		Message:  mustOutsideInitializationMessage.Render(map[string]string{"name": calleeName(context, function)}),
	})
	return true
}

// mustCallee is the function call calls, and whether it is a Must function.
func mustCallee(context *rule.Context, call *ast.CallExpr) (*types.Func, bool) {
	function, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	return function, isFunction && isMustName(function.Name())
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
func calleeName(context *rule.Context, function *types.Func) string {
	if function.Signature().Recv() != nil || function.Pkg() == nil || function.Pkg() == context.Package {
		return function.Name()
	}
	return function.Pkg().Name() + "." + function.Name()
}
