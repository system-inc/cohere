package comments

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
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

	comments := All(parseForComments(t, sourceText))

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

// TestAllCommentsSeesInsideEmptyDelimiters pins the fix for the gap that cost `no-fallthrough` a
// rule-local scan.
//
// Every case here was measured returning zero comments before `collectListInteriors` existed. They
// are kept as a table rather than folded into the coverage test above because they share a cause
// rather than a shape: what makes them invisible is an empty `NodeList`, and the shapes that own one
// range from a block to a call's argument list. A regression would take each of these silent
// individually, so each is asserted individually.
func TestAllCommentsSeesInsideEmptyDelimiters(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantTexts  []string
	}{
		// The case that started this: one of upstream's clean inputs for `no-fallthrough`.
		{
			"a block that is a switch clause's only statement",
			"switch(foo) { case 0: { /* falls through */ } case 1: b(); }",
			[]string{"/* falls through */"},
		},
		{"an empty function body", "function f() { /* only content */ }", []string{"/* only content */"}},
		{"an empty arrow body", "const f = () => { /* arrow */ };", []string{"/* arrow */"}},
		{"an empty bare block", "{ /* bare block */ }", []string{"/* bare block */"}},
		{"an empty if branch", "if (a) { /* empty then */ }", []string{"/* empty then */"}},
		{"an empty loop body", "for (;;) { /* loop body */ }", []string{"/* loop body */"}},
		{"an empty method body", "class A { m() { /* method */ } }", []string{"/* method */"}},
		{
			"every arm of an empty try",
			"try { /* t */ } catch (e) { /* c */ } finally { /* f */ }",
			[]string{"/* t */", "/* c */", "/* f */"},
		},
		{"an empty switch body", "switch (a) { /* empty switch body */ }", []string{"/* empty switch body */"}},
		{"a line comment as a body's only content", "function f() {\n// only\n}\n", []string{"// only"}},

		// None of these involve a block, which is why the defect was reported narrower than it was.
		{"an empty class body", "class A { /* empty class */ }", []string{"/* empty class */"}},
		{"an empty interface body", "interface I { /* empty interface */ }", []string{"/* empty interface */"}},
		{"an empty enum body", "enum E { /* empty enum */ }", []string{"/* empty enum */"}},
		{"an empty type literal", "type T = { /* empty type literal */ };", []string{"/* empty type literal */"}},
		{"an empty object literal", "const o = { /* empty object */ };", []string{"/* empty object */"}},
		{"an empty array literal", "const a = [ /* empty array */ ];", []string{"/* empty array */"}},
		{"an empty argument list", "f( /* empty args */ );", []string{"/* empty args */"}},
		{"an empty parameter list", "function g(/* empty params */) {}", []string{"/* empty params */"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			comments := All(parseForComments(t, testCase.sourceText))

			gotTexts := make([]string, 0, len(comments))
			for _, comment := range comments {
				gotTexts = append(gotTexts, comment.Text)
			}
			if len(gotTexts) != len(testCase.wantTexts) {
				t.Fatalf("expected %v, got %v", testCase.wantTexts, gotTexts)
			}
			for index, want := range testCase.wantTexts {
				if gotTexts[index] != want {
					t.Fatalf("comment %d: expected %q, got %q", index, want, gotTexts[index])
				}
			}
		})
	}
}

// The interior anchor must not invent a comment out of a slash that is not one.
//
// This is the failure mode of the approach that was tried first and rejected: sweeping the text for
// uncovered `/` characters read the `//` inside `'http://x'` as a comment start and produced
// `//x'; // real` as a finding. Every anchor in the shipped scan comes from the parser instead, so
// a slash inside a string or a regex is never a position the scan is offered. Pinned so a later
// cheapening cannot quietly reintroduce the text sweep.
func TestAllCommentsInventsNothingFromSlashesInLiterals(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		wantTexts  []string
	}{
		{"two slashes inside a string", "const s = 'http://x';\n", nil},
		{"two slashes inside a string, with a real comment after", "const s = 'http://x'; // real\n", []string{"// real"}},
		{"a slash inside a regex", "const r = /a\\/b/.test(c);\n", nil},
		{"a division", "const d = 6 / 2;\n", nil},
		{"a slash inside a template literal", "const t = `http://x`;\n", nil},
		{"a comment marker inside an empty object's string neighbour", "const o = { /* c */ }; const s = '//';\n", []string{"/* c */"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			comments := All(parseForComments(t, testCase.sourceText))

			gotTexts := make([]string, 0, len(comments))
			for _, comment := range comments {
				gotTexts = append(gotTexts, comment.Text)
			}
			if len(gotTexts) != len(testCase.wantTexts) {
				t.Fatalf("expected %v, got %v", testCase.wantTexts, gotTexts)
			}
			for index, want := range testCase.wantTexts {
				if gotTexts[index] != want {
					t.Fatalf("comment %d: expected %q, got %q", index, want, gotTexts[index])
				}
			}
		})
	}
}

// TestAllCommentsDeduplicates guards the scan's one real hazard: it starts from every node
// position, and a node and its first child usually begin at the same place, so the same comment is
// offered repeatedly. A rule fed duplicates reports one defect several times.
func TestAllCommentsDeduplicates(t *testing.T) {
	sourceText := "// once\nexport const value = { nested: { deeper: 1 } };\n"

	comments := All(parseForComments(t, sourceText))
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

	comments := All(parseForComments(t, sourceText))
	if len(comments) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(comments))
	}

	if comments[0].IsBlock || comments[0].IsJsDoc() {
		t.Fatalf("a double-slash comment is neither a block nor JSDoc: %+v", comments[0])
	}
	if !comments[1].IsBlock || comments[1].IsJsDoc() {
		t.Fatalf("a slash-star comment is a block but not JSDoc: %+v", comments[1])
	}
	if !comments[2].IsBlock || !comments[2].IsJsDoc() {
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
			comments := All(parseForComments(t, testCase.sourceText))
			if len(comments) == 0 {
				t.Fatal("expected a comment")
			}
			gotLines := comments[0].ContentLines()
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
