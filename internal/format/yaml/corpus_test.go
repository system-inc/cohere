package yaml

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/doc"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/markdown/mdast"
	"github.com/system-inc/cohere/internal/format/prettier"
	"github.com/system-inc/cohere/internal/needs"
)

// The printer's acceptance test: the differential's comparison over every .yaml and .yml file under the
// corpus roots, and over the YAML front matter of every .md file there, the port as the candidate and
// the embedded Prettier as the oracle. A measuring run, not a unit test.
//
//	COHERE_YAML_CORPUS   colon-separated roots, enumerated as TestCorpora does
//	COHERE_YAML_REPORT   optional file to write every difference to
//	COHERE_FORMAT_CACHE  oracle cache directory (default: the user cache directory)

// javaScriptSpaceText is the set String.prototype.trim removes, for markdown's front matter value.
const javaScriptSpaceText = "\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"

// oracleSizeLimit is the largest file measured. On a 7 MB and a 17 MB file the oracle's goja runtime,
// deep in nested generator delegation (yield*) inside eemeli/yaml, passed Go's 1 GB stack limit: a
// fatal error, not a panic a test can recover. The files that large are generated (debug-symbol
// relocation maps a Swift build leaves in .build).
const oracleSizeLimit = 1 << 20

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

// frontMatterCandidate embeds the port's doc the way markdown's front matter embed does
// (src/main/front-matter/embed.js): the value trimmed, printed with parser yaml through textToDoc,
// which strips the trailing hardline, between the delimiters.
type frontMatterCandidate struct {
	options format_options.Options
	// fullStack embeds FormatDoc's doc, the Go parser's tree printed, instead of the loaded tree's.
	fullStack    bool
	frontMatters map[string]*mdast.FrontMatter
	values       map[string]string
	trees        map[string]parsed
}

func (candidate frontMatterCandidate) Handles(fileName string) bool {
	return candidate.frontMatters[fileName] != nil
}

func (candidate frontMatterCandidate) Format(fileName string, _ string) (string, error) {
	frontMatter := candidate.frontMatters[fileName]
	var embedded doc.Doc
	var formatError error
	if candidate.fullStack {
		embedded, formatError = FormatDoc(candidate.values[fileName], candidate.options, nil)
	} else {
		tree := candidate.trees[fileName]
		if tree.parseFailure != "" {
			return "", fmt.Errorf("the parser failed: %s", tree.parseFailure)
		}
		embedded, formatError = PrintDoc(tree.root, candidate.values[fileName], candidate.options, nil)
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
		PrintWidth: candidate.options.PrintWidth,
		TabWidth:   candidate.options.TabWidth,
		UseTabs:    candidate.options.UseTabs,
	})
	// The markdown printer follows the front matter with a newline whatever comes after it.
	return printed + "\n", nil
}

// yamlCorpus is one repository's YAML and markdown files and the options its config resolves to.
type yamlCorpus struct {
	root          string
	yamlFiles     []string
	markdownFiles []string
	options       format_options.Options
}

// yamlCorpora enumerates each root the way TestCorpora does: through the engine's ignore layers, and
// into each nested repository as a corpus of its own, with its own config. So the files measured are
// the ones the formatter would be given, and not, say, the debug-symbol YAML a Swift build leaves in
// .build, which no repository formats and the oracle's goja runtime overflows its stack on.
func yamlCorpora(test *testing.T, roots string) []yamlCorpus {
	test.Helper()
	enumerator, newPrettierError := prettier.New(format_options.Default())
	if newPrettierError != nil {
		test.Fatal(newPrettierError)
	}
	var corpora []yamlCorpus
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
		corpus := yamlCorpus{root: root}
		for _, file := range enumeration.Files {
			if !filepath.IsAbs(file) {
				file = filepath.Join(root, file)
			}
			switch strings.ToLower(filepath.Ext(file)) {
			case ".yaml", ".yml":
				corpus.yamlFiles = append(corpus.yamlFiles, file)
			case ".md":
				corpus.markdownFiles = append(corpus.markdownFiles, file)
			}
		}
		if len(corpus.yamlFiles) == 0 && len(corpus.markdownFiles) == 0 {
			continue
		}

		// Both sides format with the same options, so a config cohere refuses (one naming an option
		// only JavaScript reads) measures the printer just as well under the defaults.
		corpus.options = format_options.Default()
		if resolution, resolveError := format_options.Resolve(root); resolveError != nil {
			test.Logf("%s: formatting with the default options: %v", root, resolveError)
		} else {
			corpus.options = resolution.Options
		}
		corpora = append(corpora, corpus)
	}
	// Every corpus measured, in one line, so a run that reaches fewer repositories says so.
	var measured []string
	for _, corpus := range corpora {
		measured = append(
			measured,
			fmt.Sprintf("%s (%d yaml, %d md)", corpus.root, len(corpus.yamlFiles), len(corpus.markdownFiles)),
		)
	}
	test.Logf("yaml corpora: %d\n  %s", len(corpora), strings.Join(measured, "\n  "))
	return corpora
}

