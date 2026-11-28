package native

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// The perturbed markdown documents. Markdown that is already formatted tests preservation far more than
// printing, so each of the markdown package's documents is also rewritten into something Prettier has to
// change, and both are formatted under three option sets, the native printers against the embedded
// Prettier's recorded answers. The documents hold embedded code, so the comparison reaches the embedded
// languages' printers through markdown's embed as well.

type markdownPerturbation struct {
	pattern     *regexp.Regexp
	replacement string
	rows        bool
}

var markdownPerturbations = []markdownPerturbation{
	// Bullets Prettier rewrites to `-`, or alternates by sibling list.
	{pattern: regexp.MustCompile(`(?m)^(\s*)- `), replacement: "$1* "},
	// Ordered lists all numbered one, which Prettier renumbers or keeps by its own rule.
	{pattern: regexp.MustCompile(`(?m)^(\s*)\d+\. `), replacement: "${1}1. "},
	// Strong with underscores, which Prettier prints with asterisks.
	{pattern: regexp.MustCompile(`\*\*([^*\s][^*]*[^*\s])\*\*`), replacement: "__${1}__"},
	// Emphasis with asterisks, which Prettier prints with underscores where it may.
	{pattern: regexp.MustCompile(`(^|[\s(])_([^_\s][^_]*[^_\s])_([\s).,]|$)`), replacement: "$1*$2*$3"},
	// Table rows with their padding removed, which Prettier realigns.
	{pattern: regexp.MustCompile(`(?m)^\|.*\|$`), rows: true},
	// Closing sequences on headings, which Prettier drops.
	{pattern: regexp.MustCompile(`(?m)^(#{1,6}) (.+)$`), replacement: "$1   $2 $1"},
	// Tilde fences, which Prettier prints with backticks.
	{pattern: regexp.MustCompile("(?m)^```"), replacement: "~~~"},
}

var tableCellPadding = regexp.MustCompile(`[ \t]*\|[ \t]*`)

func perturbMarkdown(text string) string {
	perturbed := text
	for _, perturbation := range markdownPerturbations {
		if perturbation.rows {
			perturbed = perturbation.pattern.ReplaceAllStringFunc(perturbed, func(row string) string {
				return tableCellPadding.ReplaceAllString(row, "|")
			})
			continue
		}
		perturbed = perturbation.pattern.ReplaceAllString(perturbed, perturbation.replacement)
	}
	// Trailing spaces on every third line, which Prettier strips outside hard breaks.
	lines := strings.Split(perturbed, "\n")
	for index := range lines {
		if index%3 == 2 && strings.TrimSpace(lines[index]) != "" {
			lines[index] += " "
		}
	}
	return strings.Join(lines, "\n")
}

// markdownDocumentsDirectory is the markdown package's documents (see its documents_test.go): whole files
// built around tables, nested lists and embedded code.
var markdownDocumentsDirectory = filepath.Join("..", "markdown", "testdata", "documents")

// TestPerturbedDocumentsMatchOracle formats every document, as written and perturbed, with Formatter and
// compares it with the embedded Prettier's recorded answer. It replaces TestPerturbedMarkdownCorpus, which
// perturbed the markdown of whatever repositories a variable named and formatted it with the live oracle. That
// run attributed a miss to a missing printer when markdown matched with the oracle formatting the embeds; the
// documents' embeds are all in languages with a native printer, so every miss here is a miss.
func TestPerturbedDocumentsMatchOracle(test *testing.T) {
	test.Parallel()
	paths, globError := filepath.Glob(filepath.Join(markdownDocumentsDirectory, "*.md"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(paths) < 6 {
		test.Fatalf(
			"%s holds %d documents, fewer than the six it was written with",
			markdownDocumentsDirectory,
			len(paths),
		)
	}
	defaults := format_options.Default()
	narrow := defaults
	narrow.PrintWidth, narrow.TabWidth = 40, 2
	tabs := defaults
	tabs.UseTabs, tabs.SingleQuote = true, !tabs.SingleQuote
	// The oracle's answers, recorded (see oracle_testing), so no JavaScript runs here.
	golden := oracle_testing.Open(test, test.Name())
	perturbedAny := false
	for _, path := range paths {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		name := filepath.Base(path)
		perturbed := perturbMarkdown(string(contents))
		perturbedAny = perturbedAny || perturbed != string(contents)
		inputs := []struct{ label, text string }{{"as written", string(contents)}, {"perturbed", perturbed}}
		for _, input := range inputs {
			for _, options := range []format_options.Options{defaults, narrow, tabs} {
				expected, oracleError := golden.Formatter(options).Format(name, input.text)
				if oracleError != nil {
					test.Fatalf("the oracle failed on %s %s: %v", name, input.label, oracleError)
				}
				actual, formatError := (Formatter{Options: options}).Format(name, input.text)
				if formatError != nil {
					test.Errorf("%s %s (printWidth %d, useTabs %v): %v", name, input.label, options.PrintWidth,
						options.UseTabs, formatError)
					continue
				}
				if actual != expected {
					test.Errorf("%s %s (printWidth %d, useTabs %v): %s", name, input.label, options.PrintWidth,
						options.UseTabs, differential.FirstDifference(expected, actual))
				}
			}
		}
	}
	// A perturbation that changes nothing compares each document with itself twice.
	if !perturbedAny {
		test.Fatal("perturbMarkdown changed none of the documents")
	}
}
