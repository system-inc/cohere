// Package ruletest runs a rule against source text and reports what it found.
//
// Every rule in verify ships a fixture pair: source that must produce a finding, and source that
// must produce none. Both directions, always, in the same commit as the rule.
//
// That is not ceremony. During the migration this tool replaces, a class-name filter that measured
// fifteen times faster silently stopped reporting one real collapse, and a hand-written identifier
// splitter silently stopped reporting another. Both were caught by fixtures. Neither was caught by
// reading the diff. The inverse defect, a rule that fires on correct code, had no guard at all,
// because the corpus was violations only.
//
// A rule that has never been shown to fire has not been shown to work.
package ruletest

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/parser"
	"github.com/microsoft/typescript-go/shim/tspath"
	"github.com/system-inc/verify/internal/rule"
)

// Result is what one rule saw in one file.
type Result struct {
	Diagnostics []rule.Diagnostic
	SourceFile  *ast.SourceFile
}

// MessageIds returns the ids of every finding, in the order they were reported.
func (r Result) MessageIds() []string {
	ids := make([]string, 0, len(r.Diagnostics))
	for _, diagnostic := range r.Diagnostics {
		ids = append(ids, diagnostic.Message.Id)
	}
	return ids
}

// Run parses source text and walks it with one rule, with no type information.
//
// Rules that need a checker are exercised against a real program elsewhere; this path exists for
// the syntax-only majority, where a full program would cost seconds to prove something a parse can
// prove in microseconds.
func Run(t *testing.T, subject rule.Rule, fileName string, sourceText string) Result {
	t.Helper()

	scriptKind := core.ScriptKindTS
	if strings.HasSuffix(fileName, ".tsx") {
		scriptKind = core.ScriptKindTSX
	}

	// typescript-go panics on a relative filename: the parser stores it as an identity and expects
	// it to be normalized and absolute. A fixture names a file for readability rather than for the
	// disk, so it is rooted here instead of at every call site.
	if !tspath.IsRootedDiskPath(fileName) {
		fileName = "/" + strings.TrimPrefix(fileName, "/")
	}
	fileName = tspath.NormalizePath(fileName)

	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName,
		Path:     tspath.Path(fileName),
	}, sourceText, scriptKind)
	if sourceFile == nil {
		t.Fatalf("could not parse %s", fileName)
	}

	var diagnostics []rule.Diagnostic
	context := rule.Context{
		SourceFile: sourceFile,
		Report: func(diagnostic rule.Diagnostic) {
			diagnostic.RuleName = subject.Name
			diagnostics = append(diagnostics, diagnostic)
		},
	}

	listeners := subject.Run(context, nil)
	if listeners != nil {
		walk(sourceFile.AsNode(), listeners)
	}

	return Result{Diagnostics: diagnostics, SourceFile: sourceFile}
}

// walk visits every node, calling any listener registered for its kind.
//
// One traversal serves every rule in a real run. This is the single-rule form of that walk, kept
// deliberately simple so a fixture failure points at the rule rather than at the harness.
func walk(node *ast.Node, listeners rule.Listeners) {
	if node == nil {
		return
	}
	if listener, isListening := listeners[node.Kind]; isListening {
		listener(node)
	}
	node.ForEachChild(func(child *ast.Node) bool {
		walk(child, listeners)
		return false
	})
}

// ExpectFindings asserts that a rule reported exactly these message ids, in order.
//
// Order matters because a rule reporting the right findings in the wrong place is still wrong, and
// because two defects on one line must both be reported rather than one hiding the other.
func ExpectFindings(t *testing.T, result Result, wantIds ...string) {
	t.Helper()

	gotIds := result.MessageIds()
	if len(gotIds) != len(wantIds) {
		t.Fatalf("expected %d findings %v, got %d %v", len(wantIds), wantIds, len(gotIds), gotIds)
	}
	for index, want := range wantIds {
		if gotIds[index] != want {
			t.Fatalf("finding %d: expected %q, got %q (all: %v)", index, want, gotIds[index], gotIds)
		}
	}
}

// ExpectClean asserts that a rule stayed silent.
//
// This is the half the old corpus never had, and the half that catches a rule which fires on
// correct code. A rule with no clean fixture has been shown to detect, never to discriminate.
func ExpectClean(t *testing.T, result Result) {
	t.Helper()

	if len(result.Diagnostics) != 0 {
		t.Fatalf("expected no findings, got %d: %v", len(result.Diagnostics), result.MessageIds())
	}
}
