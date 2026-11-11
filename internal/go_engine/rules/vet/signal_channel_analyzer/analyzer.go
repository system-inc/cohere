// Package signal_channel_analyzer is vet's sigchanyzer pass, an unbuffered channel handed to signal.Notify. Notify
// never blocks sending a signal, so a signal that arrives while no receiver is ready at an unbuffered channel is
// dropped. The pass judges the channel by how it was made: a call to signal.Notify whose channel is a variable
// declared as the builtin make with one argument, `make(chan os.Signal)`, is reported, with a fix that passes make a
// second argument, 1.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/sigchanyzer/sigchanyzer.go, whose
// message and fix it reports word for word and edit for edit, at the same position, wherever the two report. The
// registry runs the port in the original's place, and the original stays the oracle: the parity tests beside this
// file run both over the same packages, and the differences fixture holds every shape where they disagree. It sits on
// the engine's syntax dispatcher, hearing each declaration with a value and each call on the walk the dispatcher makes
// once for every syntax rule, and judges the calls once the walk has gathered every declaration of the package.
//
// It differs where it is right, in four ways.
//
//   - The original finds a variable's declaration through the parser's resolution of identifiers (ast.Ident.Obj,
//     deprecated since Go 1.22), which resolves a name within its own file alone, so a channel, or a variable holding
//     signal.Notify, declared at package level in another file of the package goes unseen. The port reads the type
//     checker's definitions, across the package's files, and fixes the declaring file.
//   - The original knows Notify only as a selector, `signal.Notify`, or a variable declared as one, so it misses
//     Notify named bare where os/signal is dot-imported. The port compares the object named with os/signal's Notify.
//   - The original reads no parentheses, so `(signal.Notify)(channel)`, `signal.Notify((channel))` and
//     `channel := (make(chan os.Signal))` go unseen. The port looks through them.
//   - The original treats a channel made by any call of one argument as unbuffered, conservatively
//     (golang/go#45043): `newChannel(5)`, a conversion `(chan os.Signal)(buffered)`, a function a package names make,
//     or `(make)(chan os.Signal)` written straight into the call, which its own exemption for make passes over only
//     for want of reading the parentheses. Each finding there is wrong whenever the channel is buffered, and its fix,
//     a size appended to the call's arguments, does not compile. The port reports only the builtin make with one
//     argument, which proves a channel unbuffered, and drops the rest with their fixes.
package signal_channel_analyzer

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "sigchanyzer"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for unbuffered channel of os.Signal\n\n" +
	"This checker reports call expression of the form\n\n" +
	"\tsignal.Notify(c <-chan os.Signal, sig ...os.Signal),\n\n" +
	"where c is an unbuffered channel, which can be at risk of missing the signal."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/sigchanyzer"

