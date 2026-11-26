package yaml

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/format/yaml/unist"
)

// formatFixtures reach the printer's branches: every node type, comments in every slot they attach to,
// flow collections that break, each block scalar header, the quote choices, prettier-ignore, document
// markers and directives. Each is formatted by the port, the Go parser and the printer, and compared with
// what the embedded Prettier printed for it, under every option set below.
var formatFixtures = []string{
	// Empty and trivial files.
	"",
	"\n",
	"\n\n\n",
	"a",
	"a\n",
	"  a  \n",
	"# c",
	"# c\n",
	"#",
	"---",
	"---\n",
	"...",
	"...\n",

	// Plain scalars, single and multi-line, and their whitespace.
	"a b  c",
	"a:    b",
	"a: b   c  ",
	"a: b\tc",
	"a:\n  b\n  c",
	"a: b\n  c\n\n  d",
	"a: b\n\n\n  c",
	"- a\n  b",
	"a\n b\n  c",
	"a: 'x'\nb: c\n",
	"a: é\nb: 🍐 x\nc: 中文 中文",
	"a: \u00a0b",
	"key: value # c",
	"a: b\n   c   \n   d",
	"a: 1\nb: 2.5\nc: true\nd: null\ne: ~",
	"a: -1\nb: .inf",

	// Quotes: which quote, escapes, embedded quotes, and multi-line quoted scalars.
	`"a"`,
	`'a'`,
	`a: "b"`,
	`a: 'b'`,
	`"a'b"`,
	`'a"b'`,
	`"a\"b"`,
	`'a''b'`,
	`"a\nb"`,
	`'a\b'`,
	`"a\"b'c"`,
	`'a''b"c'`,
	`"a\\"`,
	`"\""`,
	`"\\\""`,
	`''''`,
	`""`,
	`''`,
	"\"a\n  b\"",
	"'a\n\n  b'",
	"\"a \\\n  b\"",
	"a: \"multi\n  line  \n  text\"",
	"- 'x''y'\n- \"x'y\"",
	`"a\tb"`,
	`"\u263A"`,

	// Block scalars: style, chomping, explicit indentation, indicator comments, trailing lines.
	"a: |\n  x\n  y\n",
	"a: >\n  x\n  y\n",
	"a: |-\n  x\n",
	"a: |+\n  x\n",
	"a: >-\n  x\n",
	"a: >+\n  x\n\n",
	"a: |2\n   x\n  y\n",
	"a: |1\n  x\n",
	"a: >2-\n   x\n",
	"a: |+\n  x\n\nb: c",
	"a: |\n  x\n\n\nb: c",
	"a: |\n  x\n\n\n",
	"a: | # c\n  x\n",
	"a: >- # c\n  x\n  y",
	"|\n  x\n",
	">\n  x\n\n  y\n",
	"- |\n  x\n- y",
	"a:\n  b: |2\n     x\n    y",
	"a: >\n  a\n    b\n  c\n",
	"a: |\nb: c",
	"a: |\n",
	"a: >\n\n  x\n",
	"a: |+\n  x\n\n\n",
	"- |+\n  x\n\n",
	"a:\n  - |-\n    x\n  - >+\n    y\n\nb: c",
	"a: |\n  x  \n  y\t\n",
	"a: >\n  x\n\n\n  y\n",
	"a: |\n    x\n  y\n",
	"a: >\n  é 🍐\n  中文\n",
	"--- |\n  x\n",
	"--- >-\n  x\n...\n",
	"a: |\n  x\n# c\n",
	"a: |\n  x\n\n# c\n",
	"a: |+\n  x\n# c\n",
	"- >\n  long folded text that goes on and on\n  and on\n",

	// Anchors, aliases, tags.
	"a: &x 1\nb: *x",
	"*x : a",
	"&a a: b",
	"!!str a",
	"!t &a b",
	"&a !t b",
	"!!map\na: b",
	"&a\n- b",
	"a: !!seq\n- b",
	"a: &x\n  b: c",
	"a: !t\n  - b",
	"--- !t\na: b",
	"!!set\n? a\n? b",
	"? a\n? b",
	"a: !!str",
	"a: &x",
	"- !t\n- &a\n- *a",
	"a: !<tag:x> b",
	"a: !local b",
	"[*a, &b c, !t d]",
	"{*a : b, &c d: e}",
	"a: !t # c\n  b",
	"a: &x # c\n  # d\n  b: c",
	"!t\n# c\nb",

	// Comments in every slot: leading, trailing, middle, end, head, indicator, document.
	"# c\na: b",
	"a: b # c",
	"a: # c\n  b",
	"a:\n  # c\n  b",
	"a:\n  - b\n  # c\n",
	"a:\n  b: c\n  # d",
	"a: b\n# c",
	"a: b\n\n# c",
	"a:\n  b: c\n\n# d",
	"# c\n---\na: b",
	"--- # c\na",
	"a\n... # c",
	"a: b\n# c\nd: e",
	"a: b\n\n\nc: d",
	"- a # c\n- b",
	"- # c\n  a",
	"- # c",
	"a: # c",
	"? a # c\n: b",
	"? a\n# c\n: b",
	"a: # c\n  - b",
	"a:\n  - b # c\n  - c",
	"- a\n  # c\n- b",
	"- - a\n  # c\n- b",
	"a:\n  b:\n    c: d\n    # e\n  # f\n# g",
	"# a\n\n# b\n\nc: d",
	"a: b # c\n# d",
	"a: [b] # c",
	"a: {b: c} # d",
	"a:\n  # b\n\n  # c\n  d: e",
	"%YAML 1.2 # c\n---\na",
	"# c\n%YAML 1.2\n---\na",
	"a:\n- b\n# c\n\n# d",
	"a: b\n  # c",
	"? # c\n  a\n: b",
	"key: # c\n  value",
	"a: !t # c\n  - b",

	// prettier-ignore.
	"# prettier-ignore\na:   [1,   2]\nb:   [1,  2]",
	"a:\n  # prettier-ignore\n  b:   {c:   d}\n  e:   {f:   g}",
	"# prettier-ignore\n---\na:   [b,   c]",
	"- # prettier-ignore\n  a:   [b,   c]",
	"#  prettier-ignore  \nkey  :  value",
	"# prettier-ignore\na: |\n    x   \n",
	"a: 1\n# prettier-ignore\nb:    [1,   2]   \n\nc:   3",

	// Flow collections: empty, nested, long enough to break, comments and empty lines inside.
	"[a, b, c]",
	"{a: 1, b: 2}",
	"[]",
	"{}",
	"[ ]",
	"{ }",
	"a: []",
	"a: {}",
	"[a, b, ]",
	"{a, b}",
	"{a: }",
	"[a: b]",
	"{? a}",
	"{: b}",
	"[? a : b]",
	"[a: b, c: d]",
	"{a: [1, 2], b: {c: d}}",
	"[[a], [b, [c]]]",
	"[{a: b}, {c: d}]",
	"[\n  a,\n\n  b\n]",
	"[\n  a\n  # c\n]",
	"{\n  a: b,\n  # c\n}",
	"[a, # c\n b]",
	"{a: 1, # c\n b: 2}",
	"a: [] # c",
	"[a\n b]",
	"{a: b\n c}",
	"[\"a\", 'b']",
	"key: [aaaaaaaa, bbbbbbbbb, cccccccccc, dddddddddd, eeeeeeeeee, ffffffffff, gggggggggg, hhhhhhhhhh, iiiiiiiiii, jjjjjjjjjj]",
	"key: {aaaaaaaa: 1, bbbbbbbbb: 2, cccccccccc: 3, dddddddddd: 4, eeeeeeeeee: 5, ffffffffff: 6, gggggggggg: 7, hh: 8}",
	"- [aaaaaaaa, bbbbbbbbb, cccccccccc, dddddddddd, eeeeeeeeee]\n- {a: [bbbbbbbbbbbbbbbbbb, cccccccccccccccccc]}",
	"[a, b]: c",
	"{a: b}: c",
	"? [a, b]\n: c",
	"a: [b,\n\n  c, d]",
	"{a: b, c: }",
	"{a, b: c, }",
	"[a, {b: c}, ]",
	"a: {\n}",
	"a: [\n  # c\n]",
	"a: {\n  # c\n}",
	"a: { } # c\n",
	"{a: [b]}: c",
	"[,]",

	// Mapping items: explicit and implicit keys, empty keys and values, long keys.
	"? a\n: b",
	"? - a\n: b",
	"? |\n  a\n: b",
	"? a\n",
	": b",
	"? \n: ",
	":",
	"? a\n  b\n: c",
	"? !t a\n: b",
	"a:\n  # c\n  b",
	"a: !!map\n  b: c",
	"a: &x\n  - b",
	"a:\n  b:\n    c:\n      d: e",
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: b",
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:\n  - b",
	"\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\": b",
	"a: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	"a: bbbbbbbbbbbbbbbbbbbbbbbbbbb cccccccccccccccccccccccccc dddddddddddddddddddddddddddddd eeeeeeeeeeeeeeeeeeee fffffffffffff",
	"a: \"bbbbbbbbbbbbbbbbbbbbbbbbbbb cccccccccccccccccccccccccc dddddddddddddddddddddddd eeeeeeeeeeeeeeeeeeee fffffffffffff\"",
	"a: b\n  c",
	"a b: c",
	"a:b: c",
	"a:\n  b",
	"a:\n\n  b",
	"a: \n  - b\n  - c",
	"a:\n- b\n- c",
	"a:\n  -\n  - b",
	"? a\n: - b\n  - c",
	"? a\n: |\n  b",
	"*a :",
	"*a : b",
	"? *a\n: b",
	"&a a:",
	"{*a : b}",
	"a: [b]\nc: {d: e}",
	"? a\n: b\n? c\n: d",
	"a: &x b\nc: *x\n",
	"!!set\n? aaaaaaaa\n? b",
	"? \"a\n  b\"\n: c",
	"'a\n  b': c",

	// Sequences.
	"- a\n- b",
	"- - a\n  - b",
	"- a: b\n  c: d",
	"-\n  a",
	"- ",
	"-",
	"- - - a",
	"- a\n\n- b",
	"- a\n\n\n- b",
	"-   a\n-   b",
	"- [a]\n- {b: c}",
	"- ? a\n  : b",
	"- &a\n  - b",
	"- !t\n  a: b",
	"a:\n  - b\n  -\n    - c",

	// Documents, markers and directives.
	"---\na\n---\nb",
	"a\n...\n---\nb",
	"%YAML 1.2\n---\na",
	"%TAG ! tag:x,2000:\n---\n!a b",
	"---\n---",
	"--- a",
	"a\n--- # c\nb",
	"a\n...\n# c\n---\nb",
	"%YAML 1.2\n---\na\n...\n%YAML 1.2\n---\nb",
	"a\n...\n",
	"---\na\n...\n",
	"--- # c\n",
	"---\n# c\n",
	"a\n---\n",
	"---\n...\n---\n...\n",
	"# c\n---\n# d\n...",
	"%FOO bar baz\n---\na",
	"---\n- a\n---\n- b\n...\n",
	"a\n---\n\n\nb",
	"--- !!map\na: b\n--- !!seq\n- c",

	// Edges: an empty last flow item, end comments on a value, end comments after an empty line, a
	// document's own trailing comment, blank and whitespace-only block scalars, tabs inside folded text.
	"{a: 1, : }",
	"[a, : ]",
	"{ : }",
	"[ : ]",
	"{a, :}",
	"a: []\n  # c",
	"a: {}\n  # c\n",
	"a:\n  # c",
	"a: b\n  # c\nd: e",
	"a:\n  - b\n\n  # c",
	"a:\n  b: c\n\n  # d\ne: f",
	"- a\n\n  # c\n- b",
	"a: b\n... # c\n---\nc",
	"a\n... # c\n",
	"--- a\n... # c\n--- b",
	"a: |\n  \n\nb: c",
	"a: |\n   \n",
	"a: >\n\n\n",
	"- |\n\n- b",
	"a: >\n  x\t y\n",
	"a: >\n  x\t y\n  z\n",
	"a: |\n  x\n\n\n\nb: c",
	"- |\n  x\n\n\n- y",
	"a:\n  b: |\n    x\n\n\n  c: d",
	"a: [] # c\n  # d",
	"a: !t []\n  # c",
	"? a\n  b\n: # c",
	"\"a\n  b\": # c",

	// Line endings, byte order marks, tabs, non-ASCII and astral characters.
	"a: b\r\nc: d\r\n",
	"a: |\r\n  x\r\n  y\r\n",
	"\ufeffa: b\n",
	"a: b\rc: d",
	"a:\tb",
	"- é\n- 中文\n- 🍐🍐",
	"\"é 🍐\": '中'",
	"a: \"\u2028\"",
	"a: |\n  🍐 x\n  中\n",
	"a: >\n  aaaa bbbb 中文 cccc 🍐 dddd eeee ffff gggg hhhh\n",
}