func TestCorpusFormatMatchesOracle(test *testing.T) {
	test.Parallel()
	roots := needs.OptIn(test, "COHERE_YAML_CORPUS", "a measurement of the formatter against the oracle over a corpus")

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

	type tally struct{ identical, measured, rewriteIdentical, rewriteTotal, refused, oracleFailed int }
	measures := []string{
		"yaml files, full stack",
		"yaml files, tree loader",
		"front matter, full stack",
		"front matter, tree loader",
	}
	totals := map[string]*tally{}
	for _, name := range measures {
		totals[name] = &tally{}
	}
	syntaxErrors, tooLarge, parseDisagreements := 0, 0, 0
	var report strings.Builder
	for _, corpus := range yamlCorpora(test, roots) {
		options := corpus.options
		newEngine := func() (*prettier.Engine, error) { return prettier.New(options) }

		// The YAML files.
		candidate := yamlCandidate{options: options, texts: map[string]string{}, trees: map[string]parsed{}}
		fullStack := yamlCandidate{options: options, fullStack: true}
		var texts []string
		var yamlFiles []string
		for _, path := range corpus.yamlFiles {
			source, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			if len(source) > oracleSizeLimit {
				tooLarge++
				report.WriteString(fmt.Sprintf(
					"yaml files: too large for the oracle, not measured: %s: %d bytes\n",
					path,
					len(source),
				))
				continue
			}
			yamlFiles = append(yamlFiles, path)
			normalized, _ := normalizeInput(string(source))
			candidate.texts[path] = normalized
			texts = append(texts, normalized)
		}
		for index, tree := range parseTrees(test, texts) {
			candidate.trees[yamlFiles[index]] = tree
		}
		// TestCorpora's identity, character for character, so both share one cache.
		yamlCache := &differential.OracleCache{
			Directory: directory,
			Identity:  digest + "|" + string(bundles.Origin) + "|filepath|" + fmt.Sprintf("%+v", options),
		}
		yamlReport, compareError := differential.Compare(corpus.root, yamlFiles,
			func() (differential.Formatter, error) { return newEngine() },
			func() (differential.Formatter, error) { return candidate, nil },
			runtime.NumCPU(), yamlCache)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", corpus.root, compareError)
		}
		yamlFullStackReport, compareError := differential.Compare(corpus.root, yamlFiles,
			func() (differential.Formatter, error) { return newEngine() },
			func() (differential.Formatter, error) { return fullStack, nil },
			runtime.NumCPU(), yamlCache)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", corpus.root, compareError)
		}

		// The front matter of the markdown files: yaml, with a value left once trimmed.
		frontMatters := frontMatterCandidate{
			options:      options,
			frontMatters: map[string]*mdast.FrontMatter{},
			values:       map[string]string{},
			trees:        map[string]parsed{},
		}
		var frontMatterFiles []string
		var values []string
		for _, path := range corpus.markdownFiles {
			source, readError := os.ReadFile(path)
			if readError != nil {
				test.Fatal(readError)
			}
			normalized, _ := normalizeInput(string(source))
			frontMatter, _ := mdast.ParseFrontMatter(normalized)
			if frontMatter == nil || frontMatter.Language != "yaml" {
				continue
			}
			value := strings.Trim(frontMatter.Value, javaScriptSpaceText)
			if value == "" {
				continue
			}
			frontMatterFiles = append(frontMatterFiles, path)
			frontMatters.frontMatters[path] = frontMatter
			frontMatters.values[path] = value
			values = append(values, value)
		}
		var measuredFrontMatterFiles []string
		for index, tree := range parseTrees(test, values) {
			frontMatters.trees[frontMatterFiles[index]] = tree
			// The Go parser must refuse exactly the values the real one refuses.
			if _, parseError := parse(values[index], nil); (parseError != nil) != (tree.parseFailure != "") {
				parseDisagreements++
				report.WriteString(fmt.Sprintf(
					"front matter: the Go parser disagrees (real: %q, Go: %v): %s\n",
					tree.parseFailure,
					parseError,
					frontMatterFiles[index],
				))
			}
			if tree.parseFailure != "" {
				// Prettier prints such front matter as written, since the embed fails; there is no
				// YAML output to compare.
				syntaxErrors++
				report.WriteString(fmt.Sprintf(
					"front matter: YAML syntax error, not measured: %s: %s\n",
					frontMatterFiles[index],
					tree.parseFailure,
				))
				continue
			}
			measuredFrontMatterFiles = append(measuredFrontMatterFiles, frontMatterFiles[index])
		}
		// Its own identity: this oracle keeps only the front matter of the output.
		frontMatterCache := &differential.OracleCache{
			Directory: directory,
			Identity: digest +
				"|" +
				string(bundles.Origin) +
				"|yaml-front-matter-block|" +
				fmt.Sprintf("%+v", options),
		}
		newFrontMatterOracle := func() (differential.Formatter, error) {
			engine, newEngineError := newEngine()
			return frontMatterOracle{engine: engine}, newEngineError
		}
		frontMatterReport, compareError := differential.Compare(
			corpus.root,
			measuredFrontMatterFiles,
			newFrontMatterOracle,
			func() (differential.Formatter, error) { return frontMatters, nil },
			runtime.NumCPU(),
			frontMatterCache,
		)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", corpus.root, compareError)
		}
		frontMatterFullStack := frontMatters
		frontMatterFullStack.fullStack = true
		frontMatterFullStackReport, compareError := differential.Compare(
			corpus.root,
			measuredFrontMatterFiles,
			newFrontMatterOracle,
			func() (differential.Formatter, error) { return frontMatterFullStack, nil },
			runtime.NumCPU(),
			frontMatterCache,
		)
		if compareError != nil {
			test.Fatalf("comparing %s: %v", corpus.root, compareError)
		}

		for _, named := range []struct {
			name   string
			report differential.Report
		}{
			{"yaml files, full stack", yamlFullStackReport}, {"yaml files, tree loader", yamlReport},
			{"front matter, full stack", frontMatterFullStackReport}, {"front matter, tree loader", frontMatterReport},
		} {
			name, measured := named.name, named.report
			total := measured.Total()
			counts := totals[name]
			counts.identical += total.Identical
			counts.measured += total.Measured()
			counts.rewriteIdentical += total.RewriteIdentical
			counts.rewriteTotal += total.RewriteTotal
			counts.refused += total.CandidateRefused
			counts.oracleFailed += total.OracleFailed
			for _, result := range measured.Files {
				if result.Outcome == differential.Identical {
					continue
				}
				report.WriteString(fmt.Sprintf("%s: %s: %s: %s\n", name, result.Outcome, result.Path, result.Detail))
			}
		}
	}

	percent := func(part, whole int) float64 {
		if whole == 0 {
			return 0
		}
		return 100 * float64(part) / float64(whole)
	}
	for _, name := range measures {
		counts := totals[name]
		test.Logf("%s: %d/%d identical (%.2f%%), rewrite subset %d/%d (%.2f%%), refused %d, oracle failed %d",
			name, counts.identical, counts.measured, percent(counts.identical, counts.measured),
			counts.rewriteIdentical, counts.rewriteTotal, percent(counts.rewriteIdentical, counts.rewriteTotal),
			counts.refused, counts.oracleFailed)
	}
	test.Logf(
		"front matter not measured, a YAML syntax error: %d (the Go parser disagrees on %d); yaml files not measured, over %d bytes: %d",
		syntaxErrors,
		parseDisagreements,
		oracleSizeLimit,
		tooLarge,
	)
	if parseDisagreements > 0 {
		test.Errorf(
			"the Go parser and the real one disagree on whether %d front matter values parse",
			parseDisagreements,
		)
	}
	for index, line := range strings.SplitAfter(report.String(), "\n") {
		if index == 30 || line == "" {
			break
		}
		test.Log(strings.TrimSpace(line))
	}

	if path := os.Getenv("COHERE_YAML_REPORT"); path != "" {
		if writeError := os.WriteFile(path, []byte(report.String()), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}
