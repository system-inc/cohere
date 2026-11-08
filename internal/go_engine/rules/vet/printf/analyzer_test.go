package printf

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	original "golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstreamPackages are the fixture packages x/tools' own test runs the original over, copied unchanged into
// testdata/src.
var upstreamPackages = []string{
	"a", "b", "nofmt", "nonconst", "typeparams", "issue62595", "issue68744", "issue70572", "issue72850", "issue76616",
	"w",
}

// The original's test sets its -funcs flag to "Warn,Warnf" on its one global analyzer, and so does this package's
// test, on both analyzers, here, before any test runs: each flag adds to a package-level table every run reads, so
// setting it inside a parallel test would race the others reading it.
func init() {
	for _, analyzer := range []*analysis.Analyzer{original.Analyzer, Analyzer} {
		if setError := analyzer.Flags.Set("funcs", "Warn,Warnf"); setError != nil {
			panic("two names, neither empty, always set: " + setError.Error())
		}
	}
}

// The port is documented as the original is, byte for byte, which the registry's test holds too once the port is
// switched on.
func TestPortIsDocumentedAsTheOriginal(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != original.Analyzer.Name || Analyzer.Doc != original.Analyzer.Doc ||
		Analyzer.URL != original.Analyzer.URL {
		test.Errorf("the port is %q, %q, %q, the original %q, %q, %q", Analyzer.Name, Analyzer.Doc, Analyzer.URL,
			original.Analyzer.Name, original.Analyzer.Doc, original.Analyzer.URL)
	}
	if Analyzer.Flags.Lookup("funcs").Value.String() != original.Analyzer.Flags.Lookup("funcs").Value.String() {
		test.Errorf("the port's -funcs is %q, the original's %q", Analyzer.Flags.Lookup("funcs").Value,
			original.Analyzer.Flags.Lookup("funcs").Value)
	}
}

// x/tools' own fixtures for printf, copied unchanged into testdata/src and run as the original's test runs them:
// every `// want` the original expects, findings and facts, the port reports at the same line with the same text,
// and analysistest fails on a finding no want expects as much as on a want nothing found.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, upstreamPackages...)
}

// The original's two version-gated tests, each a module of its own in a txtar archive: a file of go1.24 or later
// gets the non-constant format finding and its suggested fix, which must give the archive's golden file, and an
// earlier file gets neither. The original extracts them with x/tools' internal testfiles and skips with its internal
// testenv on a Go before 1.23 or 1.24; here the public txtar does the extracting, and no skip is needed, since this
// module's go line is 1.27.
func TestNonConstantFormatStringGo123(test *testing.T) {
	test.Parallel()
	directory := extractArchive(test, "nonconst_go123.txtar")
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, directory, Analyzer, "example.com/nonconst")
}

func TestNonConstantFormatStringGo124(test *testing.T) {
	test.Parallel()
	directory := extractArchive(test, "nonconst_go124.txtar")
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, directory, Analyzer, "example.com/nonconst")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding a violation of every message and every branch, a clean case beside each, and the shapes most likely to
// differ, and their findings must be the same multiset of file, line, column and message, as must the facts each
// exports, by object and by the fact's words. Each side's count is logged, since two empty sides would match and
// prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	environment := []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}
	compareSides(test, directory, environment, append(slices.Clone(upstreamPackages), "parity", "parity/dependency"))
}

// The two sides agree over the version-gated archives too, each loaded as a module, as analysistest loads it.
func TestPortMatchesOriginalOverTheArchives(test *testing.T) {
	test.Parallel()
	for _, archive := range []string{"nonconst_go123.txtar", "nonconst_go124.txtar"} {
		test.Run(archive, func(test *testing.T) {
			test.Parallel()
			directory := extractArchive(test, archive)
			environment := []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}
			compareSides(test, directory, environment, []string{"example.com/nonconst"})
		})
	}
}

// compareSides fails the test unless the original and the port find and export the same over patterns.
func compareSides(test *testing.T, directory string, environment []string, patterns []string) {
	test.Helper()
	original, port := findingsOf(test, directory, environment, patterns)
	test.Logf("the original found %d and exported %d facts, the port %d and %d", len(original.findings),
		len(original.facts), len(port.findings), len(port.facts))
	if len(original.findings) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original.findings, port.findings) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range difference(port.findings, original.findings) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
	for _, missing := range difference(original.facts, port.facts) {
		test.Errorf("the original exported %s, and the port did not", missing)
	}
	for _, extra := range difference(port.facts, original.facts) {
		test.Errorf("the port exported %s, and the original did not", extra)
	}
}

// side is what one analyzer found and exported, each as a line naming its file, line and column.
type side struct {
	findings []string
	facts    []string
}

// findingsOf loads the packages patterns name at directory under environment, runs the original and the port over
// them in one checker, and returns each one's findings and exported facts, sorted.
func findingsOf(test *testing.T, directory string, environment []string, patterns []string) (side, side) {
	test.Helper()
	loaded, loadError := load.Packages(directory, patterns, environment, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// A fixture that does not type-check is skipped by both sides, which would then agree on nothing.
	for _, root := range loaded.Roots {
		for _, packageError := range root.Errors {
			test.Errorf("%s: %v", root.PkgPath, packageError)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{original.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	sides := map[*analysis.Analyzer]*side{original.Analyzer: {}, Analyzer: {}}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		found := sides[action.Analyzer]
		for _, diagnostic := range action.Diagnostics {
			// The end and every suggested edit are compared too, so a finding placed alike but spanning
			// otherwise, or fixed otherwise, is a difference.
			message := diagnostic.Message + " to " + described(action, diagnostic.End, "")
			for _, fix := range diagnostic.SuggestedFixes {
				for _, edit := range fix.TextEdits {
					message += fmt.Sprintf(", fix %q at %s%q", fix.Message, described(action, edit.Pos, ""),
						edit.NewText)
				}
			}
			found.findings = append(found.findings, described(action, diagnostic.Pos, message))
		}
		objectFacts, _ := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			found.facts = append(found.facts, described(action, objectFact.Object.Pos(),
				fmt.Sprintf("%s %v", objectFact.Object.Name(), objectFact.Fact)))
		}
	}
	for _, found := range sides {
		slices.Sort(found.findings)
		slices.Sort(found.facts)
	}
	return *sides[original.Analyzer], *sides[Analyzer]
}

// described is message at position in action's package as a finding line prints it, with the file's base name.
func described(action *checker.Action, position token.Pos, message string) string {
	resolved := action.Package.Fset.Position(position)
	return fmt.Sprintf("%s:%d:%d: %s", filepath.Base(resolved.Filename), resolved.Line, resolved.Column, message)
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []string, without []string) []string {
	remaining := map[string]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []string{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// extractArchive writes the txtar archive of testdata named name into a directory of the test's own, as x/tools'
// testfiles.ExtractTxtarFileToTmp does, and returns the directory.
func extractArchive(test *testing.T, name string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), name))
	if parseError != nil {
		test.Fatal(parseError)
	}
	files, filesError := txtar.FS(archive)
	if filesError != nil {
		test.Fatal(filesError)
	}
	directory := test.TempDir()
	if copyError := os.CopyFS(directory, files); copyError != nil {
		test.Fatal(copyError)
	}
	return directory
}

// errorsOnly hands analysistest a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