// formatVariants are the option sets every fixture runs under: our defaults (tabWidth 4, single quotes,
// printWidth 120), narrow (printWidth 40, tabWidth 2, double quotes), tabs, and the less common
// settings the printer reads (no bracket spacing, no trailing comma, CRLF).
var formatVariants = func() []format_options.Options {
	defaults := format_options.Default()
	narrow := format_options.Default()
	narrow.PrintWidth, narrow.TabWidth, narrow.SingleQuote = 40, 2, false
	tabs := format_options.Default()
	tabs.UseTabs, tabs.SingleQuote, tabs.PrintWidth, tabs.EndOfLine = true, false, 40, "cr"
	other := format_options.Default()
	other.PrintWidth, other.BracketSpacing, other.TrailingComma, other.EndOfLine = 40, false, "none", "crlf"
	return []format_options.Options{defaults, narrow, tabs, other}
}()

// normalizeInput is what Prettier's core does to a file before parsing: remove a byte order mark and
// normalize line endings to \n.
func normalizeInput(text string) (string, bool) {
	hasByteOrderMark := strings.HasPrefix(text, "\ufeff")
	withoutByteOrderMark := strings.TrimPrefix(text, "\ufeff")
	normalized := strings.ReplaceAll(strings.ReplaceAll(withoutByteOrderMark, "\r\n", "\n"), "\r", "\n")
	return normalized, hasByteOrderMark
}

