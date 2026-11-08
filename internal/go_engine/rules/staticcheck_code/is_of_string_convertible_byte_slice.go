package staticcheck_code

import (
	"go/ast"
	"go/types"
	"go/version"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// IsOfStringConvertibleByteSlice is whether expression is a slice of bytes that converts to a string directly, a
// []byte or a type defined on one. It is staticcheck's code.IsOfStringConvertibleByteSlice, from
// honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// From Go 1.18 a slice of a type defined on byte converts too, so in a file of that language version or newer the
// element is judged by its underlying type. A file whose version is unknown, as in a package loaded outside a module
// with no `//go:build` line naming one, is judged as older. It panics, as staticcheck's does, for an expression the
// type checker recorded no type of.
func IsOfStringConvertibleByteSlice(context *rule.Context, expression ast.Expr) bool {
	slice, isSlice := context.TypesInfo.TypeOf(expression).Underlying().(*types.Slice)
	if !isSlice {
		return false
	}
	element := types.Unalias(slice.Elem())
	// Before Go 1.18 a []T, where T is defined as byte, did not convert to a string; see golang.org/issue/23536.
	if version.Compare(staticcheck_report.LanguageVersion(context, expression), "go1.18") >= 0 {
		element = element.Underlying()
	}
	return types.Identical(element, types.Typ[types.Byte])
}
