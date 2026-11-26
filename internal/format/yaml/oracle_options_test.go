package yaml

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/format/printing"
)

// The oracle for what prettier.Formatter cannot be asked: proseWrap, which format_options.Options does not carry
// and every repository leaves at "preserve", and a file path the formatter does not route (.prettierrc). These
// run the same embedded bundles in their own runtime, with the option given.

// Its answers are recorded (see oracle_testing), so the bundles run only when they are recorded again.
type optionsOracle struct {
	golden *oracle_testing.Golden
}

func newOptionsOracle(test *testing.T) *optionsOracle {
	test.Helper()
	return &optionsOracle{golden: oracle_testing.Open(test, test.Name())}
}

// optionsScript formats __input's source at its file path with its parser and options.
const optionsScript = `(() => {
	const input = JSON.parse(__input);
	return prettier.format(input.source, {
		filepath: input.filePath, parser: input.parser, plugins: prettierPlugins, proseWrap: input.proseWrap,
		printWidth: input.printWidth, tabWidth: input.tabWidth, useTabs: input.useTabs, singleQuote: input.singleQuote,
		bracketSpacing: input.bracketSpacing, trailingComma: input.trailingComma, endOfLine: input.endOfLine,
	});
})()`

func (oracle *optionsOracle) format(
	text string,
	filePath string,
	parser string,
	options format_options.Options,
	proseWrap string,
) (string, error) {
	key := oracle_testing.Key("options", text, filePath, parser, fmt.Sprintf("%+v", options), proseWrap)
	input, marshalError := json.Marshal(struct {
		Source         string `json:"source"`
		FilePath       string `json:"filePath"`
		Parser         string `json:"parser"`
		ProseWrap      string `json:"proseWrap"`
		PrintWidth     int    `json:"printWidth"`
		TabWidth       int    `json:"tabWidth"`
		UseTabs        bool   `json:"useTabs"`
		SingleQuote    bool   `json:"singleQuote"`
		BracketSpacing bool   `json:"bracketSpacing"`
		TrailingComma  string `json:"trailingComma"`
		EndOfLine      string `json:"endOfLine"`
	}{
		text,
		filePath,
		parser,
		proseWrap,
		options.PrintWidth,
		options.TabWidth,
		options.UseTabs,
		options.SingleQuote,
		options.BracketSpacing,
		options.TrailingComma,
		options.EndOfLine,
	})
	if marshalError != nil {
		return "", fmt.Errorf("encoding the oracle's input: %w", marshalError)
	}
	return oracle.golden.Evaluate(key, optionsScript, string(input))
}

// proseWrapFixtures are the inputs whose words the always and never branches join and split: multi-line
// plain and quoted scalars, folded blocks, trailing backslashes, and long lines at a narrow width.
var proseWrapFixtures = []string{
	"a: b\n  c\n\n  d",
	"a: \"b\n  c\"",
	"a: 'b\n\n  c'",
	"a: \"b \\\n  c\"",
	"a: \"b\\\n  c\"",
	"a: \"b c \\\n  d\\\n  e\"",
	"a: >\n  x\n  y\n\n  z\n",
	"a: >\n  x\n    y\n  z\n",
	"a: >\n  x  \n  y\n",
	"a: >\n   x\n  y\n",
	"a: >-\n  x y  z\n",
	"a: >\n  word word word word word word word word word word word word word word word word word word\n",
	"a: word word word word word word word word word word word word word word word word word word word",
	"a: 'word word word word word word word word word word word word word word word word word word word'",
	"? word word word word word word word word word word word word word word word word\n: b",
	"word word word word word word word: word word word word word word word word word word",
	"a: b c\nd: \"e\n  f\"",
	"- a b\n  c d\n- \"e\n  f\"",
	"a: |\n  x y\n  z\n",
	"a: b  c\n  d",
	"a: \"b\\\n\n  c\"",
	"a: >\n  x\n\n\n  y\n\nb: c",
	"key: 'it''s a long line of single quoted text that wraps'",
	"a: word  word  word  word  word  word  word  word  word  word  word  word  word  word  word",
	"a: 'word  word  word  word  word  word  word  word  word  word  word  word  word  word  word'",
	"a: >\n  word  word  word  word  word  word  word  word  word  word  word  word  word  word\n",
	"a: >\n  \u00a0x\n  y\n",
	"a: >\n  x\u00a0\n  y\n",
	"a: plain\u00a0 b\n  c",
}

// unsettledProseWrapFixtures are unsettledFixtures and the inputs whose fork output only a proseWrap other than
// preserve leaves unsettled, held to the fork's recorded second pass as unsettledFixtures are.
var unsettledProseWrapFixtures = func() map[string]bool {
	union := maps.Clone(unsettledFixtures)
	// Under proseWrap never, the trailing lines a `>+` scalar keeps print once more, so each pass adds one.
	union["a: >+\n  x\n\n"] = true
	union["a:\n  - |-\n    x\n  - >+\n    y\n\nb: c"] = true
	return union
}()

