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

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-console"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_console",
	Doc: "Flags fmt.Print, Printf and Println, fmt.Fprint, Fprintf and Fprintln handed os.Stdout or os.Stderr, the " +
		"builtins print and println, and the standard logger's log.Print, log.Fatal and log.Panic functions in a " +
		"package other than main, outside a _test.go file, since only a command knows where the process's " +
		"standard output and error go. A write to any other writer, fmt.Fprintln given one or a *log.Logger's " +
		"method, is left alone.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads the types to tell fmt's Println from any other.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

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

func run(pass *analysis.Pass) (any, error) {
	// package main is the command entry point, the one place whose standard output is the program's own.
	if pass.Pkg.Name() == "main" {
		return nil, nil
	}
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it, since only a real _test.go file is a test's.
		if strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		// Every use of the function is found, not only a call: fmt.Println handed on as a callback, or kept in a
		// variable and called later, writes to standard output just the same, and a check shaped like a call would
		// report that route clean.
		ast.Inspect(file, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.CallExpr:
				// The writer is an argument, so only a call is judged here, and its children are still visited, since
				// an argument may itself be a print.
				if call, found := standardStreamWrite(pass, expression); found {
					report(pass, expression, call)
				}
			case *ast.SelectorExpr:
				if call, found := standardOutputCall(pass, expression.Sel); found {
					report(pass, expression, call)
					// The selector's own identifier is the use just reported, so it is not visited again.
					return false
				}
			case *ast.Ident:
				// A bare identifier reaches here as a dot-imported Println or a builtin println.
				if call, found := standardOutputCall(pass, expression); found {
					report(pass, expression, call)
				}
			}
			return true
		})
	}
	return nil, nil
}

// standardOutputCall is how the message names what identifier uses, `fmt.Println` or `println`, and whether it is one
// of the writes to standard output. It resolves identifier by its object, so a local function, closure or parameter
// named print, a method named Println on any type, a *log.Logger's included, and a package of another path that is
// also called fmt are none of them.
func standardOutputCall(pass *analysis.Pass, identifier *ast.Ident) (string, bool) {
	switch object := pass.TypesInfo.Uses[identifier].(type) {
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
func standardStreamWrite(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	function, isFunction := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
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
	variable, isVariable := pass.TypesInfo.Uses[writer].(*types.Var)
	if !isVariable || variable.Pkg() == nil || variable.Pkg().Path() != "os" || !standardStreams[variable.Name()] {
		return "", false
	}
	// Only the package's own variable is the process's stream, never a field or local that shares its name.
	if variable.Pkg().Scope().Lookup(variable.Name()) != variable {
		return "", false
	}
	return "fmt." + function.Name() + "(os." + variable.Name() + ", ...)", true
}

// report flags node, naming call as the message's call.
func report(pass *analysis.Pass, node ast.Node, call string) {
	pass.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: consistencyNoConsoleMessage.Id,
		Message:  consistencyNoConsoleMessage.Render(map[string]string{"call": call}),
	})
}
