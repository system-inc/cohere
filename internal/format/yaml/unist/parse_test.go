package unist

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/needs"
)

// fixtures reach every transform and every branch of attach and updatePositions: each node type with
// and without tag, anchor and comments around it; comments in every slot; documents with and without
// markers, directives and trailing comments; the !!pairs and !!omap shapes upstream fails on; and the
// edges: end of input in each place, \r\n, tabs, non-ASCII and astral characters.
var fixtures = []string{
	// Empty and trivial streams.
	"",
	"\n",
	"\n\n\n",
	" ",
	"  \n  ",
	"\t",
	"a",
	"a\n",
	"  a  \n",
	"# c",
	"# c\n",
	"#",
	"#\n#",
	"---",
	"---\n",
	"...",
	"...\n",
	"\ufeff",
	"\ufeffa",
	"\ufeff# c\n",
	"\r\n",
	"a\r\n",
	"a\r",
	"\r",

	// Plain scalars.
	"a b  c",
	"a:    b",
	"a: b   c  ",
	"a: b\tc",
	"a:\n  b\n  c",
	"a: b\n  c\n\n  d",
	"a\n b\n  c",
	"a: é\nb: 🍐 x\nc: 中文",
	"a: \u00a0b",
	"key: value # c",
	"1",
	"a: 1\nb: 2.5\nc: true\nd: null\ne: ~",
	"a: -1\nb: .inf",
	"a:\u3000b",

	// Quoted scalars.
	`"a"`,
	`'a'`,
	`a: "b"`,
	`a: 'b'`,
	`"a'b"`,
	`'a"b'`,
	`"a\"b"`,
	`'a''b'`,
	`"a\nb"`,
	`"\ud800"`,
	`"\udc00x\ud83d"`,
	`"\ud83d\ude00"`,
	`"\x41\u00e9\U0001F600"`,
	"\"a\n  b\"",
	"'a\n\n  b'",
	"\"a \\\n  b\"",
	`""`,
	`''`,
	`"a" # c`,
	"'a' # c\n",
	`"🍐"`,
	"a: \"\u2028\"",

	// Block scalars.
	"a: |\n  x\n  y\n",
	"a: >\n  x\n  y\n",
	"a: |-\n  x\n",
	"a: |+\n  x\n",
	"a: >-\n  x\n",
	"a: >+\n  x\n\n",
	"a: |2\n   x\n  y\n",
	"a: |1\n  x\n",
	"a: >2-\n   x\n",
	"a: |-2\n   x\n",
	"a: |+\n  x\n\nb: c",
	"a: |\n  x\n\n\nb: c",
	"a: | # c\n  x\n",
	"a: >- # c\n  x\n  y",
	"|\n  x\n",
	">\n  x\n\n  y\n",
	"- |\n  x\n- y",
	"a: |\nb: c",
	"a: |\n",
	"a: |\n  x\n# c\n",
	"a: |\n  x\n\n# c\n",
	"a: |+\n  x\n# c\n",
	"--- |\n  x\n",
	"--- >-\n  x\n...\n",
	"|",
	">",
	"a: !t |\n  x\n",
	"a: &x |\n  x\n",
	"- !t &a | # c\n  x",
	"? |\n  a\n: b",
	"a: |\n  x\n  # not a comment\n",

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
	"! a",
	"!! a",
	"a: ! b",
	"&a",
	"!t",
	"&a # c",
	"!t # c\n",
	"*a",
	"*a # c",
	"&a !t # c\n# d\nb",
	"%TAG !e! tag:example.com,2000:\n---\n!e!x a",
	"%TAG ! tag:x,2000:\n---\n!a b",
	"a: !!binary aGVsbG8=",
	"a: !!timestamp 2001-12-14",
	"- !!int 1\n- !!float 1.5\n- !!null\n- !!bool true",
	"<<: *a",
	"a: &a\n  b: c\nd:\n  <<: *a",

	// Comments in every slot.
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
	"a:\n  b:\n   #b\n #a",
	"a:\n  b:\n #a\n   #a",
	"a:\n  - b:\n      c\n    # d\n  # e",
	"? a\n  # b\n: c\n  # d",
	"? a\n: b\n  # c\n# d",
	"? |\n  a\n  # b\n: c",
	"a: |\n  x\n  # c\nb: d",
	"[\n  a\n  # c\n]",
	"{\n  a: b,\n  # c\n}",
	"[a, # c\n b]",
	"{a: 1, # c\n b: 2}",
	"[\n# c\n]",
	"{\n# c\n}",
	"[a # c\n]",
	"{a: b # c\n}",
	"a:\n  - [b,\n    # c\n    d]",
	"- a\n # c\n- b",
	"-\n  # c\n  a",
	"a:\n  # c\n",
	"a:\n# c\nb:",
	"? a\n# c",
	"? a\n  # c",
	"- ? a\n  # c\n  : b",
	"# c\n# d\n---\n# e\na\n# f\n...\n# g\n---\n# h\nb\n# i",
	"a # c\n---\nb # d",
	"--- # c\n--- # d\n",

	// Flow collections.
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
	"[a\n b]",
	"{a: b\n c}",
	"[\"a\", 'b']",
	"[a, b]: c",
	"{a: b}: c",
	"? [a, b]\n: c",
	"{a: b, c: }",
	"{a, b: c, }",
	"[a, {b: c}, ]",
	"a: {\n}",
	"a: { } # c\n",
	"{a: [b]}: c",
	"[,]",
	"{a: 1, : }",
	"[a, : ]",
	"{ : }",
	"[ : ]",
	"{a, :}",
	"[!t a: b]",
	"[&x a: b]",
	"[!t a]",
	"[&a [b]]",
	"{!t a: !u b}",
	"[a: !t ]",
	"[: b]",
	"[a:]",
	"[\"a\": b]",
	"['a':b]",
	"[a, b: c, d]",
	"!t [a]",
	"&a {b: c}",
	"!t # c\n[a]",
	"[*a : b]",
	"[? a]",
	"[? ]",
	"{a: b, c}",
	"{\"a\":b}",
	"[a,\n# c\nb]",
	"{a: [b, {c: [d]}]}",
	"[[[[[[a]]]]]]",
	"[a, [b, c], {d: e}, f: g]",

	// Mapping items: explicit and implicit keys, empty keys and values.
	"? a\n: b",
	"? - a\n: b",
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
	"a b: c",
	"a:b: c",
	"a:\n  b",
	"a:\n\n  b",
	"a: \n  - b\n  - c",
	"a:\n- b\n- c",
	"a:\n  -\n  - b",
	"? a\n: - b\n  - c",
	"*a :",
	"*a : b",
	"? *a\n: b",
	"&a a:",
	"a: [b]\nc: {d: e}",
	"? a\n: b\n? c\n: d",
	"? \"a\n  b\"\n: c",
	"'a\n  b': c",
	"? a",
	"?",
	"? # c",
	": # c",
	"!t a: b",
	"!t : b",
	"&a : b",
	"? &a\n: b",
	"a:\n  !t\n  b: c",
	"a: !t\n",
	"? !t\n: !u\n",
	"a:\n  ? b\n  : c\n  d: e",

	// Sequences.
	"- a\n- b",
	"- - a\n  - b",
	"- a: b\n  c: d",
	"-\n  a",
	"- ",
	"-",
	"- - - a",
	"- a\n\n- b",
	"-   a\n-   b",
	"- [a]\n- {b: c}",
	"- ? a\n  : b",
	"- &a\n  - b",
	"- !t\n  a: b",
	"a:\n  - b\n  -\n    - c",
	"- !t",
	"- &a",
	"- !t # c\n- b",
	"- &a !t\n  a",
	"-\n-\n-",
	"- # c\n- # d",
	"- a\n-",
	"- : b",
	"- ? a",

	// The !!pairs and !!omap shapes: sequences of pairs, and the items upstream cannot read.
	"!!pairs\n- a: b\n- c: d",
	"!!omap\n- a: b\n- c: d",
	"- !!pairs\n  - &x a: b\n",
	"!!pairs\n- a\n",
	"!!pairs\n- {}\n",
	"!!pairs\n- a: b\n- c\n",
	"!!omap\n- !!map {}\n",
	"!!pairs [a]",
	"!!pairs [a: b]",
	"!!pairs\n- &a a\n",
	"!!pairs\n- !t\n",
	"!!omap\n- !t a: b",
	"!!pairs\n- # c\n  a: b",
	"!!pairs\n- ? a\n  : b",
	"!!pairs\n- : b",
	"!!omap\n- a: b\n  # c",
	"!!pairs\n- &x a: b",
	"!!pairs\n- !t a: b",
	"a: !!pairs\n  - b: c",
	"!!set\n? a\n? b: c",
	"!!omap\n- a: 1\n- a: 2",
	"!!pairs\n- a:",
	"!!pairs\n- a: b # c\n",
	"!!pairs\n- &x\n  a: b",

	// Documents, markers and directives.
	"---\na\n---\nb",
	"a\n...\n---\nb",
	"%YAML 1.2\n---\na",
	"%YAML 1.1\n---\na: yes",
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
	"%FOO\tbar  baz \n---",
	"%FOO \n---",
	"%YAML 1.2\n# c\n%TAG ! !\n# d\n---",
	"%YAML 1.2 # c\n# d\n---\n",
	"---\n- a\n---\n- b\n...\n",
	"a\n---\n\n\nb",
	"--- !!map\na: b\n--- !!seq\n- c",
	"a: b\n... # c\n---\nc",
	"a\n... # c\n",
	"--- a\n... # c\n--- b",
	"...\n...\n",
	"...\n# c\n",
	"# c\n...\n",
	"--- # c\n...\n",
	"--- !t\n...",
	"--- &a\n...",
	"--- !t # c\n",
	"---\n!t\n",
	"--- # c\n# d\na",
	"--- |\n  a\n... # c\n",
	"---\n\n\n...\n",
	"---\r\na\r\n...\r\n",
	"a\n...\n\n\n",
	"--- a\n...\n--- b\n...\n",
	"--- # a\n--- # b\n...",

	// Errors: the composer's messages, and their positions.
	"a: b: c",
	"a:\n  b\n c",
	"[a",
	"{a",
	"'a",
	"\"a",
	"a: 'b",
	"- a\nb: c",
	"a: &",
	"*",
	"%YAML 1.2\na",
	"a\n%YAML",
	"--- a\nb: c",
	"a: [b, c",
	"a: {b",
	"? a\n? b\nc",
	"a: |\n x\n  y\n z",
	"\ta: b",
	"a:\n\t- b",
	"{a: b}}",
	"]",
	"}",
	"a: b\n  - c",
	"&a &b c",
	"!a !b c",
	"%TAG\n---",
	"@a",
	"`a",
	"a: @b",
	"a: ]",
	"a: \"\\q\"",
	"a: !e!x b",
	"%YAML 2.0\n---\na",
	"%YAML 1.2\n%YAML 1.2\n---\na",
	"a: b\n...\nc: d\n... e",
	"[a]: b: c",
	"- a: b\n - c",
	"a: |0\n  x",
	"a: >10\n  x",
	"a: |x\n  y",
	"*a\n*b",
	"a: 'b\n\nc",
	"a: 1\na: 2",
	"{a: 1, a: 2}",

	// Line endings, tabs, non-ASCII and astral characters.
	"a: b\r\nc: d\r\n",
	"a: |\r\n  x\r\n  y\r\n",
	"\ufeffa: b\n",
	"a: b\rc: d",
	"a:\tb",
	"- é\n- 中文\n- 🍐🍐",
	"\"é 🍐\": '中'",
	"a: |\n  🍐 x\n  中\n",
	"🍐: 🍐 # 🍐\n# 🍐\n- x",
	"é:\n  - 🍐\n  # é\n",
	"a: b # c\r\n# d\r\n",
	"- a\r\n  # c\r\n- b\r\n",
	"--- # c\r\n...\r\n",
	"[🍐, é]: {中: 文}",
	"? 🍐\n: é",
	"a:\t# c\n  b",
	"a: b\t# c",
}