func TestProseWrapMatchesOracle(test *testing.T) {
	test.Parallel()
	oracle := newOptionsOracle(test)
	forkAgain := &optionsOracle{golden: oracle_testing.Open(test, test.Name()+"SecondPass")}
	inputs := append(append([]string{}, proseWrapFixtures...), formatFixtures...)
	failures, compared := 0, 0
	unsettled := map[string]bool{}
	for _, proseWrap := range []string{"always", "never"} {
		for variantIndex, options := range formatVariants[:3] {
			for _, input := range inputs {
				expected, formatError := oracle.format(input, "fixture.yaml", "yaml", options, proseWrap)
				if formatError != nil {
					continue
				}
				compared++
				fullStack, formatFullStackError := formatFullStack("fixture.yaml", input, options, proseWrap, nil)
				if formatFullStackError != nil {
					failures++
					test.Errorf("%s, variant %d, %q: Format: %v", proseWrap, variantIndex, input, formatFullStackError)
				} else if fullStack != expected {
					failures++
					test.Errorf(
						"%s, variant %d, %q: Format: %s",
						proseWrap,
						variantIndex,
						input,
						differential.FirstDifference(expected, fullStack),
					)
				} else if wrong, unsettledAsListed := secondPass(
					unsettledProseWrapFixtures,
					input,
					expected,
					options,
					proseWrap,
					func() (string, error) {
						return forkAgain.format(expected, "fixture.yaml", "yaml", options, proseWrap)
					},
				); wrong != "" {
					failures++
					test.Errorf("%s, variant %d, %q: %s", proseWrap, variantIndex, input, wrong)
				} else if unsettledAsListed {
					unsettled[input] = true
				}
			}
		}
	}
	requireUnsettled(test, unsettledProseWrapFixtures, inputs, unsettled)
	test.Logf("%d proseWrap comparisons: %d differ", compared, failures)
}

// jsonTextToDoc stands in for the native JSON printer: the oracle formats the JSON, and the text comes
// back as a doc with its trailing newline stripped, as textToDoc strips the trailing hardline.
func jsonTextToDoc(test *testing.T, oracle *optionsOracle, options format_options.Options) printing.TextToDoc {
	return func(text string, parser string) (doc.Doc, error) {
		if parser != "json" {
			test.Errorf("the YAML printer asked for parser %q", parser)
		}
		formatted, formatError := oracle.format(text, "embedded.json", "json", options, "preserve")
		if formatError != nil {
			return nil, formatError
		}
		return doc.Text(strings.TrimSuffix(formatted, "\n")), nil
	}
}

// TestPrettierRcEmbedsJson covers embed.js: a .prettierrc, .stylelintrc or .lintstagedrc that is JSON
// prints as JSON, and one that is YAML falls back to the YAML printer; any other name never embeds.
func TestPrettierRcEmbedsJson(test *testing.T) {
	test.Parallel()
	oracle := newOptionsOracle(test)
	inputs := []string{
		`{"semi":false,"singleQuote":true}`, "{\n  \"a\": [1,2,3]\n}\n", "semi: false\nsingleQuote:   true\n",
		"# c\nsemi: false", "[1,  2]", "{a: 1}", "", "\"a\"",
	}
	fileNames := []string{
		".prettierrc",
		"a/.stylelintrc",
		"a\\.lintstagedrc",
		"prettierrc",
		".prettierrc.yaml",
		"x.yaml",
	}
	for _, options := range formatVariants[:2] {
		textToDoc := jsonTextToDoc(test, oracle, options)
		for _, fileName := range fileNames {
			for _, input := range inputs {
				expected, formatError := oracle.format(input, fileName, "yaml", options, "preserve")
				if formatError != nil {
					if _, fullStackError := formatFullStack(
						fileName,
						input,
						options,
						"preserve",
						textToDoc,
					); !printing.IsSyntax(fullStackError) {
						test.Errorf(
							"%s, %q: the oracle failed (%v) but Format gave %v",
							fileName,
							input,
							formatError,
							fullStackError,
						)
					}
					continue
				}
				if fullStack, formatFullStackError := formatFullStack(
					fileName,
					input,
					options,
					"preserve",
					textToDoc,
				); formatFullStackError != nil {
					test.Errorf("%s, %q: Format: %v", fileName, input, formatFullStackError)
				} else if fullStack != expected {
					test.Errorf(
						"%s, %q: Format: %s",
						fileName,
						input,
						differential.FirstDifference(expected, fullStack),
					)
				}
			}
		}
	}
}

// TestPrettierRcWithoutTextToDocPrintsYaml: with no textToDoc the embed fails, and the core prints the
// file as YAML, as upstream does when the JSON format throws.
func TestPrettierRcWithoutTextToDocPrintsYaml(test *testing.T) {
	test.Parallel()
	text := `{"a":1}`
	actual, printFileError := PrintFile(".prettierrc", parseFixture(test, text), text, format_options.Default(), nil)
	if printFileError != nil {
		test.Fatal(printFileError)
	}
	if actual != "{ 'a': 1 }\n" {
		test.Fatalf("got %q", actual)
	}
}

func docString(document doc.Doc) string {
	return doc.Print(document, doc.Options{PrintWidth: 120, TabWidth: 4})
}

func stripTrailingHardline(document doc.Doc) doc.Doc { return doc.StripTrailingHardline(document) }
