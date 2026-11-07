package go_origin_names

import (
	"go/ast"
	"go/types"
)

// contextTable names the context each of a short list of calls returns; a context from any other call is left for
// the waves.
var contextTable = []tableEntry{
	{packagePath: "context", name: "WithCancel", newName: "cancelContext"},
	{packagePath: "context", name: "WithCancelCause", newName: "cancelContext"},
	{packagePath: "context", name: "WithTimeout", newName: "timeoutContext"},
	{packagePath: "context", name: "WithDeadline", newName: "timeoutContext"},
	{packagePath: "context", name: "Background", newName: "backgroundContext"},
	{packagePath: "context", name: "TODO", newName: "backgroundContext"},
	{packagePath: "os/signal", name: "NotifyContext", newName: "signalContext"},
	{packagePath: "testing", receiver: "T", name: "Context", newName: "testContext"},
	{packagePath: "net/http", receiver: "Request", name: "Context", newName: "requestContext"},
	// The format shim puts a formatter's settings on a context, which is how the formatter reads them.
	{
		packagePath: "github.com/microsoft/TypeScript/tsc/shim/format",
		name:        "WithFormatCodeSettings",
		newName:     "settingsContext",
	},
}

// CallerContextName is every context.Context parameter's name. A parameter's context is the caller's: the function
// did not make it and cannot say what made it, and every caller in the tree hands down the one it was given, so the
// name says where it came from and nothing it cannot know.
const CallerContextName = "callerContext"

// IsContext reports whether a type is context.Context, or a pointer to it, by the named type and its package, never
// by a name's spelling.
func IsContext(resolvedType types.Type) bool {
	return NamedTypeOf(resolvedType) == TypeName{PackagePath: "context", Name: "Context"}
}

// ContextCallName names the context call returns, by the table. It returns the call's name for a report, and a
// category when the call names nothing: one with no declared function behind it, or one outside the table.
func ContextCallName(info *types.Info, call *ast.CallExpr) (string, string, string) {
	function, receivers, category := ProducingFunction(info, call)
	if function == nil {
		return "", category, ""
	}
	callName := QualifiedName(function)
	for _, entry := range contextTable {
		if entry.matches(function, receivers) {
			return entry.newName, "", callName
		}
	}
	return "", "call not in the table", callName
}