// generatedInputs draws inputs from YAML's indicators, properties, scalar styles, spaces, tabs, line
// breaks, comments and a few letters and non-ASCII characters, seeded so every run compares the same
// set.
func generatedInputs(count int) []string {
	random := rand.New(rand.NewPCG(5, 6))
	pieces := []string{
		" ", " ", " ", "  ", "\n", "\n", "\n", "\n  ", "\n    ", "\t", "\r\n",
		"-", "- ", "- ", "? ", ": ", ": ", ":", "#", " #", " # c", " # c", "\n# c", "\n  # c", "[", "]", "{", "}", ",",
		", ", "'", "\"", "|", ">", "|-", ">+", "|2", "&a ", "&", "!", "!!str ", "!!set ", "!!omap ", "!!pairs ",
		"!!map ", "!!seq ", "!foo ", "*a", "<<", "%YAML 1.2\n", "%TAG !e! tag:x,\n", "---", "...", "--- ", "... ",
		"a", "b", "ab", "x y", "1", "\u00e9", "\U0001F600", "\u4e2d",
	}
	inputs := make([]string, 0, count)
	for range count {
		length := 1 + random.IntN(24)
		var builder strings.Builder
		for range length {
			builder.WriteString(pieces[random.IntN(len(pieces))])
		}
		inputs = append(inputs, builder.String())
	}
	return inputs
}

func TestParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	failures, errored := compareAll(test, "fixture", nil, fixtures, 40)
	test.Logf("%d of %d fixtures match upstream (%d of them errors)", len(fixtures)-failures, len(fixtures), errored)
}

func TestGeneratedParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	count := 20000
	if testing.Short() {
		count = 2000
	}
	failures, errored := compareAll(test, "generated", nil, generatedInputs(count), 30)
	test.Logf("%d of %d generated inputs match upstream (%d of them errors)", count-failures, count, errored)
}

// TestOracleCanFail proves the comparison sees a difference: the port's tree for one text against
// upstream's for another, where the trees differ in a single position, value, comment slot or error.
func TestOracleCanFail(test *testing.T) {
	test.Parallel()
	cases := []struct{ mine, theirs string }{
		{"a:  b", "a: b"},         // a position
		{"a: c", "a: b"},          // a value
		{"a: b\n# c", "a: b # c"}, // a comment's slot
		{"a: [b", "a: {b"},        // an error's message
		{"- a\n- b", "- a\n- b\n"},
	}
	theirs := make([]string, len(cases))
	for index, each := range cases {
		theirs[index] = each.theirs
	}
	expected := oracleParse(test, theirs)
	for index, each := range cases {
		if differences := compareParse(each.mine, expected[index]); len(differences) == 0 {
			test.Errorf("the comparison found no difference between %q and %q", each.mine, each.theirs)
		}
	}
}

