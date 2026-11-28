package yaml

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// formatParsed prints a parsed input the way Prettier's core wraps the printer: the byte order mark comes
// back on the output.
func formatParsed(
	fileName string,
	tree parsed,
	text string,
	hasByteOrderMark bool,
	options format_options.Options,
	proseWrap string,
	textToDoc printing.TextToDoc,
) (string, error) {
	if tree.parseFailure != "" {
		return "", fmt.Errorf("the parser failed: %s", tree.parseFailure)
	}
	if tree.difference != "" {
		return "", errors.New("the Go parser's tree is not yaml-unist-parser's, so there is no tree to load")
	}
	formatted, printFileError := printFile(fileName, tree.root, text, options, proseWrap, textToDoc, 0)
	if printFileError != nil {
		return "", printFileError
	}
	if hasByteOrderMark {
		formatted = "\ufeff" + formatted
	}
	return formatted, nil
}

// frontMatterBlock is the front matter block of a formatted markdown file: the first line through the
// first later line that is the end delimiter ("---", or "..." as pandoc allows).
func frontMatterBlock(formatted string, endDelimiter string) string {
	lines := strings.SplitAfter(formatted, "\n")
	length := 0
	for index, line := range lines {
		length += len(line)
		if index > 0 && strings.TrimRight(line, "\n") == endDelimiter {
			return formatted[:length]
		}
	}
	return formatted
}

// embedFrontMatter embeds the port's doc for a front matter value the way markdown's front matter embed
// does (src/main/front-matter/embed.js): the value printed with parser yaml through textToDoc, which strips
// the trailing hardline, between the delimiters. The doc is the Go parser's tree printed (FormatDoc) when
// fullStack, else the loaded tree's.
func embedFrontMatter(
	frontMatter *mdast.FrontMatter,
	value string,
	tree parsed,
	fullStack bool,
	options format_options.Options,
) (string, error) {
	var embedded doc.Doc
	var formatError error
	if fullStack {
		embedded, formatError = FormatDoc(value, options, nil)
	} else {
		if tree.parseFailure != "" {
			return "", fmt.Errorf("the parser failed: %s", tree.parseFailure)
		}
		if tree.difference != "" {
			return "", errors.New("the Go parser's tree is not yaml-unist-parser's, so there is no tree to load")
		}
		embedded, formatError = PrintDoc(tree.root, value, options, nil)
	}
	if formatError != nil {
		return "", formatError
	}
	explicitLanguage := ""
	if frontMatter.ExplicitLanguage != nil {
		explicitLanguage = *frontMatter.ExplicitLanguage
	}
	printed := doc.Print(doc.MarkAsRoot(doc.Concat{
		doc.Text(frontMatter.StartDelimiter),
		doc.Text(explicitLanguage),
		doc.Hardline,
		doc.StripTrailingHardline(embedded),
		doc.Hardline,
		doc.Text(frontMatter.EndDelimiter),
	}), doc.Options{
		PrintWidth: options.PrintWidth,
		TabWidth:   options.TabWidth,
		UseTabs:    options.UseTabs,
	})
	// The markdown printer follows the front matter with a newline whatever comes after it.
	return printed + "\n", nil
}

