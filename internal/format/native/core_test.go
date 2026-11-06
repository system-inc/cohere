package native

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// TestByteOrderMarksAndCarriageReturnsMatchTheForkInEveryLanguage: main/core.js strips a byte order
// mark and turns carriage returns into newlines before any printer runs, and Formatter does the same
// once for all of them, so no printer has to. Measured with the normalization removed: markdown and
// TypeScript print a carriage-return file differently from the fork, while the others happen to
// tolerate it, which is luck a new printer should not have to rely on.
func TestByteOrderMarksAndCarriageReturnsMatchTheForkInEveryLanguage(test *testing.T) {
	test.Parallel()
	sources := map[string]string{
		"probe.ts":      "const a = { b: 1 }\n// comment\nconst c = `line\nline`\n",
		"probe.tsx":     "const view = <div className='x'>{a}</div>\n",
		"probe.js":      "export function make (name) { return { 'a': 1 } }\n",
		"probe.json":    "{ \"a\": [1, 2],\n\"b\": {} }\n",
		"package.json":  "{\"name\":\"probe\",\"files\":[]}\n",
		"probe.md":      "# Title\n\nSome *text*\nacross lines.\n\n- a\n- b\n",
		"probe.css":     "a{color:red;\nmargin:0}\n/* comment */\n",
		"probe.graphql": "query Q { a\n b(c: 1) }\n",
		"probe.yaml":    "a:   1\n# comment\nb: [ x,y ]\nc: |\n  line\n  line\n",
		"probe.yml":     "- a\n-   b: 'c'\n",
	}
	variants := map[string]func(string) string{
		"byte order mark":   func(text string) string { return "\ufeff" + text },
		"crlf":              func(text string) string { return strings.ReplaceAll(text, "\n", "\r\n") },
		"cr":                func(text string) string { return strings.ReplaceAll(text, "\n", "\r") },
		"both, crlf + mark": func(text string) string { return "\ufeff" + strings.ReplaceAll(text, "\n", "\r\n") },
	}

	options := format_options.Default()
	oracle, newPrettierError := prettier.New(options)
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}
	formatter := Formatter{Options: options}
	compared := 0
	for fileName, source := range sources {
		if !formatter.Handles(fileName) {
			test.Fatalf("%s has no native printer, so this test would pass over it", fileName)
		}
		for variantName, variant := range variants {
			input := variant(source)
			expected, formatError := oracle.Format(fileName, input)
			if formatError != nil {
				test.Fatalf("the oracle failed on %s, %s: %v", fileName, variantName, formatError)
			}
			actual, formatError := formatter.Format(fileName, input)
			if formatError != nil {
				test.Errorf("%s, %s: %v", fileName, variantName, formatError)
				continue
			}
			if actual != expected {
				test.Errorf("%s, %s: %s", fileName, variantName, differential.FirstDifference(expected, actual))
			}
			compared++
		}
	}
	if compared != len(sources)*len(variants) {
		test.Fatalf("compared %d of %d", compared, len(sources)*len(variants))
	}
}
