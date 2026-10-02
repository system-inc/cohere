package markdown

import (
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
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
)

// The perturbed corpus. Our markdown is overwhelmingly already formatted, so the straight run tests
// preservation far more than printing. This one rewrites each file into something Prettier has to
// change, then formats it under several option sets, the port against the oracle on the same input.
//
//	COHERE_MARKDOWN_PERTURB_CORPORA   colon-separated repository roots
//	COHERE_MARKDOWN_PERTURB_REPORT    optional file to write every difference to

// perturbation rewrites lines into an equivalent or nearly equivalent markdown Prettier normalizes.
type perturbation struct {
	pattern     *regexp.Regexp
	replacement string
}

var perturbations = []perturbation{
	// Bullets Prettier rewrites to `-` (or alternates by sibling list).
	{regexp.MustCompile(`(?m)^(\s*)- `), "$1* "},
	// Ordered lists all numbered one, which Prettier renumbers or keeps by its own rule.
	{regexp.MustCompile(`(?m)^(\s*)\d+\. `), "${1}1. "},
	// Strong with underscores, which Prettier prints with asterisks.
	{regexp.MustCompile(`\*\*([^*\s][^*]*[^*\s])\*\*`), "__${1}__"},
	// Emphasis with asterisks, which Prettier prints with underscores where it may.
	{regexp.MustCompile(`(^|[\s(])_([^_\s][^_]*[^_\s])_([\s).,]|$)`), "$1*$2*$3"},
	// Table rows with their padding removed, which Prettier realigns.
	{regexp.MustCompile(`(?m)^\|.*\|$`), "\x00table"},
	// Closing sequences on ATX headings, which Prettier drops.
	{regexp.MustCompile(`(?m)^(#{1,6}) (.+)$`), "$1   $2 $1"},
	// Tilde fences, which Prettier prints with backticks.
	{regexp.MustCompile("(?m)^```"), "~~~"},
}

var tableCellPadding = regexp.MustCompile(`[ \t]*\|[ \t]*`)

func perturb(text string) string {
	for _, perturbation := range perturbations {
		if perturbation.replacement == "\x00table" {
			text = perturbation.pattern.ReplaceAllStringFunc(text, func(row string) string {
				return tableCellPadding.ReplaceAllString(row, "|")
			})
			continue
		}
		text = perturbation.pattern.ReplaceAllString(text, perturbation.replacement)
	}
	// Trailing spaces on every third line, which Prettier strips outside hard breaks.
	lines := strings.Split(text, "\n")
	for index := range lines {
		if index%3 == 2 && strings.TrimSpace(lines[index]) != "" {
			lines[index] += " "
		}
	}
	return strings.Join(lines, "\n")
}

func TestPerturbedCorpusFormatMatchesOracle(t *testing.T) {
	roots := os.Getenv("COHERE_MARKDOWN_PERTURB_CORPORA")
	if roots == "" {
		t.Skip("set COHERE_MARKDOWN_PERTURB_CORPORA to measure; this is a measuring run, not a unit test")
	}

	enumerator, err := prettier.New(prettier.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}

	var files []string
	for _, root := range strings.Split(roots, ":") {
		root = strings.TrimSpace(root)
		if strings.HasPrefix(root, "~/") {
			home, _ := os.UserHomeDir()
			root = filepath.Join(home, root[2:])
		}
		if root == "" {
			continue
		}
		structureIgnore := filepath.Join(root, "libraries", "structure", "code-quality", "PrettierIgnoreDefaults.ts")
		enumeration, err := enumerator.Enumerate(root, structureIgnore)
		if err != nil {
			t.Fatalf("enumerating %s: %v", root, err)
		}
		for _, file := range enumeration.Files {
			if strings.EqualFold(filepath.Ext(file), ".md") {
				if !filepath.IsAbs(file) {
					file = filepath.Join(root, file)
				}
				files = append(files, file)
			}
		}
	}

	variants := []prettier.Options{prettier.DefaultOptions()}
	narrow := prettier.DefaultOptions()
	narrow.PrintWidth, narrow.TabWidth = 40, 2
	tabs := prettier.DefaultOptions()
	tabs.UseTabs, tabs.SingleQuote = true, false
	variants = append(variants, narrow, tabs)

	type job struct {
		path    string
		variant int
	}
	type outcome struct {
		job
		identical, embedded, rewrites bool
		detail                        string
	}
	jobs := make(chan job)
	outcomes := make(chan outcome)
	var workers sync.WaitGroup
	for range runtime.NumCPU() {
		workers.Add(1)
		go func() {
			defer workers.Done()
			engines := map[int]*prettier.Engine{}
			embeds := map[int]printing.TextToDoc{}
			for job := range jobs {
				options := variants[job.variant]
				engine := engines[job.variant]
				if engine == nil {
					engine, err = prettier.New(options)
					if err != nil {
						outcomes <- outcome{job: job, detail: "engine: " + err.Error()}
						continue
					}
					engines[job.variant] = engine
					embeds[job.variant] = oracleTextToDoc(t, options)
				}
				source, err := os.ReadFile(job.path)
				if err != nil {
					outcomes <- outcome{job: job, detail: err.Error()}
					continue
				}
				input := perturb(string(source))
				expected, err := engine.Format(job.path, input)
				if err != nil {
					outcomes <- outcome{job: job, identical: true, detail: "oracle failed: " + err.Error()}
					continue
				}
				actual, err := Format(input, options, embeds[job.variant])
				result := outcome{job: job, embedded: dependsOnEmbed(input), rewrites: expected != input}
				switch {
				case err != nil:
					result.detail = "error: " + err.Error()
				case actual != expected:
					result.detail = differential.FirstDifference(expected, actual)
				default:
					result.identical = true
				}
				outcomes <- result
			}
		}()
	}
	go func() {
		for variant := range variants {
			for _, path := range files {
				jobs <- job{path: path, variant: variant}
			}
		}
		close(jobs)
	}()
	go func() {
		workers.Wait()
		close(outcomes)
	}()

	identical, measured, embeddedIdentical, embeddedMeasured, rewrites := 0, 0, 0, 0, 0
	var differences []string
	for result := range outcomes {
		measured++
		if result.rewrites {
			rewrites++
		}
		if result.embedded {
			embeddedMeasured++
		}
		if result.identical {
			identical++
			if result.embedded {
				embeddedIdentical++
			}
			continue
		}
		differences = append(differences, fmt.Sprintf("variant %d, embed %v: %s: %s", result.variant, result.embedded, result.path, result.detail))
	}
	sort.Strings(differences)

	t.Logf("perturbed: %d/%d identical over %d files and %d option sets, %d rewritten by the oracle; without embedded-language files %d/%d",
		identical, measured, len(files), len(variants), rewrites,
		identical-embeddedIdentical, measured-embeddedMeasured)
	for index := 0; index < len(differences) && index < 15; index++ {
		t.Log(differences[index])
	}
	if path := os.Getenv("COHERE_MARKDOWN_PERTURB_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(strings.Join(differences, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
