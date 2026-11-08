// Package unreachable is vet's unreachable pass, a statement that execution can never reach because the one before
// it ends the flow of control: a return, a call to the builtin panic, a goto, break, continue or fallthrough, a loop
// with no condition and no break, a select, or an if, switch or type switch whose every way through ends so. A
// labeled statement some goto names is taken as reachable, and an empty statement is never reported. Each finding
// offers to delete the dead statement.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/unreachable/unreachable.go (here
// and in dead_state.go) and the deletion its fix makes, internal/refactor/delete.go's DeleteStmt
// (delete_statement.go), whose logic it keeps, the walk over labels, gotos, breaks, selects and switches line for
// line, and whose messages and edits it reports word for word and byte for byte, at the same position. Like the
// original it reads syntax alone, never type information, so the registry may run it without types.
//
// It differs in one judgment. The original takes a call of `panic` for the builtin's when the parser resolved the
// name to no declaration, but the parser resolves names within one file, so a package that declares its own `panic`
// at package level in another file has every call of that function read as the builtin's, and the statements after
// it reported as dead. The port also reads every file's package-level declarations, still syntax alone, and takes
// none of those calls for the builtin's (testdata/src/differences).
//
// It sits on the engine's syntax dispatcher, hearing each function declaration and literal on the walk the dispatcher
// makes once for every syntax rule, with the cursor of each, the one the original's walk handed it.
//
// The registry runs the port in the original's place, and the original stays the oracle: the parity tests beside
// this file run both over the same packages.
package unreachable

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "unreachable"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for unreachable code\n\n" +
	"The unreachable analyzer finds statements that execution can never reach\n" +
	"because they are preceded by a return statement, a call to panic, an\n" +
	"infinite loop, or similar constructs."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unreachable"

// Listener is what the port hears of each package from the dispatcher: each function declaration and literal in
// preorder, as the original's Preorder visits them, with its cursor, so its fix can find a dead statement's place in
// the tree within the function alone. Whether the package declares its own `panic`, which the original's loop asked
// once before walking, is asked on the first function and kept in the context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)},
	Node:  checkFunction,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset. It runs despite errors as the original does, since a file that does not type-check still
// has a syntax tree to walk.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	analyzer.RunDespiteErrors = true
	return analyzer
}

// packageState is whether the package declares its own `panic` at package level, asked once, on its first function.
type packageState struct {
	// decided is whether the package has been asked about yet.
	decided bool
	// panicDeclared is whether any of the package's files declares `panic` at package level.
	panicDeclared bool
}

// checkFunction walks the body of the function declaration or literal node, reporting each statement in it that
// execution can never reach.
func checkFunction(context *rule.Context, node ast.Node, _ []ast.Node) {
	var body *ast.BlockStmt
	switch declared := node.(type) {
	case *ast.FuncDecl:
		body = declared.Body
	case *ast.FuncLit:
		body = declared.Body
	default:
		return
	}
	// A function declared without a body, implemented elsewhere, has nothing to reach.
	if body == nil {
		return
	}
	declared := rule.State[packageState](context)
	if !declared.decided {
		declared.decided, declared.panicDeclared = true, declaresPanicAtPackageLevel(context.Files)
	}
	state := &deadState{
		context:       context,
		function:      context.Cursor(),
		panicDeclared: declared.panicDeclared,
		hasBreak:      map[ast.Stmt]bool{},
		hasGoto:       map[string]bool{},
		labels:        map[string]ast.Stmt{},
	}
	state.findLabels(body)
	state.reachable = true
	state.findDead(body)
}

// declaresPanicAtPackageLevel is whether any of files declares `panic` at package level, as a function, variable,
// constant or type, which hides the builtin in every file of the package. A method named panic hides nothing.
func declaresPanicAtPackageLevel(files []*ast.File) bool {
	for _, file := range files {
		for _, declaration := range file.Decls {
			switch declared := declaration.(type) {
			case *ast.FuncDecl:
				if declared.Recv == nil && declared.Name.Name == "panic" {
					return true
				}
			case *ast.GenDecl:
				if specifiesPanic(declared.Specs) {
					return true
				}
			}
		}
	}
	return false
}

// specifiesPanic is whether any of specifications, a declaration's, names `panic`. An import's name is the file's
// alone and cannot be called, so only variables, constants and types are read.
func specifiesPanic(specifications []ast.Spec) bool {
	for _, specification := range specifications {
		switch specified := specification.(type) {
		case *ast.ValueSpec:
			for _, name := range specified.Names {
				if name.Name == "panic" {
					return true
				}
			}
		case *ast.TypeSpec:
			if specified.Name.Name == "panic" {
				return true
			}
		}
	}
	return false
}
