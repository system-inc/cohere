package staticcheck_report

import (
	"bytes"
	"go/ast"
	"go/format"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Render is node as gofmt prints it, its line breaks read from the positions context.FileSet holds: staticcheck's
// report.Render, which a check uses to write code into a message or to tell two expressions apart by their text.
func Render(context *rule.Context, node ast.Node) string {
	printed := &bytes.Buffer{}
	if printError := format.Node(printed, context.FileSet, node); printError != nil {
		// Invariant: a check renders nodes of its own package's syntax, which the printer prints; one it cannot, a
		// node of a kind it does not know, is a port's bug, which the dispatcher records under the rule's name as
		// staticcheck's panic would be.
		panic(printError)
	}
	return printed.String()
}

// RenderArguments is each of arguments rendered, joined by ", ", as a call writes its arguments: staticcheck's
// report.RenderArgs.
func RenderArguments(context *rule.Context, arguments []ast.Expr) string {
	rendered := []string{}
	for _, argument := range arguments {
		rendered = append(rendered, Render(context, argument))
	}
	return strings.Join(rendered, ", ")
}