const suiteScript = `
import suite from "FORK/node_modules/yaml-test-suite/index.js";
const inputs = [];
for (const test of suite) {
  for (const testCase of test.cases) {
    if (typeof testCase.yaml === "string") inputs.push(testCase.yaml);
    if (typeof testCase.dump === "string") inputs.push(testCase.dump);
  }
}
process.stdout.write(JSON.stringify(inputs));
`

// TestYamlTestSuiteParseMatchesUpstream compares every input of the yaml-test-suite in the fork, and each
// case's canonical dump.
func TestYamlTestSuiteParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	if _, lookPathError := exec.LookPath("node"); lookPathError != nil {
		test.Skip("node is not installed")
	}
	script := filepath.Join(test.TempDir(), "suite.mjs")
	if writeError := os.WriteFile(
		script,
		[]byte(strings.ReplaceAll(suiteScript, "FORK", forkRoot(test))),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	output, outputError := exec.Command("node", script).Output()
	if outputError != nil {
		test.Skipf("the yaml-test-suite is not loadable: %v", outputError)
	}
	var texts []string
	if decodeError := json.Unmarshal(output, &texts); decodeError != nil {
		test.Fatal(decodeError)
	}
	failures, errored := compareAll(test, "suite", nil, texts, 30)
	test.Logf(
		"%d of %d yaml-test-suite inputs match upstream (%d of them errors)",
		len(texts)-failures,
		len(texts),
		errored,
	)
}