// formatFullStack is Format as native.Formatter calls it: the byte order mark removed and line endings
// normalized before, the mark restored after.
func formatFullStack(
	fileName string,
	input string,
	options format_options.Options,
	proseWrap string,
	textToDoc printing.TextToDoc,
) (string, error) {
	text, hasByteOrderMark := normalizeInput(input)
	formatted, formatWithProseWrapError := formatWithProseWrap(fileName, text, options, proseWrap, textToDoc)
	if formatWithProseWrapError != nil {
		return "", formatWithProseWrapError
	}
	if hasByteOrderMark {
		formatted = "\ufeff" + formatted
	}
	return formatted, nil
}

// parseFixture is the tree Format prints for text: the Go parser's, with root.comments deleted as
// parser-yaml.js deletes them.
func parseFixture(test testing.TB, text string) *unist.Node {
	test.Helper()
	root, parseError := parse(text, nil)
	if parseError != nil {
		test.Fatal(parseError)
	}
	return root
}

// compareFormat formats every input with Prettier and with the port, Format from the text, under every
// variant, and reports each difference. Where Prettier fails, Format must fail too, with an error
// printing.IsSyntax recognizes.
func compareFormat(test *testing.T, inputs []string) int {
	test.Helper()
	failures, compared, oracleFailures := 0, 0, 0
	// The oracle's answers, recorded (see oracle_testing), so no JavaScript runs here.
	golden := oracle_testing.Open(test, test.Name())
	for variantIndex, options := range formatVariants {
		formatter := golden.Formatter(options)
		for _, input := range inputs {
			expected, oracleError := formatter.Format("fixture.yaml", input)
			fullStack, fullStackError := formatFullStack("fixture.yaml", input, options, "preserve", nil)
			if oracleError != nil {
				// Prettier cannot format it; Format must refuse it too.
				oracleFailures++
				if fullStackError == nil || !printing.IsSyntax(fullStackError) {
					failures++
					test.Errorf(
						"variant %d, %q: the oracle failed (%v) but Format gave %q, %v",
						variantIndex,
						input,
						oracleError,
						fullStack,
						fullStackError,
					)
				}
				continue
			}
			compared++
			if fullStackError != nil {
				failures++
				test.Errorf("variant %d, %q: Format: %v", variantIndex, input, fullStackError)
			} else if fullStack != expected {
				failures++
				test.Errorf(
					"variant %d, %q: Format: %s",
					variantIndex,
					input,
					differential.FirstDifference(expected, fullStack),
				)
			}
		}
	}
	test.Logf(
		"%d comparisons over %d fixtures and %d option sets: %d differ; %d oracle failures (syntax errors)",
		compared,
		len(inputs),
		len(formatVariants),
		failures,
		oracleFailures,
	)
	return failures
}

