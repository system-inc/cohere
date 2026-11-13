package native

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/markdown"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/needs"
)

// The perturbed markdown corpus. Our markdown is almost all already formatted, so the harness tests
// preservation far more than printing. This rewrites each file into something Prettier has to change
// and formats it under three option sets, the native printers against the embedded Prettier.
//
// Every miss must be attributed, or the test fails. A miss is attributed to a missing printer when the
// markdown printer matches once the embedded parts no native printer handles are formatted by the
// oracle instead: the difference is then in those fenced blocks or front matter, and the report names
// their languages. Anything else is markdown's own, or a native printer's that differs.
//
//	COHERE_MARKDOWN_PERTURB_CORPORA   colon-separated repository roots
//	COHERE_MARKDOWN_PERTURB_REPORT    optional file to write every miss to

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

// markdownCorpus is one repository's markdown files and the options its config resolves to.
type markdownCorpus struct {
	root    string
	files   []string
	options format_options.Options
}

// markdownCorpora enumerates the .md files of each root the way TestCorpora does: through the engine's
// ignore layers, and into each nested repository as a corpus of its own, with its own config.
func markdownCorpora(test *testing.T, roots string) []markdownCorpus {
	test.Helper()
	enumerator, newPrettierError := prettier.New(format_options.Default())
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}
	var corpora []markdownCorpus
	pending := strings.Split(roots, ":")
	seen := map[string]bool{}
	for len(pending) > 0 {
		root := strings.TrimSpace(pending[0])
		pending = pending[1:]
		if strings.HasPrefix(root, "~/") {
			home, homeError := os.UserHomeDir()
			if homeError != nil {
				test.Fatalf("finding the home directory to expand %s: %v", root, homeError)
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
		var files []string
		for _, file := range enumeration.Files {
			if strings.EqualFold(filepath.Ext(file), ".md") {
				if !filepath.IsAbs(file) {
					file = filepath.Join(root, file)
				}
				files = append(files, file)
			}
		}
		resolution, resolveError := format_options.Resolve(root)
		if errors.Is(resolveError, format_options.ErrPrettierConfigRemains) {
			// A repository still carrying Prettier config, pinned to its own adoption (www-ahra-ai). The
			// differential's TestCorpora refuses this for a named corpus; here it is named and skipped.
			test.Logf("skipping %s, not yet adopted: %v", root, resolveError)
			continue
		}
		if resolveError != nil {
			test.Fatalf("resolving the format options for %s: %v", root, resolveError)
		}
		corpora = append(corpora, markdownCorpus{root: root, files: files, options: resolution.Options})
	}
	// Every corpus measured, in one line, so a run that reaches fewer repositories says so.
	var measured []string
	for _, corpus := range corpora {
		measured = append(measured, fmt.Sprintf("%s (%d .md)", corpus.root, len(corpus.files)))
	}
	test.Logf("markdown corpora: %d\n  %s", len(corpora), strings.Join(measured, "\n  "))
	return corpora
}

// standInEmbeds formats each embedded part with its native printer where one exists, and with the
// embedded Prettier where none does, recording which parsers fell back. That is the attribution: if
// markdown then matches, the miss belongs to those languages' missing printers. A native printer is kept
// rather than replaced because it knows its host, as the oracle run standalone does not (a lone JSX
// element in a tsx fence keeps no semicolon only when the printer knows its parent is markdown).
//
// Mutates: fellBack gains each parser the oracle stood in for, which the caller reads after the format
// to name the languages a miss belongs to.
func standInEmbeds(
	options format_options.Options,
	engine *prettier.Engine,
	fellBack map[string]bool,
) printing.TextToDoc {
	native := TextToDoc(options, "markdown", nil)
	return func(text string, parserOrFile string) (doc.Doc, error) {
		if printed, nativeError := native(text, parserOrFile); nativeError == nil {
			return printed, nil
		}
		fileName := parserOrFile
		if !strings.Contains(parserOrFile, ".") {
			fileName = embeddedFileNames[parserOrFile]
		}
		if fileName == "" {
			return nil, fmt.Errorf("no file name for parser %s", parserOrFile)
		}
		formatted, formatError := engine.Format(fileName, text)
		if formatError != nil {
			return nil, formatError
		}
		fellBack[parserOrFile] = true
		return doc.Text(strings.TrimSuffix(formatted, "\n")), nil
	}
}

func TestPerturbedMarkdownCorpus(test *testing.T) {
	test.Parallel()
	roots := needs.OptIn(
		test,
		"COHERE_MARKDOWN_PERTURB_CORPORA",
		"a measurement of the formatter against the oracle over a perturbed corpus",
	)

	type job struct {
		path    string
		options format_options.Options
	}
	type outcome struct {
		job
		identical, oracleFailed bool
		attribution, detail     string
	}

	var jobs []job
	for _, corpus := range markdownCorpora(test, roots) {
		narrow := corpus.options
		narrow.PrintWidth, narrow.TabWidth = 40, 2
		tabs := corpus.options
		tabs.UseTabs, tabs.SingleQuote = true, !tabs.SingleQuote
		for _, options := range []format_options.Options{corpus.options, narrow, tabs} {
			for _, path := range corpus.files {
				jobs = append(jobs, job{path: path, options: options})
			}
		}
	}

	queue := make(chan job)
	outcomes := make(chan outcome)
	var workers sync.WaitGroup
	for range runtime.NumCPU() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			engines := map[format_options.Options]*prettier.Engine{}
			for job := range queue {
				engine := engines[job.options]
				if engine == nil {
					var newPrettierError error
					if engine, newPrettierError = prettier.New(job.options); newPrettierError != nil {
						outcomes <- outcome{job: job, detail: "engine: " + newPrettierError.Error()}
						continue
					}
					engines[job.options] = engine
				}
				source, readError := os.ReadFile(job.path)
				if readError != nil {
					outcomes <- outcome{job: job, detail: readError.Error()}
					continue
				}
				input := perturbMarkdown(string(source))
				expected, formatError := engine.Format(job.path, input)
				if formatError != nil {
					outcomes <- outcome{job: job, oracleFailed: true, detail: formatError.Error()}
					continue
				}
				actual, formatError := (Formatter{Options: job.options}).Format(job.path, input)
				if formatError == nil && actual == expected {
					outcomes <- outcome{job: job, identical: true}
					continue
				}
				result := outcome{job: job}
				if formatError != nil {
					result.detail = "error: " + formatError.Error()
				} else {
					result.detail = differential.FirstDifference(expected, actual)
				}
				// Attribution: with the oracle's embedded output, does markdown match?
				if len(markdown.EmbeddedParsers(input)) > 0 {
					fellBack := map[string]bool{}
					standIn, formatError := markdown.Format(
						input,
						job.options,
						standInEmbeds(job.options, engine, fellBack),
					)
					if formatError == nil && standIn == expected && len(fellBack) > 0 {
						var parsers []string
						for parser := range fellBack {
							parsers = append(parsers, parser)
						}
						result.attribution = "no native printer for " + strings.Join(uniqueSorted(parsers), ", ")
					}
				}
				outcomes <- result
			}
		}()
	}
	go func() {
		for _, job := range jobs {
			queue <- job
		}
		close(queue)
	}()
	go func() {
		workers.Wait()
		close(outcomes)
	}()

	identical, measured, oracleFailures := 0, 0, 0
	attributed := map[string]int{}
	var unattributed []string
	var misses []string
	for result := range outcomes {
		if result.oracleFailed {
			oracleFailures++
			continue
		}
		measured++
		if result.identical {
			identical++
			continue
		}
		line := fmt.Sprintf("%s (printWidth %d, tabWidth %d, useTabs %v): %s", result.path,
			result.options.PrintWidth, result.options.TabWidth, result.options.UseTabs, result.detail)
		if result.attribution != "" {
			attributed[result.attribution]++
			misses = append(misses, result.attribution+": "+line)
			continue
		}
		unattributed = append(unattributed, line)
		misses = append(misses, "unattributed: "+line)
	}
	sort.Strings(unattributed)
	sort.Strings(misses)

	test.Logf("perturbed markdown: %d/%d identical over %d inputs, %d oracle failures excluded; attributed %v",
		identical, measured, len(jobs), oracleFailures, attributed)
	if path := os.Getenv("COHERE_MARKDOWN_PERTURB_REPORT"); path != "" {
		if writeError := os.WriteFile(path, []byte(strings.Join(misses, "\n")+"\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for index, line := range unattributed {
		if index == 20 {
			test.Errorf("... and %d more", len(unattributed)-20)
			break
		}
		test.Errorf("markdown's own: %s", line)
	}
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	var unique []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	sort.Strings(unique)
	return unique
}
