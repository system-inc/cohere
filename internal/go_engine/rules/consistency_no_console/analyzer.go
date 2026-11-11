// Package consistency_no_console is the Go engine's rule that only a command writes to the process's own standard
// output and error.
//
// A command's output is its interface, so package main prints, and so does a test, whose output the person running
// it reads. A package anything else imports cannot know where its caller's standard output goes, or whether anyone
// reads it, so it returns its text or error to the caller, or writes to an io.Writer the caller hands in, and the
// command that owns the process decides what reaches it. It is TypeScript's consistency-no-console and Swift's
// consistency-no-print, read for Go.
package consistency_no_console

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-console"

// listener reads as the syntax dispatcher's walk enters each file whether the file is judged, and hears every call,
// selector and identifier in it. Every use of a function is found, not only a call: fmt.Println handed on as a
// callback, or kept in a variable and called later, writes to standard output just the same, and a check shaped like
// a call would report that route clean.
var listener = rule.Listener{
	FileLocal: true,
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil), (*ast.SelectorExpr)(nil), (*ast.Ident)(nil)},
	Node:      judgeNode,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "consistency_no_console", "Flags fmt.Print, Printf and Println, "+
	"fmt.Fprint, Fprintf and Fprintln handed os.Stdout or os.Stderr, the builtins print and println, and the "+
	"standard logger's log.Print, log.Fatal and log.Panic functions in a package other than main, outside a _test.go "+
	"file, since only a command knows where the process's standard output and error go. A write to any other writer, "+
	"fmt.Fprintln given one or a *log.Logger's method, is left alone.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads the types to tell fmt's Println from any other.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var consistencyNoConsoleMessage = policy.MessageOf(Name, "consistencyNoConsole")

// standardOutputFunctions are the package-level functions, by import path and name, that write to the process's own
// standard output or error without being handed a writer. The log functions write through the standard logger,
// whose output is standard error unless a command redirects it, and the Fatal and Panic ones then end the process
// or unwind it, which a package other than main has no business deciding either.
var standardOutputFunctions = map[string]map[string]bool{
	"fmt": {"Print": true, "Printf": true, "Println": true},
	"log": {
		"Print": true, "Printf": true, "Println": true,
		"Fatal": true, "Fatalf": true, "Fatalln": true,
		"Panic": true, "Panicf": true, "Panicln": true,
	},
}

// writerFunctions are fmt's writes to the writer they are handed first, judged only when that writer is the process's
// own standard output or error: then the writer is no choice the caller made, and the write is fmt.Println spelled
// longer.
var writerFunctions = map[string]bool{"Fprint": true, "Fprintf": true, "Fprintln": true}

// standardStreams are os's package-level variables for the process's own standard output and error.
var standardStreams = map[string]bool{"Stdout": true, "Stderr": true}

// fileState is whether the file being walked is judged, held in the rule's file state (rule.FileState).
type fileState struct {
	// judged is whether the file being walked is judged: one outside package main and outside a _test.go file.
	judged bool
}

// enterFile reads whether file is judged. package main is the command entry point, the one place whose standard
// output is the program's own, and a test's output is read by the person running it. The file's own name is read, not
// one a //line directive gives it, since only a real _test.go file is a test's.
func enterFile(context *rule.Context, file *ast.File) {
	rule.FileState[fileState](context).judged = context.Package.Name() != "main" &&
		!strings.HasSuffix(context.FileSet.PositionFor(file.Package, false).Filename, "_test.go")
}

// judgeNode reports a call, selector or identifier of a judged file that writes to the process's own standard output
// or error, its ancestors in stack.
func judgeNode(context *rule.Context, node ast.Node, stack []ast.Node) {
	if !rule.FileState[fileState](context).judged {
		return
	}
	switch expression := node.(type) {
	case *ast.CallExpr:
		// The writer is an argument, so only a call is judged here, and its arguments are heard on their own, since
		// one may itself be a print.
		if call, found := standardStreamWrite(context, expression); found {
			report(context, expression, call)
		}
	case *ast.SelectorExpr:
		if call, found := standardOutputCall(context, expression.Sel); found {
			report(context, expression, call)
		}
	case *ast.Ident:
		// A selector's own identifier is judged with its selector, as the same use, so it is not judged again. What a
		// reported selector selects from is a package's name, which is never a write.
		if len(stack) > 0 {
			selector, isSelector := stack[len(stack)-1].(*ast.SelectorExpr)
			if isSelector && selector.Sel == expression {
				return
			}
		}
		// A bare identifier reaches here as a dot-imported Println or a builtin println.
		if call, found := standardOutputCall(context, expression); found {
			report(context, expression, call)
		}
	}
}

// standardOutputCall is how the message names what identifier uses, `fmt.Println` or `println`, and whether it is one
// of the writes to standard output. It resolves identifier by its object, so a local function, closure or parameter
// named print, a method named Println on any type, a *log.Logger's included, and a package of another path that is
// also called fmt are none of them.
func standardOutputCall(context *rule.Context, identifier *ast.Ident) (string, bool) {
	switch object := context.TypesInfo.Uses[identifier].(type) {
	case *types.Builtin:
		if object.Name() == "print" || object.Name() == "println" {
			return object.Name(), true
		}
	case *types.Func:
		// A method has a receiver, and the writer behind it was chosen by whoever built the receiver.
		if object.Pkg() == nil || object.Signature().Recv() != nil {
			return "", false
		}
		if standardOutputFunctions[object.Pkg().Path()][object.Name()] {
			// The package's own name, not the alias it was imported under, so the message reads as the documentation
			// does.
			return object.Pkg().Name() + "." + object.Name(), true
		}
	}
	return "", false
}

// standardStreamWrite is how the message names call, `fmt.Fprintln(os.Stderr, ...)`, and whether it is one of fmt's
// writes to a writer handed os.Stdout or os.Stderr itself. The callee and the writer are both resolved by object, so
// a writer the caller passed in, a local value with a field named Stdout, and os.Stderr given as a value to print
// rather than as the writer are none of them.
func standardStreamWrite(context *rule.Context, call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	function, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != "fmt" || !writerFunctions[function.Name()] {
		return "", false
	}
	var writer *ast.Ident
	switch argument := ast.Unparen(call.Args[0]).(type) {
	case *ast.Ident:
		// A dot import of os leaves the variable's name bare.
		writer = argument
	case *ast.SelectorExpr:
		writer = argument.Sel
	default:
		return "", false
	}
	variable, isVariable := context.TypesInfo.Uses[writer].(*types.Var)
	if !isVariable || variable.Pkg() == nil || variable.Pkg().Path() != "os" || !standardStreams[variable.Name()] {
		return "", false
	}
	// Only the package's own variable is the process's stream, never a field or local that shares its name.
	if context.LookupIn(variable.Pkg(), variable.Name()) != variable {
		return "", false
	}
	return "fmt." + function.Name() + "(os." + variable.Name() + ", ...)", true
}

// report flags node, naming call as the message's call.
func report(context *rule.Context, node ast.Node, call string) {
	context.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: consistencyNoConsoleMessage.Id,
		Message:  consistencyNoConsoleMessage.Render(map[string]string{"call": call}),
	})
}