// TestCorpusFormatMatchesOracle is the printer's acceptance test over the corpus: Prettier's own YAML
// format fixtures but its spec examples, which the yaml-test-suite holds, and the YAML front matter of its
// markdown fixtures, recorded from the fork under testdata/prettier_tests. Each is formatted under the
// default options, as a repository with no config of its own is, by the port two ways: the full stack
// (Format: the Go parser and the printer, from the text) and the printer alone on the tree
// yaml-unist-parser built (the tree loader: the Go parser's tree, loaded as the recording carried one, once
// it has the fingerprint recorded for upstream's). Both are compared with what the
// embedded Prettier printed, recorded in a golden (see oracle_testing). A front matter value is embedded
// as markdown's front matter embed does and compared with the front matter block of Prettier's markdown
// output. Where Prettier fails, both parsers must refuse the file too.
//
// The deleted test measured the corpora under COHERE_YAML_CORPUS, other repositories, and only logged
// its tallies; this one reads only this repository, and every difference fails it.
func TestCorpusFormatMatchesOracle(test *testing.T) {
	test.Parallel()
	options := format_options.Default()
	formatter := oracle_testing.Open(test, test.Name()).Formatter(options)
	compared, oracleFailures, syntaxErrors, failures := 0, 0, 0, 0
	fail := func(format string, arguments ...any) {
		failures++
		test.Errorf(format, arguments...)
	}
	compare := func(label string, name string, expected string, actual string, actualError error) {
		switch {
		case actualError != nil:
			fail("%s, %s: %v", label, name, actualError)
		case actual != expected:
			fail("%s, %s: %s", label, name, differential.FirstDifference(expected, actual))
		}
	}

	// The YAML files.
	halves, partsError := recorded.Parts(corpusDirectory, "yaml_inputs_")
	if partsError != nil {
		test.Fatal(partsError)
	}
	answersHalves, answersPartsError := recorded.Parts(yamlUnistParserDirectory, "corpus_answers_")
	if answersPartsError != nil {
		test.Fatal(answersPartsError)
	}
	if len(answersHalves) != len(halves) {
		test.Fatalf("%d yaml_inputs_ parts are answered by %d corpus_answers_ parts", len(halves), len(answersHalves))
	}
	for half, path := range halves {
		inputs, entries := readInputs(test, path)
		trees := recordedTrees(test, answersHalves[half], inputs, entries)
		for index, input := range inputs {
			expected, oracleError := formatter.Format(input.Name, input.Text)
			fullStack, fullStackError := formatFullStack(input.Name, input.Text, options, "preserve", nil)
			if oracleError != nil {
				// Prettier cannot format it; both parsers must refuse it too.
				oracleFailures++
				if trees[index].parseFailure == "" {
					fail("%s: the oracle failed (%v) but yaml-unist-parser did not", input.Name, oracleError)
				}
				if fullStackError == nil || !printing.IsSyntax(fullStackError) {
					fail(
						"%s: the oracle failed (%v) but Format gave %q, %v",
						input.Name,
						oracleError,
						fullStack,
						fullStackError,
					)
				}
				continue
			}
			compared++
			compare("yaml files, full stack", input.Name, expected, fullStack, fullStackError)
			// The corpus's texts are normalized already, so there is no byte order mark to restore.
			loaded, loadedError := formatParsed(input.Name, trees[index], input.Text, false, options, "preserve", nil)
			compare("yaml files, tree loader", input.Name, expected, loaded, loadedError)
		}
	}

	// The front matter of the markdown files, each value's tree from the loader's recording.
	// Each value's tree is matched by its digest (loaderInputs), so mdast reads the value Prettier's front
	// matter parser read, or the embed is fed another text and the match fails.
	loaderTexts, loaderEntries := loaderInputs(test)
	trees := map[string]parsed{}
	loaderAnswers := filepath.Join(yamlUnistParserDirectory, "loader_answers.json")
	for index, tree := range recordedTrees(test, loaderAnswers, loaderTexts, loaderEntries) {
		trees[loaderTexts[index].Name] = tree
	}
	for _, each := range readFrontMatter(test) {
		input, frontMatter := each.file, each.frontMatter
		tree, answered := trees[input.Name+" (front matter)"]
		if !answered {
			test.Fatalf("loader_answers.json has no tree for %s's front matter", input.Name)
		}
		// The Go parser must refuse exactly the values the real one refuses.
		if _, parseError := parse(each.value, nil); (parseError != nil) != (tree.parseFailure != "") {
			fail(
				"front matter, %s: the Go parser disagrees (real: %q, Go: %v)",
				input.Name,
				tree.parseFailure,
				parseError,
			)
		}
		if tree.parseFailure != "" {
			// Prettier prints such front matter as written, since the embed fails; there is no YAML output
			// to compare.
			syntaxErrors++
			continue
		}
		formatted, oracleError := formatter.Format(input.Name, input.Text)
		if oracleError != nil {
			fail("front matter, %s: the oracle failed: %v", input.Name, oracleError)
			continue
		}
		expected := frontMatterBlock(formatted, frontMatter.EndDelimiter)
		compared++
		fullStack, fullStackError := embedFrontMatter(frontMatter, each.value, tree, true, options)
		compare("front matter, full stack", input.Name, expected, fullStack, fullStackError)
		loaded, loadedError := embedFrontMatter(frontMatter, each.value, tree, false, options)
		compare("front matter, tree loader", input.Name, expected, loaded, loadedError)
	}
	test.Logf(
		"%d files and front matters compared two ways: %d differences; %d oracle failures (syntax errors), %d front matters not measured (a YAML syntax error)",
		compared,
		failures,
		oracleFailures,
		syntaxErrors,
	)
}
