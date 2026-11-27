package markdown

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// documentsDirectory holds whole markdown documents, each built around the constructs the corpus harness
// reported most (#vs0zx54): tables, nested lists and embedded code, with front matter, footnotes, CJK prose
// and ignore ranges beside them. The fixtures in format_test.go are one construct each; these put several
// in one document, as a real file does. mdast's, micromark's and native's tests read the same files.
var documentsDirectory = filepath.Join("testdata", "documents")

// dependsOnEmbed reports whether Prettier formats part of the file as another language. Those parts are
// native's printers' to format, which this package cannot import, so its comparisons leave such inputs out
// and native's tests compare them.
func dependsOnEmbed(text string) bool { return len(EmbeddedParsers(text)) > 0 }

// readDocuments is every .md file in documentsDirectory by name, sorted, so a failure names the file.
func readDocuments(test *testing.T) map[string]string {
	test.Helper()
	paths, globError := filepath.Glob(filepath.Join(documentsDirectory, "*.md"))
	if globError != nil {
		test.Fatal(globError)
	}
	documents := map[string]string{}
	for _, path := range paths {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		documents[filepath.Base(path)] = string(contents)
	}
	if len(documents) < 6 {
		test.Fatalf("%s holds %d documents, fewer than the six it was written with", documentsDirectory, len(documents))
	}
	return documents
}

// TestDocumentsMatchOracle formats every document under formatVariants and compares it with the embedded
// Prettier's recorded answer. It replaces TestCorpusFormatMatchesOracle, which measured the same printer over
// the markdown of whatever repositories a variable named; these documents hold the constructs that run
// reported, in the repository, so every default run checks them. A document with embedded code is compared
// by native's TestPerturbedDocumentsMatchOracle instead, where the embedded languages' printers are.
func TestDocumentsMatchOracle(test *testing.T) {
	test.Parallel()
	documents := readDocuments(test)
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)
	// The oracle's answers, recorded (see oracle_testing), so no JavaScript runs here.
	golden := oracle_testing.Open(test, test.Name())
	var compared []string
	for _, name := range names {
		text := documents[name]
		if dependsOnEmbed(text) {
			continue
		}
		compared = append(compared, name)
		for variantIndex, options := range formatVariants {
			expected, oracleError := golden.Formatter(options).Format(name, text)
			if oracleError != nil {
				test.Fatalf("the oracle failed on %s: %v", name, oracleError)
			}
			actual, formatError := Format(text, options, nil)
			if formatError != nil {
				test.Errorf("variant %d, %s: %v", variantIndex, name, formatError)
				continue
			}
			if actual != expected {
				test.Errorf("variant %d, %s: %s", variantIndex, name, differential.FirstDifference(expected, actual))
			}
		}
	}
	// Most documents hold no embed, so a run that compares fewer has lost them, not passed them.
	if len(compared) < 4 {
		test.Fatalf("only %d of %d documents hold no embedded code", len(compared), len(names))
	}
	test.Logf("compared under %d option sets: %s", len(formatVariants), strings.Join(compared, ", "))
}