// javaScriptSpaceText is the set String.prototype.trim removes, for markdown's front matter value.
const javaScriptSpaceText = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// corpusSizeLimit skips the generated multi-megabyte files the printer's corpus test skips.
const corpusSizeLimit = 1 << 20

// TestCorpusParseMatchesUpstream compares every .yaml and .yml file under COHERE_YAML_CORPUS, and the
// YAML front matter value of every .md file there, enumerated as the printer's corpus test enumerates
// them: through the engine's ignore layers, into nested repositories. A measuring run.
func TestCorpusParseMatchesUpstream(test *testing.T) {
	test.Parallel()
	roots := needs.OptIn(
		test,
		"COHERE_YAML_CORPUS",
		"a differential of the front matter of every .md file of a corpus against upstream",
	)
	enumerator, newPrettierError := prettier.New(format_options.Default())
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}
	var names []string
	var texts []string
	yamlFiles, frontMatters, tooLarge := 0, 0, 0
	pending := strings.Split(roots, ":")
	seen := map[string]bool{}
	// Every corpus walked, logged once the loop ends, so a run that reaches fewer repositories says so.
	var measured []string
	for len(pending) > 0 {
		root := strings.TrimSpace(pending[0])
		pending = pending[1:]
		if strings.HasPrefix(root, "~/") {
			home, homeError := os.UserHomeDir()
			if homeError != nil {
				test.Fatalf("finding the home directory for %s: %v", root, homeError)
			}
			root = filepath.Join(home, root[2:])
		}
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		// Nested repositories regardless of root's own ignore lists, as the differential finds them
		// (#k6vebep): a repository under a path root never formats is still a corpus.
		nestedRepositories, nestedRepositoriesBelowError := format_files.NestedRepositoriesBelow(root)
		if nestedRepositoriesBelowError != nil {
			test.Fatalf("finding the repositories nested in %s: %v", root, nestedRepositoriesBelowError)
		}
		for _, nested := range nestedRepositories {
			pending = append(pending, filepath.Join(root, nested))
		}

		enumeration, enumerateError := enumerator.Enumerate(root)
		if errors.Is(enumerateError, format_options.ErrPrettierConfigRemains) {
			test.Logf("skipping %s, not yet adopted: %v", root, enumerateError)
			continue
		}
		if enumerateError != nil {
			test.Fatalf("enumerating %s: %v", root, enumerateError)
		}
		measured = append(measured, fmt.Sprintf("%s (%d)", root, len(enumeration.Files)))
		for _, file := range enumeration.Files {
			if !filepath.IsAbs(file) {
				file = filepath.Join(root, file)
			}
			extension := strings.ToLower(filepath.Ext(file))
			if extension != ".yaml" && extension != ".yml" && extension != ".md" {
				continue
			}
			source, readError := os.ReadFile(file)
			if readError != nil {
				test.Fatal(readError)
			}
			if len(source) > corpusSizeLimit {
				tooLarge++
				continue
			}
			// Prettier's core: no byte order mark, \n line endings.
			text := strings.TrimPrefix(string(source), "\ufeff")
			text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
			if extension == ".md" {
				frontMatter, _ := mdast.ParseFrontMatter(text)
				if frontMatter == nil || frontMatter.Language != "yaml" {
					continue
				}
				value := strings.Trim(frontMatter.Value, javaScriptSpaceText)
				if value == "" {
					continue
				}
				frontMatters++
				names = append(names, file+" (front matter)")
				texts = append(texts, value)
				continue
			}
			yamlFiles++
			names = append(names, file)
			texts = append(texts, text)
		}
	}
	test.Logf("corpora walked: %d\n  %s", len(measured), strings.Join(measured, "\n  "))
	failures, errored := compareAll(test, "corpus", names, texts, 30)
	test.Logf(
		"%d of %d inputs match upstream (%d yaml files, %d front matter values; %d of them errors); %d files over %d bytes not measured",
		len(texts)-failures,
		len(texts),
		yamlFiles,
		frontMatters,
		errored,
		tooLarge,
		corpusSizeLimit,
	)
}