// Listener is what the port hears of each package from the dispatcher: every `:=`, every `var` specification and
// every call, in preorder, gathered only in a package that imports os/signal, which it asks once for the package, on
// the first node, and keeps in the context's state. A channel may be declared after the call that passes it, later
// in the file or in another file, so the calls are judged once the walk is done, in the preorder they were heard,
// which is the order the original's walk over the calls reported them.
var Listener = rule.Listener{
	Nodes:       []ast.Node{(*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil), (*ast.CallExpr)(nil)},
	Node:        gather,
	PackageExit: judge,
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

// signalPackagePath is the import path of the package whose Notify the pass judges.
const signalPackagePath = "os/signal"

// builtinMake is the universe's make, which the type checker records as the object of every identifier naming it.
var builtinMake = types.Universe.Lookup("make")

// packageState is what the port gathers of a package, in the context's state, over the walk.
type packageState struct {
	// decided is whether the package's import of os/signal has been asked yet.
	decided bool
	// notify is os/signal's Notify, once decided, or nil when the package does not import os/signal.
	notify *types.Func
	// declarations is every variable the package declares with a value of its own.
	declarations declaredValues
	// calls is every call of the package, in preorder.
	calls []*ast.CallExpr
}

// gather records node, a declaration with a value or a call, in the package's state.
func gather(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[packageState](context)
	if !state.decided {
		state.decided = true
		state.declarations = declaredValues{}
		// A package that does not import os/signal itself cannot name its Notify, so it is passed over unread.
		// Without this the judgment would find nothing there either, so it saves the work and changes no finding.
		signalPackage := importedPackage(context.Package, signalPackagePath)
		if signalPackage != nil {
			notify, isFunction := context.LookupIn(signalPackage, "Notify").(*types.Func)
			if !isFunction {
				// Invariant: os/signal declares Notify, a function, in every release the type checker can load.
				panic("signal_channel_analyzer: os/signal declares no function Notify")
			}
			state.notify = notify
		}
	}
	if state.notify == nil {
		return
	}
	switch heard := node.(type) {
	case *ast.AssignStmt:
		state.declarations.recordAssignment(context.TypesInfo, heard)
	case *ast.ValueSpec:
		state.declarations.recordSpecification(context.TypesInfo, heard)
	case *ast.CallExpr:
		state.calls = append(state.calls, heard)
	}
}

// judge reports each call gathered that passes Notify a channel declared as make with one argument, in the order the
// calls were heard.
func judge(context *rule.Context) {
	state := rule.State[packageState](context)
	// A package that imports no os/signal gathered nothing, and one with no node of a kind heard was never decided.
	if state.notify == nil {
		return
	}
	for _, call := range state.calls {
		judgeCall(context, state, call)
	}
}

// judgeCall reports call when it passes Notify a channel declared as make with one argument, with a fix that gives
// make a second argument, 1.
func judgeCall(context *rule.Context, state *packageState, call *ast.CallExpr) {
	if !callsNotify(context.TypesInfo, state.declarations, state.notify, call) {
		return
	}
	// A call to Notify that type-checks passes its channel, and the pass runs only over packages that type-check, so
	// this guard keeps an unchecked call from panicking and is never taken.
	if len(call.Args) == 0 {
		return
	}
	// Only a channel passed by name is judged. make written straight into the call is exempt, as the original exempts
	// it: no one else holds that channel, so there is no receive to miss a signal (golang/go#45043). Any other
	// expression passed, a call among them, makes a channel whose size the pass cannot know.
	channel, isIdentifier := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !isIdentifier {
		return
	}
	// made is the call the channel was declared as. Only make with one argument proves it unbuffered: a size of zero
	// is a second argument, and any other call's channel may hold any number. An assignment to the variable after its
	// declaration is not read, as the original reads none: the declaration proves the channel unbuffered when
	// declared, and judging what is assigned later would need flow analysis.
	made, isCall := ast.Unparen(state.declarations.valueOf(context.TypesInfo, channel)).(*ast.CallExpr)
	if !isCall || !isBuiltinMake(context.TypesInfo, made) || len(made.Args) != 1 {
		return
	}
	replacement, formatted := withBuffer(made)
	if !formatted {
		return
	}
	context.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: "misuse of unbuffered os.Signal channel as argument to signal.Notify",
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "Change to buffer channel",
			TextEdits: []analysis.TextEdit{{
				Pos:     made.Pos(),
				End:     made.End(),
				NewText: replacement,
			}},
		}},
	})
}

// withBuffer is declaration printed with a second argument, 1, and whether it printed. It prints a copy, since the
// syntax tree is shared with every other analyzer (golang/go#46129), and prints it against an empty file set, as the
// original does, so the text is laid out afresh on one line rather than following the source's positions.
func withBuffer(declaration *ast.CallExpr) ([]byte, bool) {
	buffered := *declaration
	buffered.Args = append(slices.Clone(declaration.Args), &ast.BasicLit{Kind: token.INT, Value: "1"})
	var printed bytes.Buffer
	if formatError := format.Node(&printed, token.NewFileSet(), &buffered); formatError != nil {
		// The original reports nothing when its fix cannot be printed, and neither does the port.
		return nil, false
	}
	return printed.Bytes(), true
}

// callsNotify reports whether call calls notify, os/signal's Notify, through any parentheses: named by a selector,
// `signal.Notify(...)`, or bare where os/signal is dot-imported, `Notify(...)`, or by a variable declared as either,
// `listen := signal.Notify; listen(...)`.
func callsNotify(information *types.Info, declarations declaredValues, notify *types.Func, call *ast.CallExpr) bool {
	function := ast.Unparen(call.Fun)
	if names(information, function, notify) {
		return true
	}
	variable, isIdentifier := function.(*ast.Ident)
	return isIdentifier && names(information, ast.Unparen(declarations.valueOf(information, variable)), notify)
}

// names reports whether expression, an identifier or a selector, names object.
func names(information *types.Info, expression ast.Expr, object types.Object) bool {
	switch named := expression.(type) {
	case *ast.Ident:
		return information.Uses[named] == object
	case *ast.SelectorExpr:
		return information.Uses[named.Sel] == object
	default:
		return false
	}
}

// isBuiltinMake reports whether call calls the builtin make, through any parentheses, as the type checker resolves the
// name, so a function a package names make is not.
func isBuiltinMake(information *types.Info, call *ast.CallExpr) bool {
	function, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	return isIdentifier && information.Uses[function] == builtinMake
}

// importedPackage is the package at path that importer imports directly, or nil when it imports none there. x/tools'
// typesinternal.Imports, which reports only whether it does.
func importedPackage(importer *types.Package, path string) *types.Package {
	for _, imported := range importer.Imports() {
		if imported.Path() == path {
			return imported
		}
	}
	return nil
}
