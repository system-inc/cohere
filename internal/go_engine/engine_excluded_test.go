package go_engine

// A file the project record names as excluded is never linted or counted as checked. Its package is still loaded, as
// any import of it would need, but a package with no file in scope is no lint root, and a file out of scope in a
// package with one in scope is read by the rules with its package and never counted.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// excludedRun is what these tests read of one run: its project, types and lint records, and the files a planted rule
// was given.
type excludedRun struct {
	project contract.ProjectRecord
	types   contract.TypesRecord
	lint    contract.LintRecord
	read    []string
}

// excludedModule writes a git repository holding a module of one package, example.com/value, with each of files at
// its name, and returns its directory, resolved, so the file names the engine reports are the ones written here.
func excludedModule(test *testing.T, files map[string]string) string {
	test.Helper()
	directory, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTextRulesFile(test, directory, "go.mod", fmt.Appendf(nil, "module example.com/value\n\ngo %s\n",
		textRulesGoVersion(test)))
	for name, contents := range files {
		writeTextRulesFile(test, directory, name, []byte(contents))
	}
	runGit(test, directory, "init", "-q")
	return directory
}

// runExcluded runs every registered rule and a planted one that records the files it is given over directory, without
// fixes or the findings cache, and returns what the run wrote.
func runExcluded(test *testing.T, directory string) excludedRun {
	test.Helper()
	recorder := newSourceRecorder()
	registered := append(slices.Clone(rules.All()), recorder.rule())
	stream, prose := runRecorded(test, directory, registered, "--no-fix", "--no-cache")
	var ran excludedRun
	seen := map[string]bool{}
	for line := range strings.Lines(stream) {
		var record struct {
			Kind string `json:"kind"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("a line of the stream is not a record: %v: %s", decodeError, line)
		}
		var decodeError error
		switch record.Kind {
		case "project":
			decodeError = json.Unmarshal([]byte(line), &ran.project)
		case "types":
			decodeError = json.Unmarshal([]byte(line), &ran.types)
		case "lint":
			decodeError = json.Unmarshal([]byte(line), &ran.lint)
		default:
			continue
		}
		if decodeError != nil {
			test.Fatalf("the %s record does not decode: %v: %s", record.Kind, decodeError, line)
		}
		seen[record.Kind] = true
	}
	for _, kind := range []string{"project", "types", "lint"} {
		if !seen[kind] {
			test.Fatalf("the run wrote no %s record; stderr:\n%s\nstream:\n%s", kind, prose, stream)
		}
	}
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	for file := range recorder.read {
		ran.read = append(ran.read, file)
	}
	slices.Sort(ran.read)
	return ran
}

// A module whose only file git ignores is type-checked and never linted: the project record names the file as
// ignored by git at `.gitignore:1`, the types record counts no file, the lint record walks no file and visits no node,
// and no rule is given it.
func TestAPackageEveryFileOfWhichGitIgnoresIsNeverLinted(test *testing.T) {
	test.Parallel()

	directory := excludedModule(test, map[string]string{
		".gitignore": "*.go\n",
		"value.go":   "package value\n\n// Value is one.\nfunc Value() int { return 1 }\n",
	})
	ran := runExcluded(test, directory)

	value := filepath.Join(directory, "value.go")
	want := []contract.ExcludedFile{{File: value, Reason: "ignored by git", Source: ".gitignore:1"}}
	if !slices.Equal(ran.project.Excluded, want) || ran.project.FilesInScope != 0 {
		test.Errorf("want %s excluded alone, ignored by git at .gitignore:1, and nothing in scope, got %+v and %d in "+
			"scope", value, ran.project.Excluded, ran.project.FilesInScope)
	}
	if ran.types.Files != 0 {
		test.Errorf("the types record counts %d files, want 0", ran.types.Files)
	}
	if ran.lint.FilesWalked != 0 || ran.lint.NodesVisited != 0 {
		test.Errorf("the lint record walked %d files and visited %d nodes, want 0 and 0", ran.lint.FilesWalked,
			ran.lint.NodesVisited)
	}
	if len(ran.read) > 0 {
		test.Errorf("a rule was given %q, which git ignores", ran.read)
	}
}

// A package holding one file git ignores and one it does not is linted as before: a rule is given both, as the
// package's, and the run counts one file checked, whose nodes alone are counted, as many as a run over the module
// without the ignored file visits.
func TestAPackageWithAFileInScopeIsLintedAndCountsOnlyThatFile(test *testing.T) {
	test.Parallel()

	valueSource := "package value\n\n// Value is one.\nfunc Value() int { return 1 }\n"
	mixed := excludedModule(test, map[string]string{
		".gitignore": "scratch.go\n",
		"value.go":   valueSource,
		"scratch.go": "package value\n\n// Scratch is two.\nfunc Scratch() int { return Value() + Value() }\n",
	})
	alone := excludedModule(test, map[string]string{"value.go": valueSource})
	ran := runExcluded(test, mixed)
	baseline := runExcluded(test, alone)

	scratch, value := filepath.Join(mixed, "scratch.go"), filepath.Join(mixed, "value.go")
	want := []contract.ExcludedFile{{File: scratch, Reason: "ignored by git", Source: ".gitignore:1"}}
	if !slices.Equal(ran.project.Excluded, want) {
		test.Errorf("want %s excluded, ignored by git at .gitignore:1, got %+v", scratch, ran.project.Excluded)
	}
	if ran.types.Files != 1 || ran.lint.FilesWalked != 1 {
		test.Errorf("want 1 file type-checked and 1 walked, got %d and %d", ran.types.Files, ran.lint.FilesWalked)
	}
	if ran.lint.NodesVisited == 0 || ran.lint.NodesVisited != baseline.lint.NodesVisited {
		test.Errorf("the lint record visited %d nodes, want the %d of %s alone", ran.lint.NodesVisited,
			baseline.lint.NodesVisited, value)
	}
	if !slices.Equal(ran.read, []string{scratch, value}) {
		test.Errorf("want a rule given both files of the package, got %q", ran.read)
	}
}

// A run narrowed to inside saves outside, which inside imports and which holds a finding of a rule with no facts, in
// the findings cache as a dependency: its facts and none of that finding. A whole run over the same store then
// analyzes outside as a package to analyze, so the entry falls short of what it asks (dropShortHits) and is a miss,
// never restored as a package with no findings: the whole run reads as a run without the cache does, outside's
// finding included.
func TestAWholeRunAfterANarrowedOneFindsWhatTheNarrowedRunSavedAsADependency(test *testing.T) {
	test.Parallel()

	root := excludedModule(test, map[string]string{
		"inside/inside.go": "// Package inside reports through outside.\npackage inside\n\n" +
			"import \"example.com/value/outside\"\n\n// Run reports.\nfunc Run() { outside.Report() }\n",
		"outside/outside.go": "// Package outside holds one line past the house limit.\npackage outside\n\n" +
			"// Report reports nothing, and its comment runs past the limit, which the long-line rule finds" +
			strings.Repeat(" and finds", 4) + ".\nfunc Report() {}\n",
	})
	outsideFinding := `"file":"` + filepath.Join(root, "outside", "outside.go") + `"`

	narrowed := runCachedEngine(test, root, filepath.Join(root, "inside"))
	if strings.Contains(narrowed.stream, outsideFinding) || narrowed.counts.Written == 0 {
		test.Fatalf("want a narrowed run that writes entries and reports nothing in outside, got %+v in\n%s",
			narrowed.counts, narrowed.stream)
	}
	whole := runCachedEngine(test, root)
	uncached := runCachedEngine(test, root, "--no-cache")
	if !strings.Contains(uncached.stream, outsideFinding) {
		test.Fatalf("want the run without the cache to report outside's finding, got\n%s", uncached.stream)
	}
	if !strings.Contains(whole.stream, outsideFinding) {
		test.Errorf("the whole run over the narrowed run's store lost outside's finding:\n%s", whole.stream)
	}
	if whole.counts.Hit == 0 {
		test.Errorf("want the whole run to restore what the narrowed run saved, got %+v", whole.counts)
	}
	if whole.withoutCounts != uncached.withoutCounts {
		test.Errorf("the whole run over the narrowed run's store\n%s\nunder --no-cache\n%s", whole.withoutCounts,
			uncached.withoutCounts)
	}
}
