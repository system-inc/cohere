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
// The registry runs the port in the original's place, and the original stays the oracle: the parity tests beside
// this file run both over the same packages.
package unreachable

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place. It runs despite errors as
// the original does, since a file that does not type-check still has a syntax tree to walk.
var Analyzer = &analysis.Analyzer{
	Name: "unreachable",
	Doc: "check for unreachable code\n\n" +
		"The unreachable analyzer finds statements that execution can never reach\n" +
		"because they are preceded by a return statement, a call to panic, an\n" +
		"infinite loop, or similar constructs.",
	URL:              "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unreachable",
	Requires:         []*analysis.Analyzer{inspect.Analyzer},
	RunDespiteErrors: true,
	Run:              run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("unreachable: inspect's result is missing")
	}
	panicDeclared := declaresPanicAtPackageLevel(pass.Files)
	// Each function declaration and literal in preorder, as the original's Preorder visits them, as a cursor, so its
	// fix can find a dead statement's place in the tree within the function alone.
	for function := range inspected.Root().Preorder((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		var body *ast.BlockStmt
		switch declared := function.Node().(type) {
		case *ast.FuncDecl:
			body = declared.Body
		case *ast.FuncLit:
			body = declared.Body
		}
		// A function declared without a body, implemented elsewhere, has nothing to reach.
		if body == nil {
			continue
		}
		state := &deadState{
			pass:          pass,
			function:      function,
			panicDeclared: panicDeclared,
			hasBreak:      map[ast.Stmt]bool{},
			hasGoto:       map[string]bool{},
			labels:        map[string]ast.Stmt{},
		}
		state.findLabels(body)
		state.reachable = true
		state.findDead(body)
	}
	return nil, nil
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