// TestComparisonSeesEveryField corrupts upstream's correct tree one field at a time and checks that the
// comparison reports each corruption: every field the comparison reads, positions, identities and
// parents, and null told apart from an empty array.
func TestComparisonSeesEveryField(test *testing.T) {
	test.Parallel()
	text := "%YAML 1.2 # d\n---\n- &a !t |2- # i\n   x\n- *a # t\n- ? k\n  : v\n  # e\n# l\n- [b, {c: d}]\n"
	expected := oracleParse(test, []string{text})[0]
	if expected.Error != nil {
		test.Fatalf("the fixture does not parse: %s", expected.Error.Message)
	}
	if differences := compareParse(text, expected); len(differences) != 0 {
		test.Fatalf("the uncorrupted tree differs: %v", differences)
	}
	// find returns the first node, depth first, of the type, through the fields the comparison walks.
	var find func(node *jsonNode, nodeType string) *jsonNode
	find = func(node *jsonNode, nodeType string) *jsonNode {
		if node == nil {
			return nil
		}
		if node.Type == nodeType {
			return node
		}
		for _, nodes := range [][]*jsonNode{
			node.Children,
			{node.Tag},
			{node.Anchor},
			node.MiddleComments,
			node.LeadingComments,
			{node.IndicatorComment},
			{node.TrailingComment},
			node.EndComments,
		} {
			for _, each := range nodes {
				if found := find(each, nodeType); found != nil {
					return found
				}
			}
		}
		return nil
	}
	// Each corruption edits the tree decoded into jsonNode, the shape compareParse reads it back into, so
	// encoding it again keeps every field the comparison sees, null told apart from an empty array.
	corruptions := map[string]func(root *jsonNode){
		"start offset":  func(root *jsonNode) { find(root, "plain").Position.Start.Offset++ },
		"end line":      func(root *jsonNode) { find(root, "mapping").Position.End.Line = 99 },
		"start column":  func(root *jsonNode) { find(root, "comment").Position.Start.Column = 7 },
		"document end":  func(root *jsonNode) { find(root, "document").Position.End.Offset = 3 },
		"value":         func(root *jsonNode) { find(root, "alias").Value = new("b") },
		"tag value":     func(root *jsonNode) { find(root, "tag").Value = new("!u") },
		"anchor value":  func(root *jsonNode) { find(root, "anchor").Value = new("") },
		"comment value": func(root *jsonNode) { find(root, "comment").Value = new(" x") },
		"chomping":      func(root *jsonNode) { find(root, "blockLiteral").Chomping = new("keep") },
		"indent":        func(root *jsonNode) { find(root, "blockLiteral").Indent = nil },
		"type":          func(root *jsonNode) { find(root, "blockLiteral").Type = "blockFolded" },
		"marker":        func(root *jsonNode) { find(root, "document").DirectivesEndMarker = new(false) },
		"end marker":    func(root *jsonNode) { find(root, "document").DocumentEndMarker = new(true) },
		"name":          func(root *jsonNode) { find(root, "directive").Name = new("TAG") },
		"parameters":    func(root *jsonNode) { find(root, "directive").Parameters = []string{} },
		"parent":        func(root *jsonNode) { find(root, "plain").Parent = 0 },
		"identity":      func(root *jsonNode) { find(root, "comment").Id = 9999 },
		"keys":          func(root *jsonNode) { find(root, "plain").Keys = []string{"type"} },
		"null for []":   func(root *jsonNode) { find(root, "mappingValue").LeadingComments = nil },
		"no tag":        func(root *jsonNode) { find(root, "blockLiteral").Tag = nil },
		"no indicator":  func(root *jsonNode) { find(root, "blockLiteral").IndicatorComment = nil },
		"trailing moved": func(root *jsonNode) {
			alias := find(root, "alias")
			alias.LeadingComments = []*jsonNode{alias.TrailingComment}
			alias.TrailingComment = nil
		},
		"end comment": func(root *jsonNode) {
			var corrupt func(node *jsonNode) bool
			corrupt = func(node *jsonNode) bool {
				if node == nil {
					return false
				}
				if len(node.EndComments) > 0 {
					node.EndComments = []*jsonNode{}
					return true
				}
				for _, child := range node.Children {
					if corrupt(child) {
						return true
					}
				}
				return corrupt(node.Tag) || corrupt(node.Anchor)
			}
			if !corrupt(root) {
				test.Error("the fixture has no end comment to corrupt")
			}
		},
		"root comments": func(root *jsonNode) { root.Comments = root.Comments[1:] },
		"child dropped": func(root *jsonNode) {
			flow := find(root, "flowSequence")
			flow.Children = flow.Children[:1]
		},
		"middle comments": func(root *jsonNode) {
			find(root, "plain").MiddleComments = []*jsonNode{find(root, "comment")}
		},
	}
	// The round trip alone must still read as upstream's tree, or every corruption would differ for it.
	untouched := &jsonNode{}
	if decodeError := json.Unmarshal(expected.Root, untouched); decodeError != nil {
		test.Fatal(decodeError)
	}
	reencoded, reencodeError := json.Marshal(untouched)
	if reencodeError != nil {
		test.Fatal(reencodeError)
	}
	if differences := compareParse(text, oracleResult{Root: reencoded}); len(differences) != 0 {
		test.Fatalf("the tree differs once decoded and encoded again: %v", differences)
	}
	for name, corrupt := range corruptions {
		root := &jsonNode{}
		if decodeError := json.Unmarshal(expected.Root, root); decodeError != nil {
			test.Fatal(decodeError)
		}
		corrupt(root)
		encoded, encodeError := json.Marshal(root)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		if differences := compareParse(text, oracleResult{Root: encoded}); len(differences) == 0 {
			test.Errorf("corrupting the %s went unnoticed", name)
		}
	}
	// And the errors: a different message, name or position.
	errorText := "a: [b"
	expectedError := oracleParse(test, []string{errorText})[0]
	for name, corrupt := range map[string]func(*jsonError){
		"message":  func(jsonError *jsonError) { jsonError.Message += "." },
		"name":     func(jsonError *jsonError) { jsonError.Name = "TypeError" },
		"position": func(jsonError *jsonError) { jsonError.Position.End.Offset++ },
	} {
		copied := *expectedError.Error
		position := *copied.Position
		copied.Position = &position
		corrupt(&copied)
		if differences := compareParse(errorText, oracleResult{Error: &copied}); len(differences) == 0 {
			test.Errorf("corrupting the error's %s went unnoticed", name)
		}
	}
}
