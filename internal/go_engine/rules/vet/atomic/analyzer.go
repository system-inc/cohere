// Package atomic is vet's atomic pass, an assignment of what one of sync/atomic's Add functions returns to the very
// variable it added to, `x = atomic.AddUint64(&x, 1)`. The Add is atomic and the assignment after it is not, so a
// write racing between the two is lost. Whether the two sides name the same variable is judged by their text as gofmt
// prints it, so `x` and `&x` match, as do `*pointer` and `pointer`, and nothing finer is tried.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/atomic/atomic.go, whose logic it
// keeps and whose message it reports word for word, at the same position. The registry runs the port in the
// original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
package atomic

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "atomic",
	Doc: "check for common mistakes using the sync/atomic package\n\n" +
		"The atomic checker looks for assignment statements of the form:\n\n" +
		"\tx = atomic.AddUint64(&x, 1)\n\n" +
		"which are not atomic.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/atomic",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	// The judgment reads only syntax and the callee of a call, both of which a package with type errors still has,
	// so the original runs despite them, and the port with it.
	RunDespiteErrors: true,
	Run:              run,
}

// addFunctions are the Add functions of sync/atomic that return the sum they stored. The methods of atomic.Int64 and
// its kin are not among them: their receiver is the value added to, so there is no address to compare.
var addFunctions = []string{"AddInt32", "AddInt64", "AddUint32", "AddUint64", "AddUintptr"}

func run(pass *analysis.Pass) (any, error) {
	// A call can name sync/atomic's functions only in a package that imports it, so a package that does not is
	// skipped before the walk. The skip saves work and changes no finding.
	if !imports(pass.Pkg, "sync/atomic") {
		return nil, nil
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("atomic: inspect's result is missing")
	}
	// All visits each assignment in preorder, as the original's Preorder does, and hands it over typed.
	for assignment := range inspector.All[*ast.AssignStmt](inspected) {
		// `x, y = pair()` pairs no left side with a call of its own.
		if len(assignment.Lhs) != len(assignment.Rhs) {
			continue
		}
		// `x := atomic.AddUint64(&x, 1)` declares a new x, whose address the call cannot be taking. With more than
		// one left side, `:=` may assign an x already declared, so those are judged.
		if len(assignment.Lhs) == 1 && assignment.Tok == token.DEFINE {
			continue
		}
		for index, right := range assignment.Rhs {
			call, isCall := right.(*ast.CallExpr)
			if !isCall {
				continue
			}
			if isFunctionNamed(typeutil.Callee(pass.TypesInfo, call), "sync/atomic", addFunctions...) {
				checkAtomicAddAssignment(pass, assignment.Lhs[index], call)
			}
		}
	}
	return nil, nil
}

// checkAtomicAddAssignment reports left when call, one of the Add functions, adds to the variable left names: when
// call's address is `&left`, or when left is `*pointer` and call's address is pointer.
func checkAtomicAddAssignment(pass *analysis.Pass, left ast.Expr, call *ast.CallExpr) {
	// `atomic.AddUint64()` and its like do not type-check, and a package with type errors is still judged.
	if len(call.Args) != 2 {
		return
	}
	broken := false
	if addressOf, isUnary := call.Args[0].(*ast.UnaryExpr); isUnary && addressOf.Op == token.AND {
		broken = formatted(pass.Fset, left) == formatted(pass.Fset, addressOf.X)
	} else if dereference, isStar := left.(*ast.StarExpr); isStar {
		broken = formatted(pass.Fset, dereference.X) == formatted(pass.Fset, call.Args[0])
	}
	if broken {
		pass.ReportRangef(left, "direct assignment to atomic value")
	}
}

// imports reports whether importingPackage imports the package at path directly. x/tools' typesinternal.Imports.
func imports(importingPackage *types.Package, path string) bool {
	for _, imported := range importingPackage.Imports() {
		if imported.Path() == path {
			return true
		}
	}
	return false
}

// isFunctionNamed reports whether object is a function declared at the top level of the package at packagePath, not
// a method, under one of names. x/tools' typesinternal.IsFunctionNamed.
func isFunctionNamed(object types.Object, packagePath string, names ...string) bool {
	function, isFunction := object.(*types.Func)
	return isFunction &&
		isPackageLevel(object) &&
		function.Pkg().Path() == packagePath &&
		function.Signature().Recv() == nil &&
		slices.Contains(names, function.Name())
}

// isPackageLevel reports whether object is declared in its package's scope. x/tools' typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}

// formatted is node as gofmt prints it. x/tools' astutil.Format.
func formatted(fileSet *token.FileSet, node ast.Node) string {
	var builder strings.Builder
	// Ignored: a strings.Builder never fails a write, and a node the printer cannot print leaves what it printed so
	// far, which the original compares just the same, so the error is dropped as the original drops it.
	_ = printer.Fprint(&builder, fileSet, node)
	return builder.String()
}
