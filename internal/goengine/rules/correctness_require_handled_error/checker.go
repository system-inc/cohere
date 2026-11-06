package correctness_require_handled_error

import (
	"go/ast"
	"go/types"
	"go/version"

	"github.com/system-inc/cohere/internal/goengine/rules/marker"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// checker holds what every judgment in one package reads.
type checker struct {
	pass *analysis.Pass
	// errorInterface is the predeclared error's, which a result implements to count as an error.
	errorInterface *types.Interface
	// hashInterface is hash.Hash's, or nil when no package this one imports, directly or not, is hash. A type that
	// implements hash.Hash without the package ever seeing hash makes no promise about its Write, so it is judged.
	hashInterface *types.Interface
}

// returnsError is whether call has a result that is an error, in any position, and is not one of the exempt callees
// whose error need not be checked at the call.
func (checker checker) returnsError(call *ast.CallExpr) bool {
	if !checker.isFunctionCall(call) {
		return false
	}
	switch result := checker.pass.TypesInfo.TypeOf(call).(type) {
	case *types.Tuple:
		for index := range result.Len() {
			if checker.isError(result.At(index).Type()) {
				return !checker.isExempt(call)
			}
		}
		return false
	default:
		return checker.isError(result) && !checker.isExempt(call)
	}
}

// callsDroppingError is every call on the right of an assignment or var declaration whose error lands in _. A call
// that fills the whole left side, `value, _ := strconv.Atoi(text)`, is judged by the result each _ takes; with one
// value per name, each call stands alone.
func (checker checker) callsDroppingError(left []ast.Expr, right []ast.Expr) []*ast.CallExpr {
	calls := []*ast.CallExpr{}
	if len(right) == 1 && len(left) > 1 {
		call, isCall := ast.Unparen(right[0]).(*ast.CallExpr)
		if !isCall || !checker.isFunctionCall(call) {
			return calls
		}
		// A map index, a channel receive or a type assertion fills two names too, and is no call.
		results, isTuple := checker.pass.TypesInfo.TypeOf(call).(*types.Tuple)
		if !isTuple {
			return calls
		}
		for index, name := range left {
			if index < results.Len() && isBlank(name) && checker.isError(results.At(index).Type()) {
				if !checker.isExempt(call) {
					calls = append(calls, call)
				}
				return calls
			}
		}
		return calls
	}
	if len(left) != len(right) {
		return calls
	}
	for index, name := range left {
		call, isCall := ast.Unparen(right[index]).(*ast.CallExpr)
		if !isCall || !isBlank(name) || !checker.isFunctionCall(call) {
			continue
		}
		if checker.isError(checker.pass.TypesInfo.TypeOf(call)) && !checker.isExempt(call) {
			calls = append(calls, call)
		}
	}
	return calls
}

// isFunctionCall is whether call calls a function, rather than converting to a type or calling a builtin, neither
// of which returns an error of its own.
func (checker checker) isFunctionCall(call *ast.CallExpr) bool {
	callee, known := checker.pass.TypesInfo.Types[call.Fun]
	return known && !callee.IsType() && !callee.IsBuiltin()
}

// isError is whether a result of type candidate is an error: the predeclared error, or any type implementing it.
func (checker checker) isError(candidate types.Type) bool {
	return candidate != nil && types.Implements(candidate, checker.errorInterface)
}

// isExempt is whether call is one of the callees above whose error need not be checked at the call.
func (checker checker) isExempt(call *ast.CallExpr) bool {
	function, isFunc := typeutil.Callee(checker.pass.TypesInfo, call).(*types.Func)
	if !isFunc {
		return false
	}
	if checker.isExemptCallee(function) {
		return true
	}
	if receiver := function.Signature().Recv(); receiver != nil {
		// The method's own receiver, so a Builder embedded in another struct and written through it is the same
		// Builder.WriteString.
		if exemptWriteMethods[function.Name()] && isExemptWriterType(receiver.Type()) {
			return true
		}
		return function.Name() == "Write" && checker.isHashReceiver(call)
	}
	if function.Pkg() == nil || function.Pkg().Path() != "fmt" || !exemptPrintFunctions[function.Name()] {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	// The writer's static type: an io.Writer that happens to hold a Builder promises nothing at the call.
	writer := checker.pass.TypesInfo.TypeOf(call.Args[0])
	if isExemptWriterType(writer) {
		return true
	}
	return checker.hashInterface != nil && types.Implements(writer, checker.hashInterface)
}

// isHashReceiver is whether the Write call is on a value whose type implements hash.Hash. It reads the receiver
// expression's type rather than the method's, since a hash.Hash's Write is io.Writer's, declared on io.Writer.
func (checker checker) isHashReceiver(call *ast.CallExpr) bool {
	if checker.hashInterface == nil {
		return false
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	// A method value, hasher.Write(data), or a method expression, hash.Hash.Write(hasher, data); either way the
	// selection's receiver is the type written to.
	selection, isSelection := checker.pass.TypesInfo.Selections[selector]
	if !isSelection {
		return false
	}
	receiver := selection.Recv()
	if types.Implements(receiver, checker.hashInterface) {
		return true
	}
	// An addressable value whose pointer implements hash.Hash, a maphash.Hash variable, calls the pointer's Write.
	if _, isPointer := receiver.Underlying().(*types.Pointer); isPointer || types.IsInterface(receiver) {
		return false
	}
	return types.Implements(types.NewPointer(receiver), checker.hashInterface)
}

// isExemptCallee is whether function is one of exemptCallees: a package function when the entry's type name is
// empty, or else a method declared on that type, through a pointer or not, in a package at the entry's Go version
// or later.
func (checker checker) isExemptCallee(function *types.Func) bool {
	if function.Pkg() == nil {
		return false
	}
	typeName := ""
	if receiver := function.Signature().Recv(); receiver != nil {
		receiverType := types.Unalias(receiver.Type())
		if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
			receiverType = types.Unalias(pointer.Elem())
		}
		named, isNamed := receiverType.(*types.Named)
		if !isNamed {
			return false
		}
		typeName = named.Obj().Name()
	}
	for _, callee := range exemptCallees {
		if function.Pkg().Path() != callee.packagePath || function.Name() != callee.name {
			continue
		}
		if typeName == callee.typeName {
			return checker.isAtGoVersion(callee.sinceGoVersion)
		}
	}
	return false
}

// isAtGoVersion is whether the analyzed package's Go version is since or later; an empty since is every version. A
// package with no version, loaded outside a module, is read as the latest, as the type checker reads it.
func (checker checker) isAtGoVersion(since string) bool {
	packageVersion := checker.pass.Pkg.GoVersion()
	if since == "" || packageVersion == "" {
		return true
	}
	return version.Compare(packageVersion, since) >= 0
}

// calleeName is how the message names what call calls: a function or method by its package and receiver type,
// with this package's own names left bare, and anything else, a function value or literal, as written.
func (checker checker) calleeName(call *ast.CallExpr) string {
	function, isFunc := typeutil.Callee(checker.pass.TypesInfo, call).(*types.Func)
	if !isFunc {
		return types.ExprString(call.Fun)
	}
	qualifier := func(other *types.Package) string {
		if other == checker.pass.Pkg {
			return ""
		}
		return other.Name()
	}
	if receiver := function.Signature().Recv(); receiver != nil {
		return "(" + types.TypeString(receiver.Type(), qualifier) + ")." + function.Name()
	}
	if function.Pkg() == nil || function.Pkg() == checker.pass.Pkg {
		return function.Name()
	}
	return function.Pkg().Name() + "." + function.Name()
}

// report flags call unless its excuse gives a reason, with the message that names what is missing.
func (checker checker) report(call *ast.CallExpr, given marker.Excuse) {
	message := unhandledErrorMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = unhandledErrorWithoutReasonMessage
	case marker.None:
	}
	checker.pass.Report(analysis.Diagnostic{
		Pos:      call.Pos(),
		End:      call.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"callee": checker.calleeName(call)}),
	})
}
