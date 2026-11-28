package yaml

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/format/yaml/unist"
	"github.com/system-inc/cohere/internal/testing/recorded"
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

// unsettledFixtures are the inputs whose fork output the fork itself rewrites, so Format, which prints as the fork
// does, rewrites it on a second pass too. Their second pass is held to the fork's own, recorded beside the first in
// <test>SecondPass.json, so a change to it in either direction fails, settling included. One whose recorded
// second pass settles everywhere fails too, so the list only shrinks.
var unsettledFixtures = map[string]bool{
	// The mapping is indented anew but the block scalar keeps its indentation indicator, so its text no longer sits
	// where `|2` says and the output does not parse.
	"a:\n  b: |2\n     x\n    y": true,
	// The ignored item keeps the indentation of its source and its sibling takes the new one, so the two no longer
	// start at one column and the output does not parse.
	"a:\n  # prettier-ignore\n  b:   {c:   d}\n  e:   {f:   g}": true,
	// A comment alone in an empty flow sequence prints after a lone comma, which does not parse.
	"a: [\n  # c\n]": true,
	// A comment alone in an empty flow mapping prints after a lone comma, which does not parse.
	"a: {\n  # c\n}": true,
	// The yaml-test-suite's M5DY, Spec Example 2.11: the second key, a flow sequence, drops its `?` and prints as an
	// implicit key on the line after the first pair's explicit `?` and `:`, which the fork's parser refuses on a second
	// pass ("Map comment with trailing content" at 5:1).
	"? - Detroit Tigers\n  - Chicago cubs\n:\n  - 2001-07-23\n\n" +
		"? [ New York Yankees,\n    Atlanta Braves ]\n: [ 2001-07-02, 2001-08-12,\n    2001-08-14 ]\n": true,
}

// secondPass is what is wrong with expected, the fork's output for input, formatted again under options and
// proseWrap, or "" when nothing is: formatted output formats to itself, or every run rewrites what the last one
// wrote. An input in unsettledList is held instead to the fork's own second pass, which forkAgain answers from its
// recording: the same text, or a refusal on both sides, Format's a syntax error. unsettledAsListed says the fork's
// second pass did not settle.
func secondPass(
	unsettledList map[string]bool,
	input string,
	expected string,
	options format_options.Options,
	proseWrap string,
	forkAgain func() (string, error),
) (wrong string, unsettledAsListed bool) {
	again, againError := formatFullStack("fixture.yaml", expected, options, proseWrap, nil)
	if !unsettledList[input] {
		if againError != nil || again != expected {
			return fmt.Sprintf("formatted again (%v): %s", againError, differential.FirstDifference(expected, again)),
				false
		}
		return "", false
	}
	want, wantError := forkAgain()
	unsettledAsListed = wantError != nil || want != expected
	switch {
	case wantError != nil && againError != nil:
		if !printing.IsSyntax(againError) {
			return fmt.Sprintf("formatted again, refused as the fork refuses it, but not as syntax: %v", againError),
				unsettledAsListed
		}
	case (wantError != nil) != (againError != nil) || again != want:
		return fmt.Sprintf("formatted again, not as the fork formats it again (%v, the fork %v): %s", againError,
			wantError, differential.FirstDifference(want, again)), unsettledAsListed
	}
	return "", unsettledAsListed
}

// requireUnsettled fails for each input of unsettledList the comparison asked about whose recorded second pass in
// the fork settled every time, so a fix upstream shrinks the list rather than leaving an entry that no longer holds.
func requireUnsettled(test *testing.T, unsettledList map[string]bool, inputs []string, unsettled map[string]bool) {
	test.Helper()
	// Only a clean run judges the list: an input whose first pass differed never reached its second.
	if test.Failed() {
		return
	}
	for _, input := range inputs {
		if unsettledList[input] && !unsettled[input] {
			test.Errorf("the fork formats %q to itself on a second pass, so take it out of the unsettled list", input)
		}
	}
}

// compareFormat formats every input with Prettier and with the port, Format from the text, under every
// variant, and reports each difference. Where Prettier fails, Format must fail too, with an error
// printing.IsSyntax recognizes. Prettier's answers are golden's, recorded (see oracle_testing) so no JavaScript
// runs here, and its second passes forkAgainGolden's; a test that compares in subtests opens both once and
// shares them, so each golden is held to every answer across all of them.
func compareFormat(
	test *testing.T,
	golden *oracle_testing.Golden,
	forkAgainGolden *oracle_testing.Golden,
	inputs []string,
) int {
	test.Helper()
	failures, compared, oracleFailures := 0, 0, 0
	unsettled := map[string]bool{}
	for variantIndex, options := range formatVariants {
		formatter := golden.Formatter(options)
		forkAgain := forkAgainGolden.Formatter(options)
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
			} else if wrong, unsettledAsListed := secondPass(
				unsettledFixtures,
				input,
				expected,
				options,
				"preserve",
				func() (string, error) { return forkAgain.Format("fixture.yaml", expected) },
			); wrong != "" {
				failures++
				test.Errorf("variant %d, %q: %s", variantIndex, input, wrong)
			} else if unsettledAsListed {
				unsettled[input] = true
			}
		}
	}
	requireUnsettled(test, unsettledFixtures, inputs, unsettled)
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
	compareFormat(test, oracle_testing.Open(test, test.Name()), oracle_testing.Open(test, test.Name()+"SecondPass"),
		formatFixtures)
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

// suiteDirectory holds the yaml-test-suite's cases, recorded from the package in the Prettier fork's
// node_modules (see package recorded), with the suite's MIT notices beside them.
var suiteDirectory = filepath.Join("testdata", "yaml_test_suite")

// suiteInputs is every input of one part of the yaml-test-suite (cases_<n>.json), and each case's canonical
// dump (dumps_<n>.json).
func suiteInputs(test *testing.T, cases string, part int) []string {
	test.Helper()
	read, _ := readInputs(test, cases, filepath.Join(suiteDirectory, fmt.Sprintf("dumps_%d.json", part)))
	inputs := make([]string, len(read))
	for index, input := range read {
		inputs[index] = input.Text
	}
	return inputs
}

// TestYamlTestSuiteMatchesOracle runs the suite through both sides under every option set. Inputs the
// oracle cannot format (the suite's error cases) must fail in the parser too. Each of the suite's parts
// is a subtest, and all of them read the one golden, split into parts of its own under 50 KB.
func TestYamlTestSuiteMatchesOracle(test *testing.T) {
	test.Parallel()
	parts, partsError := recorded.Parts(suiteDirectory, "cases_")
	if partsError != nil {
		test.Fatal(partsError)
	}
	golden := oracle_testing.Open(test, test.Name())
	forkAgainGolden := oracle_testing.Open(test, test.Name()+"SecondPass")
	for index, cases := range parts {
		test.Run(fmt.Sprintf("part_%d", index+1), func(test *testing.T) {
			test.Parallel()
			compareFormat(test, golden, forkAgainGolden, suiteInputs(test, cases, index+1))
		})
	}
}
