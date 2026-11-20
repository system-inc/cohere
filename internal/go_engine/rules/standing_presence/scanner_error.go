package standing_presence

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// scannerErrorNamed names, with scannererr's reason, each object the package's bodies use that the build finds as a
// package-level variable of the pass's own package that a scanner is assigned to: the rule judges such a variable by
// every use of it in the package, so each body that names it must be present.
//
// The predicate mirrors rules/vet/scanner_error/analyzer.go, which exposes none of it: a call that is itself a value
// of a `var` specification or the right of an assignment (assignedIdentifier, analyzer.go:191-215), to bufio's
// package-level NewScanner from outside bufio (isNewScanner, analyzer.go:179-185), over a reader that is no pointer to
// a bytes.Buffer, a bytes.Reader or a strings.Reader (analyzer.go:127-133, isPointerToNamed and isTypeNamed,
// analyzer.go:283-300), and the variable the name or left side at the call's index names. As the census counter reads
// it, a variable only ever assigned with `=` is a candidate too, though the rule can never report one, since the
// assignment's own left side is a use that is no method's receiver: wider is the safe side.
//
// The walk is what names every candidate the rule can report: a package-level variable assigned with `=` in a body is
// never reportable, since that left side is a use (analyzer.go:217-242) and a use that is no method's receiver ends
// the judgment (analyzer.go:248-253), so a reportable one is made only in a package-level `var` specification, which
// the build always holds, however its bodies are stripped. Every package-level variable whose type is *bufio.Scanner
// is named besides, whatever made it: cheap safety, wider than the rule, which costs the bodies that name such a
// variable and loses nothing.
func scannerErrorNamed(
	pass *analysis.Pass,
	used []fact_reach.UsedObject,
	find func(identity build_reads.Identity) (types.Object, bool),
) []fact_reach.Named {
	// candidates are read on the first package-level variable found, since most packages' bodies use none.
	var candidates map[*types.Var]bool
	var named []fact_reach.Named
	for _, object := range used {
		variable, isVariable := packageVariable(pass, find, object.Identity)
		if !isVariable {
			continue
		}
		if candidates == nil {
			candidates = scannerVariables(pass)
		}
		if isPointerToNamed(variable.Type(), "bufio", "Scanner") || candidates[variable] {
			named = append(named, fact_reach.Named{Object: object.Object, Reason: ScannerError.Reason()})
		}
	}
	return named
}

// scannerVariables are the variables of the pass's files a scanner is assigned to, at any depth: by a `var`
// specification's name, or an assignment's left side, at the index of a value that makes a scanner.
func scannerVariables(pass *analysis.Pass) map[*types.Var]bool {
	variables := map[*types.Var]bool{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch heard := node.(type) {
			case *ast.ValueSpec:
				for index, value := range heard.Values {
					if index < len(heard.Names) && makesScanner(pass, value) {
						if variable, isVariable := pass.TypesInfo.Defs[heard.Names[index]].(*types.Var); isVariable {
							variables[variable] = true
						}
					}
				}
			case *ast.AssignStmt:
				for index, value := range heard.Rhs {
					if index >= len(heard.Lhs) || !makesScanner(pass, value) {
						continue
					}
					if identifier, isIdentifier := heard.Lhs[index].(*ast.Ident); isIdentifier {
						if variable, isVariable := pass.TypesInfo.ObjectOf(identifier).(*types.Var); isVariable {
							variables[variable] = true
						}
					}
				}
			}
			return true
		})
	}
	return variables
}

// makesScanner is whether value is itself a call the rule gathers: bufio.NewScanner, never in parentheses, over a
// reader that can fail to read.
func makesScanner(pass *analysis.Pass, value ast.Expr) bool {
	call, isCall := value.(*ast.CallExpr)
	if !isCall || len(call.Args) == 0 {
		return false
	}
	callee := typeutil.Callee(pass.TypesInfo, call)
	if callee == nil || !isPackageLevel(callee) || callee.Pkg() == pass.Pkg || callee.Pkg().Path() != "bufio" ||
		callee.Name() != "NewScanner" {
		return false
	}
	reader := pass.TypesInfo.TypeOf(call.Args[0])
	return !isPointerToNamed(reader, "bytes", "Buffer", "Reader") && !isPointerToNamed(reader, "strings", "Reader")
}

// isPointerToNamed is whether subject is, or is an alias for, a pointer to a package-level defined type of the
// package at packagePath named one of names (rules/vet/scanner_error/analyzer.go:283-300).
func isPointerToNamed(subject types.Type, packagePath string, names ...string) bool {
	if subject == nil {
		return false
	}
	pointer, isPointer := types.Unalias(subject).(*types.Pointer)
	if !isPointer {
		return false
	}
	named, isNamed := types.Unalias(pointer.Elem()).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil && isPackageLevel(typeName) && typeName.Pkg().Path() == packagePath &&
		slices.Contains(names, typeName.Name())
}

// isPackageLevel is whether object is declared at its package's scope (rules/vet/scanner_error/analyzer.go:303-305).
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
