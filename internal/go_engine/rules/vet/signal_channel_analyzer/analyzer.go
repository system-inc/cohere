// Package signal_channel_analyzer is vet's sigchanyzer pass, an unbuffered channel handed to signal.Notify. Notify
// never blocks sending a signal, so a signal that arrives while no receiver is ready at an unbuffered channel is
// dropped. The pass judges the channel by how it was made: a call to signal.Notify, named directly or through a
// variable declared as it, whose channel is a variable declared as a call with one argument, or is itself such a call
// other than the builtin make, is reported, with a fix that passes that call a second argument, 1.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/sigchanyzer/sigchanyzer.go, whose
// logic it keeps and whose message and fix it reports word for word and edit for edit, at the same position. The
// registry runs the port in the original's place, and the original stays the oracle: the parity tests beside this
// file run both over the same packages.
//
// It differs in one way, where it is right. The original finds a variable's declaration through the parser's
// resolution of identifiers (ast.Ident.Obj, deprecated since Go 1.22), which resolves a name within its own file
// alone, so a channel, or a variable holding signal.Notify, declared at package level in another file of the
// package goes unseen. The port finds the declaration through the type checker's definitions, across the package's
// files, so it reports those calls too, with the fix in the declaring file. Within one file the two resolutions name
// the same declaration, and the differences fixture holds the cross-file shapes.
package signal_channel_analyzer

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "sigchanyzer",
	Doc: "check for unbuffered channel of os.Signal\n\n" +
		"This checker reports call expression of the form\n\n" +
		"\tsignal.Notify(c <-chan os.Signal, sig ...os.Signal),\n\n" +
		"where c is an unbuffered channel, which can be at risk of missing the signal.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/sigchanyzer",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// signalPackagePath is the import path of the package whose Notify the pass judges.
const signalPackagePath = "os/signal"

func run(pass *analysis.Pass) (any, error) {
	// A package that does not import os/signal itself cannot name its Notify, so it is passed over unread. Without
	// this the walk would find nothing there either, so it saves the walk and changes no finding.
	if !imports(pass.Pkg, signalPackagePath) {
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("signal_channel_analyzer: inspect's result is missing")
	}
	declarations := declaredValuesOf(inspected, pass.TypesInfo)
	// All visits each call in preorder, as the original's Preorder does, and hands it over typed.
	for call := range inspector.All[*ast.CallExpr](inspected) {
		if !isSignalNotify(pass.TypesInfo, declarations, call) {
			continue
		}
		// A call to Notify that type-checks passes its channel, and the pass runs only over packages that type-check,
		// so this guard keeps an unchecked call from panicking and is never taken.
		if len(call.Args) == 0 {
			continue
		}
		// channelDeclaration is the call that made the channel: the one that declared the variable passed, or the call
		// passed itself.
		var channelDeclaration *ast.CallExpr
		switch channel := call.Args[0].(type) {
		case *ast.Ident:
			if declared, isCall := declarations.valueOf(pass.TypesInfo, channel).(*ast.CallExpr); isCall {
				channelDeclaration = declared
			}
		case *ast.CallExpr:
			// Only signal.Notify(make(chan os.Signal), os.Interrupt) is safe, since no one else holds the channel to
			// receive from it. Any other call is conservatively treated as unsafe, as the original does, see
			// golang/go#45043.
			if isBuiltinMake(pass.TypesInfo, channel) {
				continue
			}
			channelDeclaration = channel
		}
		// A call with no argument, or with a second already, a size, is left alone: there is nothing to append a size
		// to, or the channel has one.
		if channelDeclaration == nil || len(channelDeclaration.Args) != 1 {
			continue
		}
		replacement, formatted := withBuffer(channelDeclaration)
		if !formatted {
			continue
		}
		pass.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.End(),
			Message: "misuse of unbuffered os.Signal channel as argument to signal.Notify",
			SuggestedFixes: []analysis.SuggestedFix{{
				Message: "Change to buffer channel",
				TextEdits: []analysis.TextEdit{{
					Pos:     channelDeclaration.Pos(),
					End:     channelDeclaration.End(),
					NewText: replacement,
				}},
			}},
		})
	}
	return nil, nil
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

// isSignalNotify reports whether call calls os/signal's Notify, by a selector naming it, `signal.Notify(...)`, or by
// a variable declared as such a selector, `notify := signal.Notify; notify(...)`.
func isSignalNotify(information *types.Info, declarations declaredValues, call *ast.CallExpr) bool {
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		return isNotify(information, function.Sel)
	case *ast.Ident:
		if declared, isSelector := declarations.valueOf(information, function).(*ast.SelectorExpr); isSelector {
			return isNotify(information, declared.Sel)
		}
		return false
	default:
		return false
	}
}

// isNotify reports whether name names an object called Notify in os/signal.
func isNotify(information *types.Info, name *ast.Ident) bool {
	object := information.ObjectOf(name)
	// The type checker records the object of every selector's name in a package that type-checks, and only the
	// universe's objects have no package, none of them called Notify, so neither guard is taken. The original would
	// panic where they are.
	if object == nil || object.Pkg() == nil {
		return false
	}
	return object.Name() == "Notify" && object.Pkg().Path() == signalPackagePath
}

// isBuiltinMake reports whether call calls the builtin make, named by an identifier, so a parenthesized `(make)(...)`
// is not, as in the original.
func isBuiltinMake(information *types.Info, call *ast.CallExpr) bool {
	if !information.Types[call.Fun].IsBuiltin() {
		return false
	}
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return information.ObjectOf(function).Name() == "make"
	default:
		return false
	}
}

// imports reports whether importer imports the package at path directly. x/tools' typesinternal.Imports.
func imports(importer *types.Package, path string) bool {
	return slices.ContainsFunc(importer.Imports(), func(imported *types.Package) bool {
		return imported.Path() == path
	})
}
