package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const warningCommentFile = "/project/source/Thing.ts"

// warningCommentWant is one finding a case expects: the word, and which occurrence of it in the source the finding
// covers, counted from 1, so a word the rule must skip earlier in the source is told apart from the one it reports.
type warningCommentWant struct {
	word       string
	occurrence int
}

// expectWarningWords requires exactly the wanted findings, in order, each covering its word at its occurrence. A
// count alone would pass a rule that reports the whole comment, which is what ESLint's `no-warning-comments` did.
func expectWarningWords(test *testing.T, sourceText string, wants []warningCommentWant) {
	test.Helper()
	result := rule_testing.Run(test, ConsistencyNoWarningComment, warningCommentFile, sourceText)
	wantIds := make([]string, 0, len(wants))
	for range wants {
		wantIds = append(wantIds, "warningComment")
	}
	if len(wants) == 0 {
		rule_testing.ExpectClean(test, result)
		return
	}
	rule_testing.ExpectFindings(test, result, wantIds...)
	for index, want := range wants {
		start := nthIndex(sourceText, want.word, want.occurrence)
		if start < 0 {
			test.Fatalf("the source holds no occurrence %d of %q", want.occurrence, want.word)
		}
		diagnostic := result.Diagnostics[index]
		if diagnostic.Range.Pos() != start || diagnostic.Range.End() != start+len(want.word) {
			test.Errorf("finding %d covers %q at %d, want %q at %d", index,
				sourceText[diagnostic.Range.Pos():diagnostic.Range.End()], diagnostic.Range.Pos(), want.word, start)
		}
		if !strings.Contains(diagnostic.Message.Description, `"`+want.word+`"`) {
			test.Errorf("finding %d's message does not name %q: %s", index, want.word, diagnostic.Message.Description)
		}
	}
}

// nthIndex is the byte offset of the occurrence'th match of word in text, counted from 1, or -1.
func nthIndex(text string, word string, occurrence int) int {
	offset := 0
	for range occurrence {
		found := strings.Index(text[offset:], word)
		if found < 0 {
			return -1
		}
		offset += found + len(word)
	}
	return offset - len(word)
}

