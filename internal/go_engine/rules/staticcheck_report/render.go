package staticcheck_report

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Render is node as gofmt prints it, its line breaks read from the positions context.FileSet holds: staticcheck's
// report.Render, which a check uses to write code into a message or to tell two expressions apart by their text.
func Render(context *rule.Context, node ast.Node) string {
	printed := &bytes.Buffer{}
	if printError := format.Node(printed, renderingFileSet(context, node), node); printError != nil {
		// Invariant: a check renders nodes of its own package's syntax, which the printer prints; one it cannot, a
		// node of a kind it does not know, is a port's bug, which the dispatcher records under the rule's name as
		// staticcheck's panic would be.
		panic(printError)
	}
	return printed.String()
}

// renderingFileSet is the file set Render prints node through: a new one holding only node's file, when every node
// within node starts in that file, and context.FileSet otherwise. The printer asks a file set where each token it
// prints is, and context.FileSet is the load's one set, shared by every worker of the run, so each of those lookups
// read and wrote the set's one cache of the last file found, which bounced between cores; a set of its own answers
// from a cache that always holds the file. It holds the very token.File, at its own base, so it places every position
// of that file where context.FileSet does, and a node of a package's syntax has every position in its own file, so
// Render prints the same text either way.
func renderingFileSet(context *rule.Context, node ast.Node) *token.FileSet {
	tokenFile := context.FileSet.File(node.Pos())
	if tokenFile == nil {
		return context.FileSet
	}
	// A node a check built from the syntax of two files, which none does, would have positions the set of one file
	// cannot place, so such a node is printed through the shared set, as it always was.
	within := &withinFile{tokenFile: tokenFile}
	ast.Walk(within, node)
	if within.outside {
		return context.FileSet
	}
	fileSet := token.NewFileSet()
	fileSet.AddExistingFiles(tokenFile)
	return fileSet
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
