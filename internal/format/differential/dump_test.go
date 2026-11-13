package differential

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/native"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/needs"
)

// TestDumpDifferences is TestCorpora's working copy for a printer port: the same comparison against the
// same oracle cache, but every difference is written out whole instead of three samples per language,
// so a porter can group the failures by construct and read the expected output beside its own.
//
// Off by default. Driven by TestCorpora's variables plus:
//
//	COHERE_FORMAT_DUMP        directory to write into: <corpus>/summary.txt, and <corpus>/files/<path>.expected
//	                          and .actual for every differing file
//	COHERE_FORMAT_EXTENSIONS  comma-separated extensions to compare, e.g. .ts,.tsx (default: every file)
//	COHERE_FORMAT_PRINT_WIDTH overrides each corpus's printWidth
//
// The print width override is a stress test, not the acceptance number. Our trees are already formatted
// at their own width, so most files are identical to their formatted form and only a few hundred test a
// layout decision. At another width nearly every file is rewritten, which exercises the breaking logic
// the way freshly written code will. The oracle cache keys on the options, so each width warms its own.
//
// The candidate is always native. It reads the cache TestCorpora warms under the same identity, so a
// dump costs the candidate's time once the corpus has been measured.
func TestDumpDifferences(test *testing.T) {
	test.Parallel()
	roots := needs.OptIn(
		test,
		"COHERE_FORMAT_CORPORA",
		"it dumps every difference of the formatter over a corpus of trees",
	)
	output := needs.OptIn(test, "COHERE_FORMAT_DUMP", "it dumps every difference of the formatter into that directory")
	extensions := map[string]bool{}
	for _, extension := range strings.Split(os.Getenv("COHERE_FORMAT_EXTENSIONS"), ",") {
		if extension = strings.TrimSpace(extension); extension != "" {
			extensions[extension] = true
		}
	}

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
	enumerator, newPrettierError := prettier.New(format_options.Default())
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}

	pending := strings.Split(roots, ":")
	seen := map[string]bool{}
	for len(pending) > 0 {
		root := expandHome(strings.TrimSpace(pending[0]))
		pending = pending[1:]
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true

		// Nested repositories regardless of root's own ignore lists, before anything can skip root, as
		// TestCorpora finds them (#k6vebep).
		nestedRepositories, nestedRepositoriesBelowError := format_files.NestedRepositoriesBelow(root)
		if nestedRepositoriesBelowError != nil {
			test.Fatalf("finding the repositories nested in %s: %v", root, nestedRepositoriesBelowError)
		}
		for _, nested := range nestedRepositories {
			pending = append(pending, filepath.Join(root, nested))
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

		enumeration, enumerateError := enumerator.Enumerate(root)
		if errors.Is(enumerateError, format_options.ErrPrettierConfigRemains) {
			test.Logf("skipping %s, not yet adopted: %v", root, enumerateError)
			continue
		}
		if enumerateError != nil {
			test.Fatalf("enumerating %s: %v", root, enumerateError)
		}
		files := enumeration.Files
		if len(extensions) > 0 {
			files = nil
			for _, file := range enumeration.Files {
				if extensions[filepath.Ext(file)] {
					files = append(files, file)
				}
			}
		}

		options := resolution.Options
		if width := os.Getenv("COHERE_FORMAT_PRINT_WIDTH"); width != "" {
			if _, scanError := fmt.Sscan(width, &options.PrintWidth); scanError != nil {
				test.Fatalf("COHERE_FORMAT_PRINT_WIDTH %q: %v", width, scanError)
			}
		}
		newOracle := func() (Formatter, error) { return prettier.New(options) }
		newCandidate := func() (Formatter, error) { return native.Formatter{Options: options}, nil }
		// The identity TestCorpora uses, so both read and warm one cache.
		cache := &OracleCache{
			Directory: directory,
			Identity:  digest + "|" + string(bundles.Origin) + "|filepath|" + describeOptions(options),
		}

		report, compareError := Compare(root, files, newOracle, newCandidate, runtime.NumCPU(), cache)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", root, compareError)
		}
		test.Log(report.Summary())

		// The whole root names the directory, because nested corpora share basenames (three are nexus).
		corpus := filepath.Join(
			output,
			strings.ReplaceAll(strings.Trim(root, string(filepath.Separator)), string(filepath.Separator), "_"),
		)
		var summary strings.Builder
		summary.WriteString(report.Summary())
		results := append([]FileResult(nil), report.Files...)
		sort.Slice(results, func(left, right int) bool { return results[left].Path < results[right].Path })
		oracle, newOracleError := newOracle()
		if newOracleError != nil {
			test.Fatal(newOracleError)
		}
		for _, result := range results {
			if result.Outcome != Different && result.Outcome != CandidateRefused {
				continue
			}
			relative, relativeError := filepath.Rel(root, result.Path)
			if relativeError != nil {
				test.Fatalf("naming %s under %s: %v", result.Path, root, relativeError)
			}
			fmt.Fprintf(&summary, "%s\t%s\t%s\n", result.Outcome, relative, firstLine(result.Detail))
			if result.Outcome != Different {
				continue
			}
			source, readError := os.ReadFile(result.Path)
			if readError != nil {
				test.Fatal(readError)
			}
			expected, formatError := cache.format(oracle, result.Path, string(source))
			if formatError != nil {
				test.Fatal(formatError)
			}
			actual, formatError := native.Formatter{Options: options}.Format(result.Path, string(source))
			if formatError != nil {
				test.Fatal(formatError)
			}
			base := filepath.Join(corpus, "files", relative)
			if createDirectoryError := os.MkdirAll(filepath.Dir(base), 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
			if writeError := os.WriteFile(base+".expected", []byte(expected), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			if writeError := os.WriteFile(base+".actual", []byte(actual), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
		if createDirectoryError := os.MkdirAll(corpus, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(
			filepath.Join(corpus, "summary.txt"),
			[]byte(summary.String()),
			0o644,
		); writeError != nil {
			test.Fatal(writeError)
		}
	}
}
