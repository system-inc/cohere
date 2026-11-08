// Package unexiting_test_main is staticcheck's SA3000, a TestMain that runs the tests and never calls os.Exit, so
// before Go 1.15 a failing test run still exits zero, ported as a check on syntax that reports through the kit every
// port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa3000/sa3000.go, whose logic it keeps and
// whose message it reports word for word, over the whole function declaration, where staticcheck's report puts it. It
// asks staticcheck_code.IsCallTo where the original asks code.IsCallTo, and staticcheck_report.StandardLibraryVersion
// where it asks code.StdlibVersion, each the port of the one the original calls, proven against it beside it. Where
// the original asks code.IsOfPointerToTypeWithName, the port asks what that wrapper asks, staticcheck's
// typeutil.IsPointerToTypeWithName of the parameter's type.
//
// The original keeps state across the whole walk of a package's declarations and calls, not across one TestMain
// alone: whether a call to os.Exit was seen is cleared only when a TestMain's walk ends, so a call to os.Exit at
// package level, in a variable's initializer, counts for the TestMain after it, which is then not reported. The port
// keeps the same state in the same walk, so that false negative is the original's and stays (differences'
// exitAtPackageLevel shows it), as does the original's false positive, a TestMain whose os.Exit is in a function it
// calls (upstream's CheckTestMainExit-4). The registry still runs the original, which stays the oracle: the parity
// tests beside this file run both over the same modules, and the two differ nowhere they hold.
package unexiting_test_main

import (
	"go/ast"
	"go/types"
	"go/version"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA3000",
	Doc: "Flags a TestMain that runs the tests without calling os.Exit, before Go 1.15: staticcheck's SA3000, " +
		"ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// exitingVersion is the first Go whose test framework calls os.Exit after TestMain returns, so a TestMain that does
// not is no longer wrong.
const exitingVersion = "go1.15"

// run builds the pass's context and walks the package's declarations and calls.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("unexiting_test_main: the inspect pass's result is missing")
	}
	Check(context, inspected)
	return nil, nil
}

// message is the original's, word for word.
const message = "TestMain should call os.Exit to set exit code"

// Check walks every function declaration and call of the package in order, reporting each TestMain of a standard
// library older than Go 1.15 that calls Run on its *testing.M and never calls os.Exit.
func Check(context *rule.Context, inspected *inspector.Inspector) {
	// What the walk has seen: the TestMain it is inside, nil outside one, and that TestMain's parameter, the
	// *testing.M, and since the last TestMain's end, whether a call to os.Exit was seen, inside a TestMain or not, and
	// whether a call to Run on the parameter was.
	var testMain *ast.FuncDecl
	var parameter types.Object
	callsExit := false
	callsRun := false
	// visit is one step of the walk, entering node when push is set and leaving it otherwise, and whether to walk the
	// node's children.
	visit := func(node ast.Node, push bool) bool {
		if !push {
			if testMain != nil && node == testMain {
				if !callsExit && callsRun {
					staticcheck_report.Report(context, testMain, message)
				}
				testMain = nil
				callsExit = false
				callsRun = false
				parameter = nil
			}
			return true
		}
		switch node := node.(type) {
		case *ast.FuncDecl:
			// Declarations do not nest, so this holds only for a TestMain the walk is already inside.
			if testMain != nil {
				return true
			}
			// Any other function is passed over whole, its calls unseen.
			if !isTestMain(context, node) {
				return false
			}
			if version.Compare(staticcheck_report.StandardLibraryVersion(context, node), exitingVersion) >= 0 {
				return false
			}
			testMain = node
			parameter = context.TypesInfo.ObjectOf(node.Type.Params.List[0].Names[0])
			return true
		case *ast.CallExpr:
			// A call's arguments are not walked once the call is judged, so `os.Exit(m.Run())` counts as an exit
			// alone.
			if staticcheck_code.IsCallTo(context, node, "os.Exit") {
				callsExit = true
				return false
			}
			selector, isSelector := node.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			receiver, isName := selector.X.(*ast.Ident)
			if !isName || parameter != context.TypesInfo.ObjectOf(receiver) {
				return true
			}
			if selector.Sel.Name == "Run" {
				callsRun = true
				return false
			}
			return true
		default:
			// Invariant: the walk hands this function only the declarations and calls it was asked for.
			panic("unexiting_test_main: the inspector handed over a node that is neither a declaration nor a call")
		}
	}
	inspected.Nodes([]ast.Node{(*ast.FuncDecl)(nil), (*ast.CallExpr)(nil)}, visit)
}

// isTestMain is whether declaration is named TestMain and takes one parameter, named, of type *testing.M. A method so
// named counts too, as the original reads it.
func isTestMain(context *rule.Context, declaration *ast.FuncDecl) bool {
	if declaration.Name.Name != "TestMain" || len(declaration.Type.Params.List) != 1 {
		return false
	}
	parameter := declaration.Type.Params.List[0]
	if len(parameter.Names) != 1 {
		return false
	}
	return typeutil.IsPointerToTypeWithName(context.TypesInfo.TypeOf(parameter.Type), "testing.M")
}
