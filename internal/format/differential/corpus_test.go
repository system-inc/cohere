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

// TestCorpora is the acceptance test: the native printer against the embedded Prettier, over real trees.
//
// Off by default, because it reads every formattable file in three repositories and the first run
// formats all of them through goja. Driven by environment:
//
//	COHERE_FORMAT_CORPORA     colon-separated repository roots, e.g. ~/Projects/ahra:~/Projects/phi/www-phi-health
//	COHERE_FORMAT_CANDIDATE   native (the printers that replace Prettier), none, unchanged, or oracle
//	COHERE_FORMAT_CACHE       oracle cache directory (default: the user cache directory)
//
// "none" is the honest candidate today and reads 0%. "unchanged" and "oracle" are the two controls,
// run against the real corpora rather than only the fixtures: oracle against itself must read 100%
// everywhere, and unchanged reads 0% on the rewrite subset while revealing how large that subset is.
//
// A repository containing other repositories is measured as several corpora, one per root, because
// the format phase refuses to walk into a nested repository and a percentage taken across a boundary
// nobody can see is the 08-25 mistake: a root that silently measures half its tree.
func TestCorpora(test *testing.T) {
	test.Parallel()
	roots := needs.OptIn(
		test,
		"COHERE_FORMAT_CORPORA",
		"a measurement of the formatter against the oracle over a corpus of trees",
	)

	candidateName := os.Getenv("COHERE_FORMAT_CANDIDATE")

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
	test.Logf("oracle cache %s, bundles %s from %s", directory, digest[:12], bundles.Origin)

	enumerator, newPrettierError := prettier.New(format_options.Default())
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}

	pending := strings.Split(roots, ":")
	named := map[string]bool{}
	for _, root := range pending {
		named[expandHome(strings.TrimSpace(root))] = true
	}
	seen := map[string]bool{}
	// Every corpus measured, and every one skipped, logged together at the end, so a run that measures
	// fewer repositories than the last one says so in one place rather than in a smaller total.
	var measured []string
	var skipped []string
	offeredTotal := 0
	for len(pending) > 0 {
		root := expandHome(strings.TrimSpace(pending[0]))
		pending = pending[1:]
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true

		// Nested repositories are found regardless of root's own ignore lists, before anything else can
		// skip root: a repository under a path root never formats (ahra's projects/**) is still evidence
		// for the printers, and dropping it shrank the corpus by about 1,950 files with every line still
		// reading 100% (#k6vebep).
		nestedRepositories, nestedRepositoriesBelowError := format_files.NestedRepositoriesBelow(root)
		if nestedRepositoriesBelowError != nil {
			test.Fatalf("finding the repositories nested in %s: %v", root, nestedRepositoriesBelowError)
		}
		for _, nested := range nestedRepositories {
			pending = append(pending, filepath.Join(root, nested))
		}

		// Each corpus formats with its own resolved options. One set of options for every repository is
		// how api-phi-health's bracketSameLine went unmeasured on the first run.
		//
		// A nested repository still carrying Prettier config is one pinned to its own adoption (www-ahra-ai,
		// #j3nk2zk): measuring it would mean guessing its options, so it is named and skipped. A named
		// corpus refusing is a migration that did not happen, and stops the run.
		resolution, resolveError := format_options.Resolve(root)
		if errors.Is(resolveError, format_options.ErrPrettierConfigRemains) && !named[root] {
			test.Logf("skipping %s, a nested repository not yet adopted: %v", root, resolveError)
			skipped = append(skipped, root)
			continue
		}
		if resolveError != nil {
			test.Fatalf("resolving the format options for %s: %v", root, resolveError)
		}

		// A leftover .prettierignore is Prettier config too, refused by the walk rather than the resolver,
		// and skipped or fatal on the same terms.
		enumeration, enumerateError := enumerator.Enumerate(root)
		if errors.Is(enumerateError, format_options.ErrPrettierConfigRemains) && !named[root] {
			test.Logf("skipping %s, a nested repository not yet adopted: %v", root, enumerateError)
			skipped = append(skipped, root)
			continue
		}
		if enumerateError != nil {
			test.Fatalf("enumerating %s: %v", root, enumerateError)
		}

		options := resolution.Options
		newOracle := func() (Formatter, error) { return prettier.New(options) }
		newCandidate := candidateFor(test, candidateName, newOracle, options)
		cache := &OracleCache{
			Directory: directory,
			// "filepath" marks oracle output computed with the file name passed to Prettier. Answers
			// cached before that changed had no filepath, and a cache serving them would hide the fix.
			Identity: digest + "|" + string(bundles.Origin) + "|filepath|" + describeOptions(options),
		}

		report, compareError := Compare(root, enumeration.Files, newOracle, newCandidate, runtime.NumCPU(), cache)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", root, compareError)
		}
		test.Logf("walked %d, offered %d, nested repositories %v\nconfig %s\n%s",
			enumeration.Walked, len(enumeration.Files), nestedRepositories,
			resolution.Source, report.Summary())
		logSampleDifferences(test, report)
		measured = append(measured, fmt.Sprintf("%s (%d)", root, len(enumeration.Files)))
		offeredTotal += len(enumeration.Files)
	}
	test.Logf("measured %d corpora, %d files offered in all:\n  %s\nskipped %d: %v",
		len(measured), offeredTotal, strings.Join(measured, "\n  "), len(skipped), skipped)
}

func candidateFor(test *testing.T, name string, newOracle NewFormatter, options format_options.Options) NewFormatter {
	switch name {
	case "native":
		// The printers that replace Prettier. A file type with no printer yet is refused, so this reads
		// 0% for it rather than crediting work that has not been done.
		test.Logf("native printers registered for %v", native.Extensions())
		return func() (Formatter, error) { return native.Formatter{Options: options}, nil }
	case "", "none":
		return func() (Formatter, error) { return refuser{}, nil }
	case "unchanged":
		return func() (Formatter, error) { return unchanged{}, nil }
	case "oracle":
		return newOracle
	default:
		test.Fatalf("unknown COHERE_FORMAT_CANDIDATE %q: native, none, unchanged or oracle", name)
		return nil
	}
}

// describeOptions puts every option into the cache identity, so changing one cannot serve old answers.
//
// %+v names every field, so a field added to Options later joins the identity without anyone
// remembering to add it here.
func describeOptions(options format_options.Options) string {
	return fmt.Sprintf("%+v", options)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, homeDirectoryError := os.UserHomeDir(); homeDirectoryError == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// logSampleDifferences prints a few differences per language, so a run teaches something beyond a count.
func logSampleDifferences(test *testing.T, report Report) {
	byLanguage := map[string][]FileResult{}
	for _, result := range report.Files {
		if result.Outcome == Different {
			byLanguage[result.Language] = append(byLanguage[result.Language], result)
		}
	}
	languages := make([]string, 0, len(byLanguage))
	for language := range byLanguage {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	for _, language := range languages {
		results := byLanguage[language]
		for index := 0; index < len(results) && index < 3; index++ {
			test.Logf("  %s differs: %s: %s", language, results[index].Path, results[index].Detail)
		}
	}
}
