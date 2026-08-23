package rule

import (
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/parser"
	"github.com/microsoft/typescript-go/shim/tspath"
)

// TestFixHelpersDoNotEatLeadingTrivia is the guard for the sharpest edge in this API.
//
// `node.Loc.Pos()` is the position before leading trivia, so a fix built from Loc replaces the
// whitespace or comment preceding the node along with the node itself:
//
//	import * as X from 'fs'   ->   import * as X from'node:fs'
//
// What makes this worse than cosmetic is that the corrupted output still parses. The edit engine
// refuses a rewrite that breaks syntax, and that guard never fires here, so the damage lands with
// every downstream check green. A range wider than the rule intended is damage no later validation
// can detect.
//
// Found by @system_verify_lint_fix running real rules through the engine rather than reasoning
// about ranges.
func TestFixHelpersDoNotEatLeadingTrivia(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"space", "import * as X from 'fs';", "import * as X from 'node:fs';"},
		{"newline", "import * as X from\n\t'fs';", "import * as X from\n\t'node:fs';"},
		{"comment", "import * as X from /* pinned */ 'fs';", "import * as X from /* pinned */ 'node:fs';"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sourceFile, specifier := parseModuleSpecifier(t, testCase.source)
			context := Context{SourceFile: sourceFile}

			fix := context.ReplaceNode(specifier, "'node:fs'")
			applied := testCase.source[:fix.Range.Pos()] + fix.Text + testCase.source[fix.Range.End():]

			if applied != testCase.want {
				t.Fatalf("applying the fix produced\n  %q\nwant\n  %q", applied, testCase.want)
			}
		})
	}
}

// TestRemoveNodeLeavesTriviaBehind pins a deliberate choice rather than an accident.
//
// Removing a node and removing the blank line above it are different intentions, and a helper that
// guessed would be wrong half the time. A rule that wants the surrounding whitespace gone says so
// with ReplaceRange over a range it computed.
func TestRemoveNodeLeavesTriviaBehind(t *testing.T) {
	source := "import * as X from /* pinned */ 'fs';"
	sourceFile, specifier := parseModuleSpecifier(t, source)
	context := Context{SourceFile: sourceFile}

	fix := context.RemoveNode(specifier)
	applied := source[:fix.Range.Pos()] + source[fix.Range.End():]

	if applied != "import * as X from /* pinned */ ;" {
		t.Fatalf("RemoveNode swept up trivia: %q", applied)
	}
}

// TestInsertBeforeLandsOnTheTokenNotItsComment covers the third helper that reads Pos().
//
// Inserting before a node's trivia rather than before the node puts a modifier above the comment
// that documents the declaration, which is never what a rule means.
func TestInsertBeforeLandsOnTheTokenNotItsComment(t *testing.T) {
	source := "import * as X from /* pinned */ 'fs';"
	sourceFile, specifier := parseModuleSpecifier(t, source)
	context := Context{SourceFile: sourceFile}

	fix := context.InsertBefore(specifier, "!")
	applied := source[:fix.Range.Pos()] + fix.Text + source[fix.Range.End():]

	if applied != "import * as X from /* pinned */ !'fs';" {
		t.Fatalf("InsertBefore landed before the trivia: %q", applied)
	}
}

// TestInsertAfterNeedsNoTrimming confirms the one helper that was already correct, so a later
// refactor cannot quietly break it while fixing the others.
func TestInsertAfterNeedsNoTrimming(t *testing.T) {
	source := "import * as X from 'fs';"
	sourceFile, specifier := parseModuleSpecifier(t, source)
	context := Context{SourceFile: sourceFile}

	fix := context.InsertAfter(specifier, "!")
	applied := source[:fix.Range.Pos()] + fix.Text + source[fix.Range.End():]

	if applied != "import * as X from 'fs'!;" {
		t.Fatalf("InsertAfter moved: %q", applied)
	}
}

// TestTokenRangeSurvivesANilSourceFile keeps the helper from panicking in a harness that parsed no
// file. Untrimmed is wrong, but a crash inside a linter is worse.
func TestTokenRangeSurvivesANilSourceFile(t *testing.T) {
	_, specifier := parseModuleSpecifier(t, "import * as X from 'fs';")

	if got := TokenRange(nil, specifier); got != specifier.Loc {
		t.Fatalf("expected the untrimmed Loc as a fallback, got %+v", got)
	}
}

func parseModuleSpecifier(t *testing.T, source string) (*ast.SourceFile, *ast.Node) {
	t.Helper()

	fileName := tspath.NormalizePath("/Fixture.ts")
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName, Path: tspath.Path(fileName),
	}, source, core.ScriptKindTS)
	if sourceFile == nil {
		t.Fatalf("could not parse %q", source)
	}

	var specifier *ast.Node
	var walk func(*ast.Node)
	walk = func(node *ast.Node) {
		if node == nil || specifier != nil {
			return
		}
		if node.Kind == ast.KindImportDeclaration {
			specifier = node.AsImportDeclaration().ModuleSpecifier
			return
		}
		node.ForEachChild(func(child *ast.Node) bool { walk(child); return false })
	}
	walk(sourceFile.AsNode())

	if specifier == nil {
		t.Fatalf("no module specifier in %q", source)
	}
	return sourceFile, specifier
}
