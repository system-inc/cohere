package nexus

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/core"
	"github.com/microsoft/typescript-go/shim/parser"
	"github.com/microsoft/typescript-go/shim/tspath"
)

// parseForComments parses source text the way the rule harness does, so the comment scan is proven
// against the same tree the rules will walk.
func parseForComments(t *testing.T, sourceText string) *ast.SourceFile {
	t.Helper()

	fileName := tspath.NormalizePath("/repository/source/Thing.ts")
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: fileName,
		Path:     tspath.Path(fileName),
	}, sourceText, core.ScriptKindTS)
	if sourceFile == nil {
		t.Fatal("could not parse")
	}
	return sourceFile
}

// TestAllCommentsSeesEveryPosition is the coverage proof for the scan.
//
// A comment scan that misses a position fails silently and in the safest-looking way: the rule
// reports nothing, the fixtures pass, and the gate is green. Nothing reports a comment it did not
// see. So the scan is proven against a file holding a comment in every position a comment can
// occupy, and the trailing case is the one that motivated this test, because it sits after the
// last node in the tree.
func TestAllCommentsSeesEveryPosition(t *testing.T) {
	sourceText := strings.Join([]string{
		"// leading file comment",
		"/* leading block */",
		"/** leading jsdoc */",
		"import * as NodePath from 'node:path';",
		"",
		"// before a statement",
		"export function thing() {",
		"    // inside a body",
		"    const value = 1; // trailing on a line",
		"    /* inside block */",
		"    return value;",
		"}",
		"",
		"// after the last statement",
		"/* final block */",
		"",
	}, "\n")

	comments := allComments(parseForComments(t, sourceText))

	wantTexts := []string{
		"// leading file comment",
		"/* leading block */",
		"/** leading jsdoc */",
		"// before a statement",
		"// inside a body",
		"// trailing on a line",
		"/* inside block */",
		"// after the last statement",
		"/* final block */",
	}

	gotTexts := make([]string, 0, len(comments))
	for _, comment := range comments {
		gotTexts = append(gotTexts, comment.Text)
	}

	if len(gotTexts) != len(wantTexts) {
		t.Fatalf("expected %d comments %v, got %d %v", len(wantTexts), wantTexts, len(gotTexts), gotTexts)
	}
	for index, want := range wantTexts {
		if gotTexts[index] != want {
			t.Fatalf("comment %d: expected %q, got %q (all: %v)", index, want, gotTexts[index], gotTexts)
		}
	}
}

// TestAllCommentsDeduplicates guards the scan's one real hazard: it starts from every node
// position, and a node and its first child usually begin at the same place, so the same comment is
// offered repeatedly. A rule fed duplicates reports one defect several times.
func TestAllCommentsDeduplicates(t *testing.T) {
	sourceText := "// once\nexport const value = { nested: { deeper: 1 } };\n"

	comments := allComments(parseForComments(t, sourceText))
	if len(comments) != 1 {
		texts := make([]string, 0, len(comments))
		for _, comment := range comments {
			texts = append(texts, comment.Text)
		}
		t.Fatalf("expected 1 comment, got %d: %v", len(comments), texts)
	}
}

func TestCommentClassification(t *testing.T) {
	sourceText := "// line\n/* block */\n/** jsdoc */\nexport const value = 1;\n"

	comments := allComments(parseForComments(t, sourceText))
	if len(comments) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(comments))
	}

	if comments[0].IsBlock || comments[0].isJsDoc() {
		t.Fatalf("a double-slash comment is neither a block nor JSDoc: %+v", comments[0])
	}
	if !comments[1].IsBlock || comments[1].isJsDoc() {
		t.Fatalf("a slash-star comment is a block but not JSDoc: %+v", comments[1])
	}
	if !comments[2].IsBlock || !comments[2].isJsDoc() {
		t.Fatalf("a slash-star-star comment is both: %+v", comments[2])
	}
}

func TestCommentContentLines(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantLines  []string
	}{
		{"one line jsdoc", "/** Does the thing. */\nexport const value = 1;\n", []string{"Does the thing."}},
		{
			"multi line jsdoc, asterisks are furniture",
			"/**\n * Does the thing.\n */\nexport const value = 1;\n",
			[]string{"Does the thing."},
		},
		{
			"jsdoc with a tag",
			"/**\n * Does the thing.\n * @returns nothing\n */\nexport const value = 1;\n",
			[]string{"Does the thing.", "@returns nothing"},
		},
		{"empty jsdoc", "/** */\nexport const value = 1;\n", nil},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			comments := allComments(parseForComments(t, testCase.sourceText))
			if len(comments) == 0 {
				t.Fatal("expected a comment")
			}
			gotLines := comments[0].contentLines()
			if len(gotLines) != len(testCase.wantLines) {
				t.Fatalf("expected %v, got %v", testCase.wantLines, gotLines)
			}
			for index, want := range testCase.wantLines {
				if gotLines[index] != want {
					t.Fatalf("line %d: expected %q, got %q", index, want, gotLines[index])
				}
			}
		})
	}
}
