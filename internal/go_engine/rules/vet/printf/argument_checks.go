package printf

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates/verbs"
)

// recursiveStringer is the full name of the String or Error method whose body expression is in, when expression is
// that method's receiver or its address, so formatting it calls the method again, as t and &t do here:
//
//	func (t *T) String() string { printf("%s",  t) }
//	func (t  T) Error() string { printf("%s",  t) }
//	func (t  T) String() string { printf("%s", &t) }
func recursiveStringer(pass *analysis.Pass, expression ast.Expr) (string, bool) {
	expressionType := pass.TypesInfo.Types[expression].Type
	// A value with a Format method is unlikely to call its String method recursively. An expression the type
	// checker gave no type would stop the original here, and the port passes it by.
	if expressionType == nil || verbs.IsFormatter(expressionType) {
		return "", false
	}
	stringObject, _, _ := types.LookupFieldOrMethod(expressionType, false, pass.Pkg, "String")
	stringMethod, hasString := stringObject.(*types.Func)
	errorObject, _, _ := types.LookupFieldOrMethod(expressionType, false, pass.Pkg, "Error")
	errorMethod, hasError := errorObject.(*types.Func)
	var method *types.Func
	switch {
	case hasString && stringMethod.Pkg() == pass.Pkg && inScope(expression, stringMethod):
		method = stringMethod
	case hasError && errorMethod.Pkg() == pass.Pkg && inScope(expression, errorMethod):
		method = errorMethod
	default:
		return "", false
	}
	signature := method.Signature()
	if !isStringer(signature) {
		return "", false
	}
	receiver := expression
	if unary, isUnary := expression.(*ast.UnaryExpr); isUnary && unary.Op == token.AND {
		receiver = unary.X
	}
	identifier, isIdentifier := receiver.(*ast.Ident)
	// Uses names the declared method's receiver, and the method looked up on an instantiated receiver type may
	// have an instantiated signature with receivers of its own, so the origin is what compares.
	if isIdentifier && pass.TypesInfo.Uses[identifier] == signature.Recv().Origin() {
		return method.FullName(), true
	}
	return "", false
}

// inScope reports whether expression is in the scope of function.
func inScope(expression ast.Expr, function *types.Func) bool {
	return function.Scope() != nil && function.Scope().Contains(expression.Pos())
}

// isStringer reports whether a method's signature is String's in fmt.Stringer.
func isStringer(signature *types.Signature) bool {
	return signature.Params().Len() == 0 && signature.Results().Len() == 1 &&
		signature.Results().At(0).Type() == types.Typ[types.String]
}

// isFunctionValue reports whether expression is a function, not a function's call, which is almost always a mistake
// to print.
func isFunctionValue(pass *analysis.Pass, expression ast.Expr) bool {
	expressionType := pass.TypesInfo.Types[expression].Type
	if expressionType == nil {
		return false
	}
	// The type itself, not its underlying type: a named function type with a String method prints well.
	_, isSignature := expressionType.(*types.Signature)
	return isSignature
}

// formatNode is node as go/printer prints it: x/tools' astutil.Format.
func formatNode(pass *analysis.Pass, node ast.Node) string {
	var printed strings.Builder
	// Ignored: a strings.Builder never fails a write, and a node the printer cannot print prints as far as it can,
	// which is what x/tools' Format gives too.
	_ = printer.Fprint(&printed, pass.Fset, node)
	return printed.String()
}
