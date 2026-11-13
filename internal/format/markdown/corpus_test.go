package markdown

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/format/printing"
	"github.com/system-inc/cohere/internal/needs"
)

// The printer's acceptance test, run from this package: the differential's comparison over every .md
// file in the corpora, the port as the candidate and the embedded Prettier as the oracle. It shares
// TestCorpora's oracle cache, so the two never format a file twice, and it builds without the native
// package, whose other printers are mid-port.
//
//	COHERE_MARKDOWN_CORPORA   colon-separated repository roots
//	COHERE_MARKDOWN_REPORT    optional file to write every difference to
//	COHERE_FORMAT_CACHE       oracle cache directory (default: the user cache directory)

// markdownCandidate formats .md files with the port and refuses the rest.
type markdownCandidate struct {
	options   format_options.Options
	textToDoc printing.TextToDoc
}

// oracleTextToDoc formats embedded code with the embedded Prettier, standing in for native printers
// that do not exist yet, so the embed plumbing (fences, front matter) is measured on its own. Set
// COHERE_MARKDOWN_EMBED_ORACLE to use it.
func oracleTextToDoc(test *testing.T, options format_options.Options) printing.TextToDoc {
	if os.Getenv("COHERE_MARKDOWN_EMBED_ORACLE") == "" {
		return nil
	}
	engine, newPrettierError := prettier.New(options)
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}
	fileNames := map[string]string{
		"typescript": "embedded.tsx", "babel": "embedded.js", "json": "embedded.json", "json5": "embedded.json5",
		"jsonc": "embedded.jsonc", "yaml": "embedded.yaml", "graphql": "embedded.graphql", "css": "embedded.css",
		"scss": "embedded.scss", "less": "embedded.less", "markdown": "embedded.md",
	}
	return func(text string, parser string) (doc.Doc, error) {
		fileName := parser
		if !strings.Contains(parser, ".") {
			fileName = fileNames[parser]
		}
		formatted, formatError := engine.Format(fileName, text)
		if formatError != nil {
			return nil, formatError
		}
		return doc.Text(strings.TrimSuffix(formatted, "\n")), nil
	}
}

func (candidate markdownCandidate) Handles(fileName string) bool {
	return strings.EqualFold(filepath.Ext(fileName), ".md")
}

func (candidate markdownCandidate) Format(fileName string, text string) (string, error) {
	if !candidate.Handles(fileName) {
		return "", fmt.Errorf("markdown: not a .md file: %s", fileName)
	}
	return Format(text, candidate.options, candidate.textToDoc)
}

// dependsOnEmbed reports whether Prettier formats part of the file as another language. Those parts
// cannot match until that language's printer exists, so the report separates them.
func dependsOnEmbed(text string) bool { return len(EmbeddedParsers(text)) > 0 }

// markdownCorpus is one repository's markdown files and the options its config resolves to.
type markdownCorpus struct {
	root    string
	files   []string
	options format_options.Options
}

// markdownCorpora enumerates the .md files of each root the way TestCorpora does: through the
// engine's ignore layers, and into each nested repository as a corpus of its own, with its own config.
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

func TestCorpusFormatMatchesOracle(test *testing.T) {
	test.Parallel()
	roots := needs.OptIn(
		test,
		"COHERE_MARKDOWN_CORPORA",
		"a measurement of the formatter against the oracle over a corpus",
	)

	bundles, bundlesError := prettier.Bundles()
	if bundlesError != nil {
		test.Fatal(bundlesError)
	}
	digest, digestBundlesError := prettier.DigestBundles(bundles.Files)
	if digestBundlesError != nil {
		test.Fatal(digestBundlesError)
	}
	directory := os.Getenv("COHERE_FORMAT_CACHE")
	if directory == "" {
		base, userCacheDirectoryError := os.UserCacheDir()
		if userCacheDirectoryError != nil {
			test.Fatal(userCacheDirectoryError)
		}
		directory = filepath.Join(base, "cohere", "prettier-oracle")
	}
	var report strings.Builder
	for _, corpus := range markdownCorpora(test, roots) {
		root, files, options := corpus.root, corpus.files, corpus.options
		cache := &differential.OracleCache{
			Directory: directory,
			// TestCorpora's identity, character for character, so both share one cache.
			Identity: digest + "|" + string(bundles.Origin) + "|filepath|" + fmt.Sprintf("%+v", options),
		}
		newOracle := func() (differential.Formatter, error) { return prettier.New(options) }
		newCandidate := func() (differential.Formatter, error) {
			return markdownCandidate{options: options, textToDoc: oracleTextToDoc(test, options)}, nil
		}

		compared, compareError := differential.Compare(root, files, newOracle, newCandidate, runtime.NumCPU(), cache)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", root, compareError)
		}

		type count struct{ identical, measured, rewriteIdentical, rewriteTotal int }
		var all count
		var withoutEmbed count
		differences := map[string]int{}
		var different []differential.FileResult
		for _, result := range compared.Files {
			if result.Outcome == differential.OracleFailed {
				continue
			}
			path := result.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			source, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			embedded := dependsOnEmbed(string(source))
			for _, tally := range []*count{&all, &withoutEmbed} {
				if tally == &withoutEmbed && embedded {
					continue
				}
				tally.measured++
				if result.Outcome == differential.Identical {
					tally.identical++
				}
				if result.OracleRewrites {
					tally.rewriteTotal++
					if result.Outcome == differential.Identical {
						tally.rewriteIdentical++
					}
				}
			}
			if result.Outcome != differential.Identical {
				different = append(different, result)
				if !embedded {
					differences[string(result.Outcome)]++
				}
			}
		}

		percent := func(part, whole int) float64 {
			if whole == 0 {
				return 0
			}
			return 100 * float64(part) / float64(whole)
		}
		test.Logf("%s: md %d/%d identical (%.2f%%), rewrite subset %d/%d (%.2f%%)",
			root, all.identical, all.measured, percent(all.identical, all.measured),
			all.rewriteIdentical, all.rewriteTotal, percent(all.rewriteIdentical, all.rewriteTotal))
		test.Logf(
			"%s without embedded-language files: md %d/%d identical (%.2f%%), rewrite subset %d/%d (%.2f%%); outcomes %v",
			root,
			withoutEmbed.identical,
			withoutEmbed.measured,
			percent(withoutEmbed.identical, withoutEmbed.measured),
			withoutEmbed.rewriteIdentical,
			withoutEmbed.rewriteTotal,
			percent(withoutEmbed.rewriteIdentical, withoutEmbed.rewriteTotal),
			differences,
		)

		sort.Slice(different, func(left, right int) bool { return different[left].Path < different[right].Path })
		for index, result := range different {
			line := fmt.Sprintf("%s: %s: %s\n", result.Outcome, result.Path, result.Detail)
			report.WriteString(line)
			if index < 15 {
				test.Log(strings.TrimSpace(line))
			}
		}
	}

	if path := os.Getenv("COHERE_MARKDOWN_REPORT"); path != "" {
		if writeError := os.WriteFile(path, []byte(report.String()), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}
