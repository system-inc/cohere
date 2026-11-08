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
// calls (upstream's CheckTestMainExit-4). The registry runs the port in the original's place, under its name, and the
// original stays only as the oracle: the parity tests beside this file run both over the same modules, and the two
// differ nowhere they hold.
package unexiting_test_main

import (
	"go/ast"
	"go/types"
	"go/version"
	"slices"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every function declaration and call, in
// preorder, and each file as the walk leaves it, where a TestMain the walk was inside has ended.
var Listener = rule.Listener{
	Nodes:    []ast.Node{(*ast.FuncDecl)(nil), (*ast.CallExpr)(nil)},
	Node:     Check,
	FileExit: checkFileExit,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA3000",
	"SA3000",
	"Flags a TestMain that runs the tests without calling os.Exit, before Go 1.15: staticcheck's SA3000, "+
		"ported as a check on syntax.",
	&Listener,
)

// exitingVersion is the first Go whose test framework calls os.Exit after TestMain returns, so a TestMain that does
// not is no longer wrong.
const exitingVersion = "go1.15"

// message is the original's, word for word.
const message = "TestMain should call os.Exit to set exit code"

// walk is what the walk of the package's declarations and calls has seen, kept across it in rule.State as the
// original keeps it across its one inspector walk: the TestMain it is inside, nil outside one, and that TestMain's
// parameter, the *testing.M, and since the last TestMain's end, whether a call to os.Exit was seen, inside a TestMain
// or not, and whether a call to Run on the parameter was. skipped is the last node whose children the original's walk
// does not enter, nil before any, since the dispatcher hands every node and the port passes over those itself.
type walk struct {
	testMain  *ast.FuncDecl
	parameter types.Object
	callsExit bool
	callsRun  bool
	skipped   ast.Node
}

// Check is one step of the walk over every function declaration and call of the package in order, reporting each
// TestMain of a standard library older than Go 1.15 that calls Run on its *testing.M and never calls os.Exit. The
// original's walk leaves a TestMain before it enters anything after it, so a TestMain no longer among node's
// ancestors has ended, and is judged before node is; and node is passed over when an ancestor's children are, as the
// original never enters them.
func Check(context *rule.Context, node ast.Node, stack []ast.Node) {
	state := rule.State[walk](context)
	if state.testMain != nil && !slices.Contains(stack, ast.Node(state.testMain)) {
		leaveTestMain(context, state)
	}
	if state.skipped != nil && slices.Contains(stack, state.skipped) {
		return
	}
	if !enter(context, state, node) {
		state.skipped = node
	}
}

// checkFileExit judges the TestMain the walk was inside, if any, which ends with its file at the latest.
func checkFileExit(context *rule.Context, _ *ast.File) {
	state := rule.State[walk](context)
	if state.testMain != nil {
		leaveTestMain(context, state)
	}
}

// leaveTestMain is the walk leaving the TestMain it was inside: it is reported if it called Run and never os.Exit,
// and what the walk has seen starts again.
//
// Mutates: state is cleared for the walk past the TestMain, the walk's own state, kept in rule.State for it
func leaveTestMain(context *rule.Context, state *walk) {
	if !state.callsExit && state.callsRun {
		staticcheck_report.Report(context, state.testMain, message)
	}
	state.testMain = nil
	state.callsExit = false
	state.callsRun = false
	state.parameter = nil
}

// enter is the walk entering node, and whether to walk the node's children.
//
// Mutates: state records the TestMain entered and the calls seen, the walk's own state, kept in rule.State for it
func enter(context *rule.Context, state *walk, node ast.Node) bool {
	switch node := node.(type) {
	case *ast.FuncDecl:
		// Declarations do not nest, so this holds only for a TestMain the walk is already inside.
		if state.testMain != nil {
			return true
		}
		// Any other function is passed over whole, its calls unseen.
		if !isTestMain(context, node) {
			return false
		}
		if version.Compare(staticcheck_report.StandardLibraryVersion(context, node), exitingVersion) >= 0 {
			return false
		}
		state.testMain = node
		state.parameter = context.TypesInfo.ObjectOf(node.Type.Params.List[0].Names[0])
		return true
	case *ast.CallExpr:
		// A call's arguments are not walked once the call is judged, so `os.Exit(m.Run())` counts as an exit
		// alone.
		if staticcheck_code.IsCallTo(context, node, "os.Exit") {
			state.callsExit = true
			return false
		}
		selector, isSelector := node.Fun.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		receiver, isName := selector.X.(*ast.Ident)
		if !isName || state.parameter != context.TypesInfo.ObjectOf(receiver) {
			return true
		}
		if selector.Sel.Name == "Run" {
			state.callsRun = true
			return false
		}
		return true
	default:
		// Invariant: the dispatcher hands Node only the declarations and calls it was asked for.
		panic("unexiting_test_main: the dispatcher handed over a node that is neither a declaration nor a call")
	}
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