// The verdict's reported shapes (textrules-decisions.md, "Warning comments"): each word, anywhere in any comment, on
// the word itself.
func TestConsistencyNoWarningCommentFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wants      []warningCommentWant
	}{
		{"a word opening a line comment", "// TODO: fix this\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}}},
		{"each of the three words", "// FIXME and XXX and TODO\nexport const Value = 1;\n",
			[]warningCommentWant{{"FIXME", 1}, {"XXX", 1}, {"TODO", 1}}},
		{"a word mid-sentence", "// see the TODO below\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}}},
		{"the second line of a block comment", "/* intro\n   TODO fix this */\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}}},
		{"a decorated line of a JSDoc comment",
			"/**\n * Reads the value.\n * FIXME: cache it\n */\nexport const Value = 1;\n",
			[]warningCommentWant{{"FIXME", 1}}},
		{"a trailing comment", "export function value(): number {\n    return 1; // TODO fix\n}\n",
			[]warningCommentWant{{"TODO", 1}}},
		{"a word after multibyte text, on its byte", "// café TODO fix\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}}},
		{"punctuation and hyphens are boundaries", "// (TODO), FIXME! and TODO-later\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}, {"FIXME", 1}, {"TODO", 2}}},
		// A backtick pair masks only what it holds, so the second word, outside it, is reported.
		{"a mention beside a task", "// `TODO` is a mention, and TODO is not\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 2}}},
		// An unclosed backtick masks nothing, or opening one would turn the rule off for the rest of the comment.
		{"an unclosed backtick", "// an unclosed ` before FIXME\nexport const Value = 1;\n",
			[]warningCommentWant{{"FIXME", 1}}},
		// A blank line ends a group, so the lone backtick above it pairs with nothing below it.
		{"a blank line ends a group", "// a lone ` here\n\n// TODO and ` there\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}}},
		// The trailing comment is a group of its own, as go/parser groups a line comment, so its lone backtick
		// pairs with nothing in the passage under it.
		{"a trailing comment is a group of its own",
			"export const Value = 1; // a lone ` here\n// TODO and ` there\nexport const Other = 2;\n",
			[]warningCommentWant{{"TODO", 1}}},
		// Code between two comments ends the group, though they sit on adjacent lines.
		{"code between comments ends a group", "/* a lone ` */ export const Value = 1;\n// TODO and ` there\n",
			[]warningCommentWant{{"TODO", 1}}},
		// An indented line straight after prose continues the paragraph, as CommonMark reads it, so it is read.
		{"a continuation line is prose", "/*\n * Prose.\n *     TODO in a continuation\n */\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}}},
		{"two of one word are two findings", "// TODO, then TODO\nexport const Value = 1;\n",
			[]warningCommentWant{{"TODO", 1}, {"TODO", 2}}},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			expectWarningWords(test, testCase.sourceText, testCase.wants)
		})
	}
}

// The shapes that look like a task and are not: another case, a longer word, a mention, quoted code, and the words
// outside a comment.
func TestConsistencyNoWarningCommentStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"lowercase is prose", "// todo: fix this, and a fixme branch, and xxx in a name\nexport const Value = 1;\n"},
		{"a capitalized sentence opener", "// Todo items are sorted, Fixme too\nexport const Value = 1;\n"},
		{"an underscore continues the word", "// TODO_LIST holds the queue\nexport const Value = 1;\n"},
		{"a digit continues the word", "// TODO2 holds the second queue\nexport const Value = 1;\n"},
		{"a longer word", "// TODOS, myTODO, XXXX, XXXL and 0xXXX\nexport const Value = 1;\n"},
		{"a letter of any script continues the word",
			"// ÜTODO and TODOé are other words\nexport const Value = 1;\n"},
		{"a word in backticks", "// the `TODO` marker\nexport const Value = 1;\n"},
		{"a backtick pair wrapped across line comments",
			"// a pair `wraps onto\n// TODO the next` line\nexport const Value = 1;\n"},
		{"a backtick pair wrapped in a block comment",
			"/*\n * a pair `wraps onto\n * FIXME the next` line\n */\nexport const Value = 1;\n"},
		{"a fence across line comments",
			"// The marker reads like this:\n//\n// ```\n// TODO fix the parser\n// ```\nexport const Value = 1;\n"},
		{"a fence in a block comment", "/*\n * ```\n * XXX races\n * ```\n */\nexport const Value = 1;\n"},
		{"an indented code block",
			"/*\n * Shown as code:\n *\n *     legacy(); // TODO in a block\n */\nexport const Value = 1;\n"},
		{"the words in code", "export const Marker = 'TODO';\nexport const Comment = '// FIXME cache this';\n"},
		{"the words in a template", "export const Block = `/* XXX the cache */`;\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			expectWarningWords(test, testCase.sourceText, nil)
		})
	}
}

// A CRLF file's block comment keeps its carriage returns in the source, and the finding must still land on the
// word: the rule reads them as spaces, a byte for a byte, so no offset drifts. Its blank gutter line must still read
// as blank, or the indented block after it would not open and its word would be reported.
func TestConsistencyNoWarningCommentInACrlfFile(test *testing.T) {
	test.Parallel()

	reported := strings.Join([]string{
		"/*",
		" * A block comment in a CRLF file,",
		" * FIXME the drift",
		" */",
		"export const Value = 1;",
		"",
	}, "\r\n")
	expectWarningWords(test, reported, []warningCommentWant{{"FIXME", 1}})

	codeBlock := strings.Join([]string{
		"/*",
		" * Shown as code:",
		" *",
		" *     legacy(); // TODO in a block",
		" */",
		"export const Value = 1;",
		"",
	}, "\r\n")
	expectWarningWords(test, codeBlock, nil)
}