func TestFormatFixturesMatchOracle(test *testing.T) {
	test.Parallel()
	compareFormat(test, formatFixtures)
}

// TestFormatOracleCanFail proves the comparison sees a difference: the port's output for one input
// against the oracle's for another.
func TestFormatOracleCanFail(test *testing.T) {
	test.Parallel()
	formatter := oracle_testing.Open(test, test.Name()).Formatter(format_options.Default())
	expected, formatError := formatter.Format("fixture.yaml", "a: [b]")
	if formatError != nil {
		test.Fatal(formatError)
	}
	text := "a: [b, c]"
	actual, printError := Print(parseFixture(test, text), text, format_options.Default(), nil)
	if printError != nil {
		test.Fatal(printError)
	}
	if actual == expected {
		test.Fatalf("the comparison found no difference: both printed %q", actual)
	}
}

// TestPrintDocLeavesTheTrailingHardline checks the embedding contract: PrintDoc's doc ends in the
// hardline that upstream's textToDoc strips, and the caller strips it.
func TestPrintDocLeavesTheTrailingHardline(test *testing.T) {
	test.Parallel()
	text := "a: b\n"
	document, printDocError := PrintDoc(parseFixture(test, text), text, format_options.Default(), nil)
	if printDocError != nil {
		test.Fatal(printDocError)
	}
	printed := docString(document)
	if printed != "a: b\n" {
		test.Fatalf("PrintDoc laid out is %q", printed)
	}
	if stripped := docString(stripTrailingHardline(document)); stripped != "a: b" {
		test.Fatalf("stripped, it is %q", stripped)
	}
	if strings.Contains(printed, "\r") {
		test.Fatal("a doc has no line endings of its own")
	}
}
